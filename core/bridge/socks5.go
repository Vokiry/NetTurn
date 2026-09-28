package bridge

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"time"
)

var (
	ErrUnsupportedSocksVersion = errors.New("socks5: unsupported version")
	ErrUnsupportedCommand      = errors.New("socks5: command not supported")
	ErrUnsupportedAddressType  = errors.New("socks5: address type not supported")
)

func handleSocks5(conn net.Conn) {
	// 1. Рукопожатие и выбор метода аутентификации (RFC 1928)
	// Читаем VER и NMETHODS
	buf := make([]byte, 258)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}

	if buf[0] != 0x05 {
		return
	}

	nMethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		return
	}

	// Отвечаем: Версия 5, Метод 0 (Аутентификация не требуется)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// 2. Чтение запроса подключения (CMD)
	// [VER, CMD, RSV, ATYP]
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}

	cmd := buf[1]
	atyp := buf[3]

	// Поддерживаем только команду CONNECT (0x01)
	if cmd != 0x01 {
		_, _ = conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Command not supported
		return
	}

	var targetHost string
	switch atyp {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		targetHost = net.IP(buf[:4]).String()
	case 0x03: // Доменное имя
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:domainLen]); err != nil {
			return
		}
		targetHost = string(buf[:domainLen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		targetHost = net.IP(buf[:16]).String()
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
		return
	}

	// Читаем порт (2 байта Big Endian)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	targetPort := binary.BigEndian.Uint16(buf[:2])
	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(int(targetPort)))

	// Подключаемся к целевому узлу
	targetConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
		return
	}
	defer targetConn.Close()

	// Успешный ответ клиенту
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Двунаправленный обмен
	Relay(conn, targetConn)
}
