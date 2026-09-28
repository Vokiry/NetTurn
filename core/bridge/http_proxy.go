package bridge

import (
	"bufio"
	"net"
	"net/http"
	"strings"
	"time"
)

func handleHTTPProxy(conn net.Conn) {
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	// Обработка HTTPS CONNECT туннеля (RFC 7231)
	if req.Method == http.MethodConnect {
		targetAddr := req.RequestURI
		if !strings.Contains(targetAddr, ":") {
			targetAddr = net.JoinHostPort(targetAddr, "443")
		}

		targetConn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
		if err != nil {
			_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer targetConn.Close()

		// Подтверждаем установку туннеля
		_, err = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		if err != nil {
			return
		}

		// Двунаправленная пересылка трафика
		Relay(conn, targetConn)
		return
	}

	// Обработка обычного HTTP GET/POST запроса
	targetHost := req.URL.Host
	if targetHost == "" {
		targetHost = req.Host
	}
	if !strings.Contains(targetHost, ":") {
		targetHost = net.JoinHostPort(targetHost, "80")
	}

	targetConn, err := net.DialTimeout("tcp", targetHost, 10*time.Second)
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer targetConn.Close()

	// Пересылаем исходный запрос
	if err := req.Write(targetConn); err != nil {
		return
	}

	// Передаем ответ обратно клиенту
	Relay(conn, targetConn)
}
