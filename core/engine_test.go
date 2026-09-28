package core

import (
	"context"
	"testing"
	"time"

	"github.com/Vokiry/NetTurn/core/eventbus"
)

func TestEngineConfigValidation(t *testing.T) {
	bus := eventbus.NewBus()

	// Пустой Peer
	e1 := NewEngine(EngineConfig{Password: "123"}, bus)
	err := e1.Start(context.Background())
	if err != ErrPeerRequired {
		t.Fatalf("Expected ErrPeerRequired, got %v", err)
	}

	// Пустой Password
	e2 := NewEngine(EngineConfig{Peer: "127.0.0.1:56003"}, bus)
	err = e2.Start(context.Background())
	if err != ErrPasswordReq {
		t.Fatalf("Expected ErrPasswordReq, got %v", err)
	}
}

func TestEngineMetricsAndState(t *testing.T) {
	bus := eventbus.NewBus()
	cfg := DefaultEngineConfig()
	cfg.Peer = "127.0.0.1:56003"
	cfg.Password = "testPassword"

	e := NewEngine(cfg, bus)
	m := e.Metrics()

	if m.State != StateDisconnected {
		t.Errorf("Expected initial state %s, got %s", StateDisconnected, m.State)
	}
	if m.TotalWorkers != 9 {
		t.Errorf("Expected 9 workers in default config, got %d", m.TotalWorkers)
	}

	stateCh := make(chan ConnectionState, 5)
	bus.Subscribe("state", func(ev any) {
		stateCh <- ev.(ConnectionState)
	})

	e.setState(StateResolvingDNS)

	select {
	case s := <-stateCh:
		if s != StateResolvingDNS {
			t.Errorf("Expected state %s, got %s", StateResolvingDNS, s)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("Timeout waiting for state event")
	}

	e.Stop()
}
