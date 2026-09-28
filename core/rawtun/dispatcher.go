package rawtun

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vokiry/NetTurn/core/protocol"
)

var ErrDispatcherClosed = errors.New("dispatcher: closed")

// Dispatcher связывает TunDevice и пул воркеров TURN, организуя упаковку RA-кадров и переупорядочивание.
type Dispatcher struct {
	tun         TunDevice
	sendFunc    func(raFramed []byte) error
	reorder     *protocol.ReorderBuffer
	outSeq      protocol.OutSeq
	ctx         context.Context
	cancel      context.CancelFunc
	upBytes     atomic.Int64
	downBytes   atomic.Int64
	upPackets   atomic.Int64
	downPackets atomic.Int64
	tunWriteMu  sync.Mutex
	isClosed    atomic.Bool
	stopOnce    sync.Once
}

// NewDispatcher создает новый двунаправленный диспетчер трафика.
func NewDispatcher(tun TunDevice, sendFunc func(raFramed []byte) error) *Dispatcher {
	return &Dispatcher{
		tun:      tun,
		sendFunc: sendFunc,
		reorder:  protocol.NewReorderBuffer(),
	}
}

// Start запускает цикл чтения из TUN интерфейса и отправки через пул воркеров.
func (d *Dispatcher) Start(ctx context.Context) {
	d.ctx, d.cancel = context.WithCancel(ctx)
	go d.tunReadLoop()
}

func (d *Dispatcher) tunReadLoop() {
	mtu := d.tun.MTU()
	if mtu <= 0 {
		mtu = 1280
	}
	buf := make([]byte, mtu+100)

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		n, err := d.tun.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || d.ctx.Err() != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if n < 20 {
			// Отбрасываем слишком короткие пакеты (меньше минимального IPv4 заголовка)
			continue
		}

		pkt := buf[:n]

		// Проверяем, что это IPv4 пакет
		if pkt[0]>>4 != 4 {
			continue
		}

		d.upBytes.Add(int64(n))
		d.upPackets.Add(1)

		// Оборачиваем в RA Frame со сквозным sequence номером
		seq := d.outSeq.Next()
		framed := protocol.EncodeRAFrame(seq, pkt)

		// Отправляем через пул воркеров
		if d.sendFunc != nil {
			_ = d.sendFunc(framed)
		}
	}
}

// HandleDownlink обрабатывает входящие датаграммы, полученные от любого TURN воркера,
// восстанавливает правильный порядок пакетов через ReorderBuffer и записывает их в TUN.
func (d *Dispatcher) HandleDownlink(packet []byte) {
	if d.isClosed.Load() || len(packet) == 0 {
		return
	}

	// 1. Если это RA-кадр с порядковым номером
	if protocol.IsRAFrame(packet) {
		seq, ipPkt, ok := protocol.DecodeRAFrame(packet)
		if ok {
			// Пропускаем через кольцевой буфер переупорядочивания со stall-таймаутом 40мс
			orderedPackets := d.reorder.Push(seq, ipPkt)
			for _, p := range orderedPackets {
				d.writeToTUN(p)
			}
			return
		}
	}

	// 2. Если это сырой IPv4 пакет без RA-заголовка
	if len(packet) >= 20 && packet[0]>>4 == 4 {
		d.writeToTUN(packet)
	}
}

func (d *Dispatcher) writeToTUN(pkt []byte) {
	if len(pkt) == 0 {
		return
	}

	d.tunWriteMu.Lock()
	n, err := d.tun.Write(pkt)
	d.tunWriteMu.Unlock()

	if err == nil {
		d.downBytes.Add(int64(n))
		d.downPackets.Add(1)
	}
}

// Stats возвращает счетчики трафика диспетчера.
func (d *Dispatcher) Stats() (upBytes, downBytes, upPackets, downPackets int64) {
	return d.upBytes.Load(), d.downBytes.Load(), d.upPackets.Load(), d.downPackets.Load()
}

// Close останавливает горутины диспетчера.
func (d *Dispatcher) Close() {
	d.stopOnce.Do(func() {
		d.isClosed.Store(true)
		if d.cancel != nil {
			d.cancel()
		}
	})
}
