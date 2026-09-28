package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestDualProxySocksAndHTTP(t *testing.T) {
	// Создаем тестовый эхо-сервер
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listener: %v", err)
	}
	defer echoListener.Close()

	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	echoAddr := echoListener.Addr().(*net.TCPAddr)

	// Запускаем DualProxy на случайном свободном порту
	proxy := NewDualProxy(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := proxy.Start(ctx); err != nil {
		t.Fatalf("proxy start: %v", err)
	}
	defer proxy.Close()

	proxyAddr := proxy.Addr().String()

	// 1. Тест SOCKS5 подключения
	t.Run("Socks5", func(t *testing.T) {
		conn, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		defer conn.Close()

		// 1.1. Handshake
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00})
		resp := make([]byte, 2)
		if _, err := io.ReadFull(conn, resp); err != nil {
			t.Fatalf("read socks5 auth resp: %v", err)
		}
		if resp[0] != 0x05 || resp[1] != 0x00 {
			t.Fatalf("invalid socks5 auth resp: %x", resp)
		}

		// 1.2. Connect to echo server
		port := echoAddr.Port
		req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, byte(port >> 8), byte(port & 0xFF)}
		_, _ = conn.Write(req)

		connResp := make([]byte, 10)
		if _, err := io.ReadFull(conn, connResp); err != nil {
			t.Fatalf("read socks5 conn resp: %v", err)
		}
		if connResp[1] != 0x00 {
			t.Fatalf("socks5 connect failed: status=%d", connResp[1])
		}

		// 1.3. Echo Data
		testMsg := "Hello through SOCKS5!"
		_, _ = conn.Write([]byte(testMsg))

		echoBuf := make([]byte, len(testMsg))
		if _, err := io.ReadFull(conn, echoBuf); err != nil {
			t.Fatalf("read echo: %v", err)
		}
		if string(echoBuf) != testMsg {
			t.Fatalf("Echo mismatch: got %q, want %q", string(echoBuf), testMsg)
		}
	})

	// 2. Тест HTTP CONNECT подключения
	t.Run("HTTP_CONNECT", func(t *testing.T) {
		conn, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		defer conn.Close()

		connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", echoAddr.String(), echoAddr.String())
		_, _ = conn.Write([]byte(connectReq))

		reader := bufio.NewReader(conn)
		statusLine, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read connect status: %v", err)
		}
		if !strings.Contains(statusLine, "200") {
			t.Fatalf("HTTP CONNECT failed: %s", statusLine)
		}

		// Читаем пустую строку окончания HTTP-заголовков
		_, _ = reader.ReadString('\n')

		// Тестируем эхо через созданный HTTP туннель
		testMsg := "Hello through HTTP CONNECT!"
		_, _ = conn.Write([]byte(testMsg))

		echoBuf := make([]byte, len(testMsg))
		if _, err := io.ReadFull(reader, echoBuf); err != nil {
			t.Fatalf("read echo: %v", err)
		}
		if string(echoBuf) != testMsg {
			t.Fatalf("Echo mismatch: got %q, want %q", string(echoBuf), testMsg)
		}
	})
}
