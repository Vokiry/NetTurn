package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// DualProxy слушает единый порт (по умолчанию 24066) и автоматически различает SOCKS5 и HTTP CONNECT.
type DualProxy struct {
	listenAddr string
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	isRunning  atomic.Bool
	stopOnce   sync.Once
	activeConn atomic.Int64
}

// NewDualProxy создает новый экземпляр двухрежимного прокси-моста.
func NewDualProxy(port int) *DualProxy {
	if port <= 0 {
		port = 24066
	}
	return &DualProxy{
		listenAddr: fmt.Sprintf("0.0.0.0:%d", port),
	}
}

// Start запускает слушатель TCP на указанном порту.
func (p *DualProxy) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)

	l, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("dual proxy listen %s: %w", p.listenAddr, err)
	}
	p.listener = l
	p.isRunning.Store(true)

	go p.acceptLoop()
	return nil
}

func (p *DualProxy) acceptLoop() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			if !p.isRunning.Load() {
				return
			}
			continue
		}

		p.activeConn.Add(1)
		go func(c net.Conn) {
			defer p.activeConn.Add(-1)
			defer c.Close()
			p.handleConnection(c)
		}(conn)
	}
}

// bufferedConn позволяет прочитать первый байт для сниффинга протокола, не теряя его для нижележащих парсеров.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

func (p *DualProxy) handleConnection(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)

	// Читаем первый байт без его удаления из буфера
	firstByte, err := reader.Peek(1)
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	bConn := &bufferedConn{Conn: conn, r: reader}

	// 0x05 - стандартный маркер версии SOCKS5
	if firstByte[0] == 0x05 {
		handleSocks5(bConn)
		return
	}

	// Иначе обрабатываем как стандартный HTTP CONNECT / HTTP Proxy
	handleHTTPProxy(bConn)
}

// Relay двунаправленно копирует байты между локальным клиентом и удаленным целевым узлом.
func Relay(c1, c2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	closeConns := func() {
		_ = c1.Close()
		_ = c2.Close()
	}

	go func() {
		defer wg.Done()
		defer closeConns()
		_, _ = io.Copy(c1, c2)
	}()

	go func() {
		defer wg.Done()
		defer closeConns()
		_, _ = io.Copy(c2, c1)
	}()

	wg.Wait()
}

// Addr возвращает адрес слушателя.
func (p *DualProxy) Addr() net.Addr {
	if p.listener != nil {
		return p.listener.Addr()
	}
	return nil
}

// Close останавливает слушатель и закрывает активные соединения.
func (p *DualProxy) Close() {
	p.stopOnce.Do(func() {
		p.isRunning.Store(false)
		if p.cancel != nil {
			p.cancel()
		}
		if p.listener != nil {
			_ = p.listener.Close()
		}
	})
}
