package rawtun

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Vokiry/NetTurn/core/protocol"
)

type mockTun struct {
	readCh  chan []byte
	writeCh chan []byte
	mtu     int
}

func newMockTun(mtu int) *mockTun {
	return &mockTun{
		readCh:  make(chan []byte, 100),
		writeCh: make(chan []byte, 100),
		mtu:     mtu,
	}
}

func (m *mockTun) Read(b []byte) (int, error) {
	pkt, ok := <-m.readCh
	if !ok {
		return 0, ioEOF
	}
	n := copy(b, pkt)
	return n, nil
}

func (m *mockTun) Write(b []byte) (int, error) {
	cp := append([]byte(nil), b...)
	m.writeCh <- cp
	return len(b), nil
}

func (m *mockTun) Close() error {
	close(m.readCh)
	return nil
}

func (m *mockTun) MTU() int {
	return m.mtu
}

func (m *mockTun) Name() string {
	return "mocktun0"
}

var ioEOF = context.Canceled

func createTestIPv4(seqByte byte) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45
	pkt[9] = 6
	pkt[12], pkt[13], pkt[14], pkt[15] = 10, 70, 0, 2
	pkt[16], pkt[17], pkt[18], pkt[19] = 1, 1, 1, 1
	pkt[20] = seqByte
	return pkt
}

func TestDispatcherUplinkAndDownlink(t *testing.T) {
	tun := newMockTun(1280)

	sentPackets := make([][]byte, 0)
	var sendMu sync.Mutex

	sendFunc := func(raFramed []byte) error {
		sendMu.Lock()
		sentPackets = append(sentPackets, append([]byte(nil), raFramed...))
		sendMu.Unlock()
		return nil
	}

	disp := NewDispatcher(tun, sendFunc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disp.Start(ctx)

	// 1. Проверяем Uplink: отправляем сырой IP-пакет в TUN -> он должен быть завернут в RA Frame
	testPkt := createTestIPv4(77)
	tun.readCh <- testPkt

	time.Sleep(50 * time.Millisecond)

	sendMu.Lock()
	if len(sentPackets) != 1 {
		t.Fatalf("Expected 1 sent packet in uplink, got %d", len(sentPackets))
	}
	seq, ipPayload, ok := protocol.DecodeRAFrame(sentPackets[0])
	sendMu.Unlock()

	if !ok || seq != 0 || !bytes.Equal(ipPayload, testPkt) {
		t.Fatalf("Uplink RA Frame encoding failed")
	}

	// 2. Проверяем Downlink: инициализируем поток seq=0, затем передаем seq=2, затем seq=1
	pkt0 := createTestIPv4(0)
	pkt1 := createTestIPv4(1)
	pkt2 := createTestIPv4(2)

	ra0 := protocol.EncodeRAFrame(0, pkt0)
	ra1 := protocol.EncodeRAFrame(1, pkt1)
	ra2 := protocol.EncodeRAFrame(2, pkt2)

	// Передаем начальный seq=0
	disp.HandleDownlink(ra0)
	select {
	case received0 := <-tun.writeCh:
		if !bytes.Equal(received0, pkt0) {
			t.Fatalf("Packet 0 mismatch")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Timeout waiting for packet 0")
	}

	// Передаем теперь seq=2 (должен буферизоваться в ожидании seq=1)
	disp.HandleDownlink(ra2)

	select {
	case <-tun.writeCh:
		t.Fatalf("Packet 2 should NOT be written yet (waiting for packet 1)")
	case <-time.After(30 * time.Millisecond):
		// OK
	}

	// Теперь передаем seq=1 -> оба пакета должны записаться строго в порядке 1, затем 2
	disp.HandleDownlink(ra1)

	var received1, received2 []byte
	select {
	case received1 = <-tun.writeCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Timeout waiting for packet 1")
	}

	select {
	case received2 = <-tun.writeCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Timeout waiting for packet 2")
	}

	if !bytes.Equal(received1, pkt1) || !bytes.Equal(received2, pkt2) {
		t.Fatalf("Downlink packets written in wrong order!")
	}

	upB, downB, upP, downP := disp.Stats()
	if upP != 1 || downP != 3 || upB == 0 || downB == 0 {
		t.Errorf("Stats mismatch: upP=%d, downP=%d, upB=%d, downB=%d", upP, downP, upB, downB)
	}

	disp.Close()
}
