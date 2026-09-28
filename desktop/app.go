package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/Vokiry/NetTurn/core"
	"github.com/Vokiry/NetTurn/core/eventbus"
	"github.com/Vokiry/NetTurn/core/network"
	"github.com/Vokiry/NetTurn/core/vk"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App представляет собой основной мост между UI (React/TS) и ядром туннеля Go.
type App struct {
	ctx       context.Context
	engine    *core.Engine
	bus       *eventbus.Bus
	routeMgr  *network.RouteManager
	config    core.EngineConfig
	mu        sync.Mutex
	isStarted bool
}

// NewApp инициализирует приложение.
func NewApp() *App {
	bus := eventbus.NewBus()
	cfg := core.DefaultEngineConfig()

	return &App{
		bus:    bus,
		config: cfg,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Проброс событий конвейера (Connection Pipeline) в React фронтенд
	a.bus.Subscribe("pipeline", func(ev any) {
		wailsruntime.EventsEmit(a.ctx, "pipeline_step", ev)
	})

	// Проброс секундных метрик скорости и состояния в React фронтенд
	a.bus.Subscribe("metrics", func(ev any) {
		wailsruntime.EventsEmit(a.ctx, "metrics_update", ev)
	})

	// Проброс смены статуса
	a.bus.Subscribe("state", func(ev any) {
		wailsruntime.EventsEmit(a.ctx, "state_change", ev)
	})
}

func (a *App) shutdown(ctx context.Context) {
	a.StopTunnel()
}

// GetDefaultConfig возвращает конфигурацию по умолчанию.
func (a *App) GetDefaultConfig() core.EngineConfig {
	return a.config
}

// StartTunnel запускает туннель с переданными из UI настройками.
func (a *App) StartTunnel(cfg core.EngineConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.isStarted {
		return fmt.Errorf("tunnel is already running")
	}

	a.config = cfg
	a.engine = core.NewEngine(cfg, a.bus)
	a.routeMgr = network.NewRouteManager(cfg.TunName)

	go func() {
		if err := a.engine.Start(context.Background()); err != nil {
			wailsruntime.EventsEmit(a.ctx, "tunnel_error", err.Error())
			a.mu.Lock()
			a.isStarted = false
			a.mu.Unlock()
			return
		}

		// Автоматическая настройка защитных маршрутов Linux
		_ = a.routeMgr.ProtectIP(cfg.Peer)
		_ = a.routeMgr.EnableTunnelRoutes()
	}()

	a.isStarted = true
	return nil
}

// StopTunnel останавливает активный туннель и сбрасывает сетевые маршруты.
func (a *App) StopTunnel() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.isStarted {
		return
	}

	if a.routeMgr != nil {
		a.routeMgr.DisableTunnelRoutes()
	}
	if a.engine != nil {
		a.engine.Stop()
	}

	a.isStarted = false
	wailsruntime.EventsEmit(a.ctx, "state_change", core.StateDisconnected)
}

// GetMetrics возвращает текущие метрики туннеля.
func (a *App) GetMetrics() core.EngineMetrics {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.engine != nil {
		return a.engine.Metrics()
	}
	return core.EngineMetrics{
		State: core.StateDisconnected,
	}
}

// CheckCallHealth проверяет валидность ссылки на звонок без запуска полного туннеля.
func (a *App) CheckCallHealth(link string) (string, error) {
	client := vk.NewHTTPClient("77.88.8.8", vk.DefaultBrowserProfile())
	creds, err := vk.FetchTurnCredentials(context.Background(), client, link)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Звонок активен. Релей: %s", creds.ServerAddr), nil
}
