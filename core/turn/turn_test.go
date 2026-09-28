package turn

import (
	"net"
	"testing"

	"github.com/Vokiry/NetTurn/core/protocol"
)

func TestNewWorker(t *testing.T) {
	peerAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:56003")
	handler := func(packet []byte, workerID int) {}

	w, err := NewWorker(1, peerAddr, "testPassword123", protocol.ObfsAudio, handler)
	if err != nil {
		t.Fatalf("NewWorker failed: %v", err)
	}

	if w.id != 1 {
		t.Errorf("Expected worker ID 1, got %d", w.id)
	}

	st, _ := w.state.Load().(WorkerState)
	if st != WorkerStateIdle {
		t.Errorf("Expected state %s, got %s", WorkerStateIdle, st)
	}

	stats := w.Stats()
	if stats.ID != 1 || stats.State != WorkerStateIdle {
		t.Errorf("Stats mismatch: %+v", stats)
	}

	// Проверка ошибки при пустом пароле
	_, err = NewWorker(2, peerAddr, "", protocol.ObfsAudio, handler)
	if err == nil {
		t.Errorf("NewWorker with empty password should return error")
	}
}

func TestNewWorkerPool(t *testing.T) {
	peerAddr, _ := net.ResolveUDPAddr("udp", "1.2.3.4:56003")
	cfg := Config{
		NumWorkers:     9,
		PeerAddr:       peerAddr,
		Password:       "pass",
		AllocateGateMs: 100,
	}

	pool := NewWorkerPool(cfg, nil, protocol.ObfsAudio, nil)
	if pool.cfg.NumWorkers != 9 {
		t.Errorf("Expected 9 workers configured, got %d", pool.cfg.NumWorkers)
	}
	if pool.ActiveWorkersCount() != 0 {
		t.Errorf("Expected 0 active workers before start, got %d", pool.ActiveWorkersCount())
	}
}
