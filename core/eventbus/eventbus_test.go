package eventbus

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus(t *testing.T) {
	bus := NewBus()

	var wg sync.WaitGroup
	wg.Add(2)

	received := make([]string, 0)
	var mu sync.Mutex

	sub1 := bus.Subscribe("test-topic", func(ev any) {
		mu.Lock()
		received = append(received, ev.(string)+"-sub1")
		mu.Unlock()
		wg.Done()
	})

	sub2 := bus.Subscribe("test-topic", func(ev any) {
		mu.Lock()
		received = append(received, ev.(string)+"-sub2")
		mu.Unlock()
		wg.Done()
	})

	bus.Publish("test-topic", "hello")

	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Timeout waiting for event handlers")
	}

	if len(received) != 2 {
		t.Fatalf("Expected 2 events received, got %d", len(received))
	}

	// Отписка одного обработчика
	sub1.Unsubscribe()

	wg.Add(1)
	bus.Publish("test-topic", "world")

	waitCh2 := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh2)
	}()

	select {
	case <-waitCh2:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Timeout waiting for second event")
	}

	sub2.Unsubscribe()
}
