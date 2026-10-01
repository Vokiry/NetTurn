package turn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vokiry/NetTurn/core/crypto"
	"github.com/Vokiry/NetTurn/core/protocol"
	"github.com/Vokiry/NetTurn/core/vk"
	"github.com/pion/logging"
	pionturn "github.com/pion/turn/v5"
)

var (
	ErrWorkerClosed = errors.New("turn: worker is closed")
)

// Worker представляет собой один независимый TURN-канал через релеи VK Calls.
type Worker struct {
	id          int
	peerAddr    *net.UDPAddr
	key         []byte
	obfsCfg     *protocol.RTPConfig
	obfsState   *protocol.RTPState
	state       atomic.Value // WorkerState
	client      *pionturn.Client
	localConn   net.PacketConn
	relayConn   net.PacketConn
	handler     PacketHandler
	ctx         context.Context
	cancel      context.CancelFunc
	bytesSent   atomic.Int64
	bytesRecv   atomic.Int64
	packetsSent atomic.Int64
	packetsRecv atomic.Int64
	lastSeen    atomic.Int64
	closeOnce   sync.Once
}

// NewWorker создает новый экземпляр воркера с собственными независимыми счетчиками RTP.
func NewWorker(id int, peerAddr *net.UDPAddr, password string, obfs protocol.ObfsType, handler PacketHandler) (*Worker, error) {
	key, err := crypto.DeriveKey(password)
	if err != nil {
		return nil, fmt.Errorf("worker %d: key derive failed: %w", id, err)
	}

	w := &Worker{
		id:        id,
		peerAddr:  peerAddr,
		key:       key,
		obfsCfg:   protocol.NewRTPConfig(obfs),
		obfsState: protocol.NewRTPState(),
		handler:   handler,
	}
	w.state.Store(WorkerStateIdle)
	w.lastSeen.Store(time.Now().UnixNano())
	return w, nil
}

type connectedUDPConn struct {
	*net.UDPConn
}

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.Write(p)
}

// Start подключается к TURN-серверу по полученным креденшелам, запрашивает аллокацию и запускает циклы приема и keepalive.
func (w *Worker) Start(ctx context.Context, creds *vk.TurnCredentials, useTCP bool) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.state.Store(WorkerStateConnecting)

	var conn net.PacketConn
	if useTCP {
		d := net.Dialer{Timeout: 5 * time.Second}
		tcpConn, dErr := d.DialContext(w.ctx, "tcp", creds.ServerAddr)
		if dErr != nil {
			w.state.Store(WorkerStateError)
			return fmt.Errorf("worker %d: tcp dial turn: %w", w.id, dErr)
		}
		conn = pionturn.NewSTUNConn(tcpConn)
	} else {
		turnUDPAddr, err := net.ResolveUDPAddr("udp", creds.ServerAddr)
		if err != nil {
			w.state.Store(WorkerStateError)
			return fmt.Errorf("worker %d: resolve turn addr %s: %w", w.id, creds.ServerAddr, err)
		}
		udpConn, dErr := net.DialUDP("udp", nil, turnUDPAddr)
		if dErr != nil {
			w.state.Store(WorkerStateError)
			return fmt.Errorf("worker %d: dial udp turn: %w", w.id, dErr)
		}
		conn = &connectedUDPConn{udpConn}
	}
	w.localConn = conn

	cfg := &pionturn.ClientConfig{
		STUNServerAddr:         creds.ServerAddr,
		TURNServerAddr:         creds.ServerAddr,
		Conn:                   conn,
		Username:               creds.Username,
		Password:               creds.Password,
		RequestedAddressFamily: pionturn.RequestedAddressFamilyIPv4,
		LoggerFactory:          logging.NewDefaultLoggerFactory(),
	}

	client, err := pionturn.NewClient(cfg)
	if err != nil {
		w.cleanupLocal()
		w.state.Store(WorkerStateError)
		return fmt.Errorf("worker %d: new turn client: %w", w.id, err)
	}
	w.client = client

	if err := client.Listen(); err != nil {
		w.Close()
		w.state.Store(WorkerStateError)
		return fmt.Errorf("worker %d: turn listen: %w", w.id, err)
	}

	relayConn, err := client.Allocate()
	if err != nil {
		w.Close()
		w.state.Store(WorkerStateError)
		return fmt.Errorf("worker %d: turn allocate: %w", w.id, err)
	}
	w.relayConn = relayConn

	w.state.Store(WorkerStateActive)

	// Запуск фоновых горутин чтения и keepalive
	go w.readLoop()
	go w.keepaliveLoop()

	return nil
}

// Send упаковывает пакет в RTP v2 с ChaCha20 AEAD и отправляет его на VPS сервер через TURN-релей.
func (w *Worker) Send(payload []byte) error {
	if w.state.Load() != WorkerStateActive || w.relayConn == nil {
		return ErrWorkerClosed
	}

	wire, err := protocol.WrapPacket(w.key, payload, w.obfsCfg, w.obfsState)
	if err != nil {
		return fmt.Errorf("worker %d: wrap packet: %w", w.id, err)
	}

	n, err := w.relayConn.WriteTo(wire, w.peerAddr)
	if err != nil {
		return err
	}

	w.bytesSent.Add(int64(n))
	w.packetsSent.Add(1)
	w.lastSeen.Store(time.Now().UnixNano())
	return nil
}

func (w *Worker) readLoop() {
	buf := make([]byte, 2048)
	plainBuf := make([]byte, 2048)

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		_ = w.relayConn.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, _, err := w.relayConn.ReadFrom(buf)
		if err != nil {
			if w.ctx.Err() != nil {
				return
			}
			// При сетевом таймауте продолжаем ожидание
			continue
		}

		w.lastSeen.Store(time.Now().UnixNano())
		w.bytesRecv.Add(int64(n))
		w.packetsRecv.Add(1)

		raw := buf[:n]

		// Обработка 1-байтового keepalive от сервера (0xFF)
		if n == 1 && raw[0] == 0xFF {
			continue
		}

		// Дешифрация RTP пакета
		decLen, err := protocol.UnwrapPacket(w.key, raw, plainBuf)
		if err != nil {
			// Отбрасываем невалидный или поврежденный пакет
			continue
		}

		// Передаем расшифрованный полезный груз в обработчик
		if w.handler != nil && decLen > 0 {
			w.handler(plainBuf[:decLen], w.id)
		}
	}
}

func (w *Worker) keepaliveLoop() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	ping := []byte{0xFF}

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if w.relayConn != nil && w.state.Load() == WorkerStateActive {
				_, _ = w.relayConn.WriteTo(ping, w.peerAddr)
			}
		}
	}
}

// Stats возвращает актуальную статистику работы воркера.
func (w *Worker) Stats() WorkerStats {
	relayAddrStr := ""
	if w.relayConn != nil {
		relayAddrStr = w.relayConn.LocalAddr().String()
	}

	st, _ := w.state.Load().(WorkerState)
	return WorkerStats{
		ID:           w.id,
		State:        st,
		RelayAddr:    relayAddrStr,
		BytesSent:    w.bytesSent.Load(),
		BytesRecv:    w.bytesRecv.Load(),
		PacketsSent:  w.packetsSent.Load(),
		PacketsRecv:  w.packetsRecv.Load(),
		LastActivity: time.Unix(0, w.lastSeen.Load()),
	}
}

func (w *Worker) cleanupLocal() {
	if w.localConn != nil {
		_ = w.localConn.Close()
	}
}

// Close корректно останавливает воркер и освобождает сетевые ресурсы.
func (w *Worker) Close() {
	w.closeOnce.Do(func() {
		w.state.Store(WorkerStateClosed)
		if w.cancel != nil {
			w.cancel()
		}
		if w.relayConn != nil {
			_ = w.relayConn.Close()
		}
		if w.client != nil {
			w.client.Close()
		}
		w.cleanupLocal()
	})
}
