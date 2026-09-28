package network

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// NetworkChangeHandler вызывается при обнаружении смены шлюза или сетевого интерфейса.
type NetworkChangeHandler func(newGw *GatewayInfo)

// AdaptiveWatcher отслеживает состояние сети, сон ПК и смену шлюзов через TCP-зондирование.
type AdaptiveWatcher struct {
	targetAddr   string
	probeTimeout time.Duration
	interval     time.Duration
	onChanged    NetworkChangeHandler
	lastGwIP     string
	lastIface    string
	mu           sync.Mutex
	isProbing    atomic.Bool
	ctx          context.Context
	cancel       context.CancelFunc
	stopOnce     sync.Once
}

// NewAdaptiveWatcher создает новый сетевой наблюдатель.
func NewAdaptiveWatcher(interval time.Duration, onChanged NetworkChangeHandler) *AdaptiveWatcher {
	if interval <= 0 {
		interval = 2500 * time.Millisecond
	}

	return &AdaptiveWatcher{
		targetAddr:   "77.88.8.8:53",
		probeTimeout: 800 * time.Millisecond,
		interval:     interval,
		onChanged:    onChanged,
	}
}

// Start запускает цикл наблюдения в фоновой горутине.
func (w *AdaptiveWatcher) Start(ctx context.Context) {
	w.ctx, w.cancel = context.WithCancel(ctx)

	// Инициализируем текущее состояние шлюза
	if gw, err := FindPhysicalDefaultGateway(); err == nil {
		w.lastGwIP = gw.GatewayIP.String()
		w.lastIface = gw.Interface
	}

	go w.watchLoop()
}

func (w *AdaptiveWatcher) watchLoop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.checkNetwork()
		}
	}
}

func (w *AdaptiveWatcher) checkNetwork() {
	// 1. Проверяем смену шлюза в системе
	gw, err := FindPhysicalDefaultGateway()
	if err == nil && gw != nil {
		w.mu.Lock()
		gwChanged := gw.GatewayIP.String() != w.lastGwIP || gw.Interface != w.lastIface
		if gwChanged {
			w.lastGwIP = gw.GatewayIP.String()
			w.lastIface = gw.Interface
			w.mu.Unlock()

			if w.onChanged != nil {
				w.onChanged(gw)
			}
			return
		}
		w.mu.Unlock()
	}

	// 2. Легковесная TCP-проба доступности сети (SYN probe)
	d := net.Dialer{Timeout: w.probeTimeout}
	conn, err := d.DialContext(w.ctx, "tcp", w.targetAddr)
	if err == nil {
		_ = conn.Close()
	}
}

// Stop останавливает наблюдатель.
func (w *AdaptiveWatcher) Stop() {
	w.stopOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
	})
}
