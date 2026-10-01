package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Vokiry/NetTurn/core"
	"github.com/Vokiry/NetTurn/core/eventbus"
	"github.com/Vokiry/NetTurn/core/network"
)

const banner = `
  _   _      _  _____                 
 | \ | | ___| ||_   _|   _ _ __ _ __  
 |  \| |/ _ \ __|| || | | | '__| '_ \ 
 | |\  |  __/ |_ | || |_| | |  | | | |
 |_| \_|\___|\__||_| \__,_|_|  |_| |_|
 Next-Gen VK Calls TURN Media Tunnel
`

func main() {
	peerFlag := flag.String("peer", "", "VPS server address (e.g. 1.2.3.4:56003)")
	passFlag := flag.String("password", "", "Connection password / shared secret")
	vkLinkFlag := flag.String("vk-link", "", "VK call invite link (https://vk.com/call/join/...)")
	workersFlag := flag.Int("workers", 9, "Number of parallel TURN workers (default 9)")
	obfsFlag := flag.String("obfs", "audio", "Obfuscation mode: audio (Opus) or video (VP8)")
	tunFlag := flag.String("tun", "netturn0", "Name of TUN interface (Linux)")
	dnsFlag := flag.String("dns", "", "Preferred DNS server (empty for system DNS)")
	routesFlag := flag.Bool("routes", true, "Automatically apply system routing rules on Linux")
	flag.Parse()

	fmt.Print(banner)

	if *peerFlag == "" || *passFlag == "" || *vkLinkFlag == "" {
		fmt.Println("Usage:")
		fmt.Println("  netturn-client -peer <ip:port> -password <pass> -vk-link <link> [options]")
		fmt.Println()
		fmt.Println("Required flags:")
		fmt.Println("  -peer      VPS server address (e.g. 198.51.100.1:56003)")
		fmt.Println("  -password  Connection password")
		fmt.Println("  -vk-link   VK Call invite link")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -workers   Number of TURN workers (default: 9)")
		fmt.Println("  -obfs      Obfuscation mode: audio (default) or video")
		fmt.Println("  -tun       TUN interface name (default: netturn0)")
		fmt.Println("  -dns       DNS server (default: 77.88.8.8)")
		fmt.Println("  -routes    Apply routing rules (default: true)")
		os.Exit(1)
	}

	cfg := core.DefaultEngineConfig()
	cfg.Peer = *peerFlag
	cfg.Password = *passFlag
	cfg.VKLinks = []string{*vkLinkFlag}
	cfg.Workers = *workersFlag
	cfg.TunName = *tunFlag
	cfg.DNS = *dnsFlag
	if strings.ToLower(*obfsFlag) == "video" {
		cfg.Obfs = core.ObfsVideo
	} else {
		cfg.Obfs = core.ObfsAudio
	}

	bus := eventbus.NewBus()

	// Подписка на этапы Connection Pipeline
	bus.Subscribe("pipeline", func(ev any) {
		step, ok := ev.(core.PipelineStepEvent)
		if !ok {
			return
		}
		var icon string
		switch step.Status {
		case core.PhaseStatusOK:
			icon = "\033[32m[✓]\033[0m"
		case core.PhaseStatusRunning:
			icon = "\033[33m[⋯]\033[0m"
		case core.PhaseStatusFailed:
			icon = "\033[31m[✗]\033[0m"
		default:
			icon = "[ ]"
		}
		fmt.Printf("%s \033[1m%-12s\033[0m %s\n", icon, step.Phase, step.Message)
	})

	// Подписка на обновление метрик в реальном времени
	bus.Subscribe("metrics", func(ev any) {
		m, ok := ev.(core.EngineMetrics)
		if !ok || m.State != core.StateConnected {
			return
		}

		upRateFmt := formatSpeed(m.UploadRateBps)
		downRateFmt := formatSpeed(m.DownloadRateBps)
		uptimeFmt := formatUptime(m.UptimeSeconds)

		fmt.Printf("\r\033[K\033[36m▲ %s\033[0m  \033[32m▼ %s\033[0m | Воркеры: %d/%d | Время: %s",
			upRateFmt, downRateFmt, m.ActiveWorkers, m.TotalWorkers, uptimeFmt)
	})

	engine := core.NewEngine(cfg, bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Перехват сигналов завершения (Ctrl+C, SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	routeMgr := network.NewRouteManager(*tunFlag)

	// Запуск туннеля
	go func() {
		if err := engine.Start(ctx); err != nil {
			fmt.Printf("\n\033[31m[!] Ошибка запуска туннеля:\033[0m %v\n", err)
			cancel()
			return
		}

		if *routesFlag {
			_ = routeMgr.ProtectIP(*peerFlag)
			if err := routeMgr.EnableTunnelRoutes(); err != nil {
				fmt.Printf("\n\033[33m[!] Предупреждение маршрутизации (требуются права root/cap_net_admin):\033[0m %v\n", err)
			}
		}
	}()

	select {
	case <-sigChan:
		fmt.Println("\n\nПолучен сигнал завершения. Остановка туннеля...")
	case <-ctx.Done():
	}

	if *routesFlag {
		routeMgr.DisableTunnelRoutes()
	}
	engine.Stop()
	fmt.Println("Туннель остановлен. До свидания!")
}

func formatSpeed(bytesPerSec int64) string {
	bitsPerSec := float64(bytesPerSec * 8)
	if bitsPerSec >= 1_000_000 {
		return fmt.Sprintf("%.1f Mbps", bitsPerSec/1_000_000)
	}
	return fmt.Sprintf("%.1f Kbps", bitsPerSec/1_000)
}

func formatUptime(seconds int64) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
