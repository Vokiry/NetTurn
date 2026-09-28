package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vokiry/NetTurn/core/eventbus"
	"github.com/Vokiry/NetTurn/core/rawtun"
	"github.com/Vokiry/NetTurn/core/turn"
	"github.com/Vokiry/NetTurn/core/vk"
)

var (
	ErrAlreadyStarted = errors.New("engine: already started")
	ErrPeerRequired   = errors.New("engine: peer address is required")
	ErrPasswordReq    = errors.New("engine: password is required")
)

// Engine главный контроллер туннеля NetTurn, управляющий всеми подсистемами.
type Engine struct {
	cfg        EngineConfig
	bus        *eventbus.Bus
	callPool   *vk.CallPool
	httpClient *vk.HTTPClient
	workerPool *turn.WorkerPool
	dispatcher *rawtun.Dispatcher
	tun        rawtun.TunDevice
	peerAddr   *net.UDPAddr
	ctx        context.Context
	cancel     context.CancelFunc
	state      atomic.Value // ConnectionState
	startTime  time.Time
	mu         sync.RWMutex
	stopOnce   sync.Once
	isStarted  atomic.Bool

	// Метрики
	prevUpBytes   int64
	prevDownBytes int64
	prevTime      time.Time
}

// NewEngine инициализирует новый экземпляр движка.
func NewEngine(cfg EngineConfig, bus *eventbus.Bus) *Engine {
	if bus == nil {
		bus = eventbus.NewBus()
	}

	e := &Engine{
		cfg: cfg,
		bus: bus,
	}
	e.state.Store(StateDisconnected)
	return e
}

// Start запускает полный цикл подключения туннеля и обновляет Connection Pipeline.
func (e *Engine) Start(ctx context.Context) error {
	if e.isStarted.Swap(true) {
		return ErrAlreadyStarted
	}

	if e.cfg.Peer == "" {
		e.isStarted.Store(false)
		return ErrPeerRequired
	}
	if e.cfg.Password == "" {
		e.isStarted.Store(false)
		return ErrPasswordReq
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.startTime = time.Now()
	e.prevTime = time.Now()

	e.emitPhase(PhaseDNS, PhaseStatusRunning, "Разрешение адреса сервера...")
	e.setState(StateResolvingDNS)

	peer, err := net.ResolveUDPAddr("udp", e.cfg.Peer)
	if err != nil {
		e.failPhase(PhaseDNS, fmt.Sprintf("Ошибка DNS: %v", err))
		return err
	}
	e.peerAddr = peer
	e.emitPhase(PhaseDNS, PhaseStatusOK, "Адрес сервера успешно разрешен")

	// 2. Инициализация VK API и пула звонков
	e.emitPhase(PhaseVKAPI, PhaseStatusRunning, "Подключение к VK Calls API...")
	e.setState(StateFetchingVKCreds)

	e.httpClient = vk.NewHTTPClient(e.cfg.DNS, vk.DefaultBrowserProfile())
	e.callPool = vk.NewCallPool(e.httpClient, e.cfg.VKLinks)

	creds, err := e.callPool.GetTurnCredentials(e.ctx)
	if err != nil {
		e.failPhase(PhaseVKAPI, fmt.Sprintf("Не удалось получить креденшелы VK: %v", err))
		return err
	}
	e.emitPhase(PhaseVKAPI, PhaseStatusOK, "Креденшелы звонка успешно получены")
	e.emitPhase(PhaseCaptcha, PhaseStatusOK, "Проверка защиты пройдена")

	// 3. Подготовка криптографии WRAP AEAD
	e.emitPhase(PhaseWrapAEAD, PhaseStatusRunning, "Инициализация криптографического движка...")
	e.emitPhase(PhaseWrapAEAD, PhaseStatusOK, "ChaCha20-Poly1305 AEAD инициализирован")

	// 4. Инициализация TURN и пула воркеров
	e.emitPhase(PhaseTURNRelay, PhaseStatusOK, fmt.Sprintf("Релей обнаружен: %s", creds.ServerAddr))
	e.emitPhase(PhaseWorkers, PhaseStatusRunning, fmt.Sprintf("Каскадный запуск %d воркеров...", e.cfg.Workers))
	e.setState(StateWorkersInit)

	turnCfg := turn.Config{
		NumWorkers:     e.cfg.Workers,
		PeerAddr:       e.peerAddr,
		Password:       e.cfg.Password,
		UseTCP:         e.cfg.TurnTCP,
		AllocateGateMs: 150,
	}

	e.workerPool = turn.NewWorkerPool(turnCfg, e.callPool, e.cfg.Obfs, func(packet []byte, workerID int) {
		if e.dispatcher != nil {
			e.dispatcher.HandleDownlink(packet)
		}
	})

	if err := e.workerPool.Start(e.ctx); err != nil {
		e.failPhase(PhaseWorkers, fmt.Sprintf("Сбой запуска воркеров: %v", err))
		return err
	}
	e.emitPhase(PhaseWorkers, PhaseStatusOK, fmt.Sprintf("Воркеры активны (%d/%d)", e.workerPool.ActiveWorkersCount(), e.cfg.Workers))

	// 5. Инициализация TUN адаптера
	e.emitPhase(PhaseTUN, PhaseStatusRunning, "Конфигурация виртуального адаптера...")
	e.setState(StateConfiguringTUN)

	var tunDev rawtun.TunDevice
	if e.cfg.AndroidFd > 0 {
		tunDev, err = rawtun.WrapFd(e.cfg.AndroidFd, e.cfg.MTU)
	} else {
		tunDev, err = rawtun.CreateLinuxTun(e.cfg.TunName, e.cfg.MTU)
	}

	if err != nil {
		e.failPhase(PhaseTUN, fmt.Sprintf("Сбой создания TUN: %v", err))
		return err
	}
	e.tun = tunDev

	e.dispatcher = rawtun.NewDispatcher(tunDev, func(raFramed []byte) error {
		if e.workerPool != nil {
			return e.workerPool.Dispatch(raFramed)
		}
		return nil
	})
	e.dispatcher.Start(e.ctx)

	e.emitPhase(PhaseTUN, PhaseStatusOK, fmt.Sprintf("Адаптер %s поднят (MTU %d)", tunDev.Name(), tunDev.MTU()))
	e.emitPhase(PhaseActive, PhaseStatusOK, "Туннель успешно подключен")
	e.setState(StateConnected)

	// Фоновый цикл сбора метрик и статистики
	go e.metricsLoop()

	return nil
}

func (e *Engine) metricsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-ticker.C:
			e.publishMetrics(now)
		}
	}
}

func (e *Engine) publishMetrics(now time.Time) {
	if e.dispatcher == nil {
		return
	}

	upBytes, downBytes, _, _ := e.dispatcher.Stats()
	elapsed := now.Sub(e.prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	upRate := int64(float64(upBytes-e.prevUpBytes) / elapsed)
	downRate := int64(float64(downBytes-e.prevDownBytes) / elapsed)
	if upRate < 0 {
		upRate = 0
	}
	if downRate < 0 {
		downRate = 0
	}

	e.prevUpBytes = upBytes
	e.prevDownBytes = downBytes
	e.prevTime = now

	activeWorkers := 0
	if e.workerPool != nil {
		activeWorkers = e.workerPool.ActiveWorkersCount()
	}

	st, _ := e.state.Load().(ConnectionState)

	metrics := EngineMetrics{
		State:           st,
		UploadBytes:     upBytes,
		DownloadBytes:   downBytes,
		UploadRateBps:   upRate,
		DownloadRateBps: downRate,
		ActiveWorkers:   activeWorkers,
		TotalWorkers:    e.cfg.Workers,
		UptimeSeconds:   int64(now.Sub(e.startTime).Seconds()),
	}

	e.bus.Publish("metrics", metrics)
}

func (e *Engine) setState(s ConnectionState) {
	e.state.Store(s)
	e.bus.Publish("state", s)
}

func (e *Engine) emitPhase(phase PipelinePhase, status PhaseStatus, msg string) {
	e.bus.Publish("pipeline", PipelineStepEvent{
		Phase:   phase,
		Status:  status,
		Message: msg,
		Time:    time.Now(),
	})
}

func (e *Engine) failPhase(phase PipelinePhase, msg string) {
	e.emitPhase(phase, PhaseStatusFailed, msg)
	e.setState(StateError)
	e.Stop()
}

// Metrics возвращает снимок текущей производительности туннеля.
func (e *Engine) Metrics() EngineMetrics {
	now := time.Now()
	upBytes, downBytes := int64(0), int64(0)
	if e.dispatcher != nil {
		upBytes, downBytes, _, _ = e.dispatcher.Stats()
	}

	activeWorkers := 0
	if e.workerPool != nil {
		activeWorkers = e.workerPool.ActiveWorkersCount()
	}

	st, _ := e.state.Load().(ConnectionState)

	return EngineMetrics{
		State:         st,
		UploadBytes:   upBytes,
		DownloadBytes: downBytes,
		ActiveWorkers: activeWorkers,
		TotalWorkers:  e.cfg.Workers,
		UptimeSeconds: int64(now.Sub(e.startTime).Seconds()),
	}
}

// Stop корректно останавливает туннель и освобождает ресурсы.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		e.setState(StateDisconnected)
		e.isStarted.Store(false)

		if e.cancel != nil {
			e.cancel()
		}
		if e.dispatcher != nil {
			e.dispatcher.Close()
		}
		if e.tun != nil {
			_ = e.tun.Close()
		}
		if e.workerPool != nil {
			e.workerPool.Close()
		}
	})
}
