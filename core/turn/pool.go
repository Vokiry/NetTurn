package turn

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vokiry/NetTurn/core/protocol"
	"github.com/Vokiry/NetTurn/core/vk"
)

var (
	ErrNoActiveWorkers = errors.New("turn: no active workers in pool")
)

// WorkerPool управляет группой параллельных TURN-воркеров, реализуя каскадный запуск и Flow Affinity.
type WorkerPool struct {
	cfg         Config
	callPool    *vk.CallPool
	workers     []*Worker
	flowTable   *protocol.FlowTable[*Worker]
	onPacket    PacketHandler
	obfs        protocol.ObfsType
	rrCounter   atomic.Uint64
	assignedIP  string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	activeCount atomic.Int32
	isStarted   atomic.Bool
	stopOnce    sync.Once
}

// NewWorkerPool создает новый пул воркеров.
func NewWorkerPool(cfg Config, callPool *vk.CallPool, obfs protocol.ObfsType, onPacket PacketHandler) *WorkerPool {
	if cfg.NumWorkers <= 0 {
		cfg.NumWorkers = 9
	}
	if cfg.AllocateGateMs <= 0 {
		cfg.AllocateGateMs = 150
	}

	return &WorkerPool{
		cfg:       cfg,
		callPool:  callPool,
		workers:   make([]*Worker, cfg.NumWorkers),
		flowTable: protocol.NewFlowTable[*Worker](protocol.DefaultFlowTTL),
		obfs:      obfs,
		onPacket:  onPacket,
	}
}

// Start запускает воркеры пула с каскадной паузой (staggered allocation) для защиты от лимитов TURN 486.
func (p *WorkerPool) Start(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.isStarted.Store(true)

	// Получаем первоначальные креденшелы
	creds, err := p.callPool.GetTurnCredentials(p.ctx)
	if err != nil {
		return fmt.Errorf("pool: failed to get initial turn credentials: %w", err)
	}

	gateDelay := time.Duration(p.cfg.AllocateGateMs) * time.Millisecond

	// Каскадный запуск воркеров
	var firstErr error
	var startedCount int

	for i := 0; i < p.cfg.NumWorkers; i++ {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
		}

		w, err := NewWorker(i, p.cfg.PeerAddr, p.cfg.Password, p.obfs, p.handleWorkerPacket)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		p.mu.Lock()
		p.workers[i] = w
		p.mu.Unlock()

		// Подключаем воркер к TURN
		startErr := w.Start(p.ctx, creds, p.cfg.UseTCP)
		if startErr != nil {
			if firstErr == nil {
				firstErr = startErr
			}
			// Запускаем фоновую попытку переподключения воркера
			go p.superviseWorker(i)
		} else {
			p.activeCount.Add(1)
			startedCount++

			if i == 0 {
				// Воркер 0 получает назначенный IP синхронно без конкуренции горутин чтения
				ip, hErr := w.HandshakeRawConf(p.cfg.DeviceID, p.cfg.Password, p.cfg.MTU)
				if hErr == nil && ip != "" && p.assignedIP == "" {
					p.assignedIP = ip
				}
				w.StartLoops()
			} else {
				// Вторичные воркеры регистрируются в фоне и активируют циклы чтения
				go func(worker *Worker) {
					_, _ = worker.HandshakeRawConf(p.cfg.DeviceID, p.cfg.Password, p.cfg.MTU)
					worker.StartLoops()
				}(w)
			}
		}

		// Задержка перед запуском следующего воркера (защита от rate limit)
		if i < p.cfg.NumWorkers-1 {
			time.Sleep(gateDelay)
		}
	}

	if startedCount == 0 && firstErr != nil {
		p.Close()
		return fmt.Errorf("pool: failed to start any worker: %w", firstErr)
	}

	// Запуск наблюдателя за здоровьем воркеров
	go p.watchdogLoop()

	return nil
}

// Dispatch отправляет пакет через воркер, используя Flow Affinity (5-tuple) или Round-Robin.
func (p *WorkerPool) Dispatch(packet []byte) error {
	if !p.isStarted.Load() {
		return ErrNoActiveWorkers
	}

	key := protocol.FlowKey(packet)

	// 1. Проверяем наличие привязки потока
	if worker, ok := p.flowTable.Get(key); ok && worker != nil {
		st, _ := worker.state.Load().(WorkerState)
		if st == WorkerStateActive {
			p.flowTable.Touch(key)
			return worker.Send(packet)
		}
	}

	// 2. Если привязки нет или воркер недоступен - выбираем следующий активный воркер
	liveWorkers := p.getLiveWorkers()
	if len(liveWorkers) == 0 {
		return ErrNoActiveWorkers
	}

	idx := p.rrCounter.Add(1) % uint64(len(liveWorkers))
	chosenWorker := liveWorkers[idx]

	// Закрепляем поток за выбранным воркером
	p.flowTable.Set(key, chosenWorker)

	return chosenWorker.Send(packet)
}

func (p *WorkerPool) handleWorkerPacket(packet []byte, workerID int) {
	if p.onPacket != nil {
		p.onPacket(packet, workerID)
	}
}

func (p *WorkerPool) getLiveWorkers() []*Worker {
	p.mu.RLock()
	defer p.mu.RUnlock()

	live := make([]*Worker, 0, len(p.workers))
	for _, w := range p.workers {
		if w != nil {
			st, _ := w.state.Load().(WorkerState)
			if st == WorkerStateActive {
				live = append(live, w)
			}
		}
	}
	return live
}

func (p *WorkerPool) superviseWorker(id int) {
	select {
	case <-p.ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}

	p.mu.Lock()
	oldW := p.workers[id]
	if oldW != nil {
		oldW.Close()
	}
	p.mu.Unlock()

	creds, err := p.callPool.GetTurnCredentials(p.ctx)
	if err != nil {
		return
	}

	newW, err := NewWorker(id, p.cfg.PeerAddr, p.cfg.Password, p.obfs, p.handleWorkerPacket)
	if err != nil {
		return
	}

	p.mu.Lock()
	p.workers[id] = newW
	p.mu.Unlock()

	if err := newW.Start(p.ctx, creds, p.cfg.UseTCP); err == nil {
		p.activeCount.Add(1)
	}
}

func (p *WorkerPool) watchdogLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.mu.RLock()
			for i, w := range p.workers {
				if w == nil {
					go p.superviseWorker(i)
					continue
				}
				st, _ := w.state.Load().(WorkerState)
				if st != WorkerStateActive && st != WorkerStateConnecting {
					go p.superviseWorker(i)
				}
			}
			p.mu.RUnlock()
		}
	}
}

// ActiveWorkersCount возвращает число активных в данный момент воркеров.
func (p *WorkerPool) ActiveWorkersCount() int {
	return len(p.getLiveWorkers())
}

// AssignedIP возвращает IP-адрес клиента, выданный сервером.
func (p *WorkerPool) AssignedIP() string {
	return p.assignedIP
}

// Stats собирает суммарную статистику со всех воркеров пула.
func (p *WorkerPool) Stats() []WorkerStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make([]WorkerStats, 0, len(p.workers))
	for _, w := range p.workers {
		if w != nil {
			stats = append(stats, w.Stats())
		}
	}
	return stats
}

// Close корректно останавливает все воркеры пула.
func (p *WorkerPool) Close() {
	p.stopOnce.Do(func() {
		p.isStarted.Store(false)
		if p.cancel != nil {
			p.cancel()
		}

		p.mu.Lock()
		defer p.mu.Unlock()

		for _, w := range p.workers {
			if w != nil {
				w.Close()
			}
		}
		p.workers = nil
	})
}
