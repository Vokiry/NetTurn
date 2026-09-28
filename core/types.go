package core

import (
	"time"
)

// ConnectionState отражает текущее состояние жизненного цикла туннеля.
type ConnectionState string

const (
	StateDisconnected    ConnectionState = "DISCONNECTED"
	StateResolvingDNS    ConnectionState = "RESOLVING_DNS"
	StateFetchingVKCreds ConnectionState = "FETCHING_VK_CREDS"
	StateSolvingCaptcha  ConnectionState = "SOLVING_CAPTCHA"
	StateConnectingTURN  ConnectionState = "CONNECTING_TURN"
	StateWorkersInit     ConnectionState = "WORKERS_INIT"
	StateConfiguringTUN  ConnectionState = "CONFIGURING_TUN"
	StateConnected       ConnectionState = "CONNECTED"
	StateReconnecting    ConnectionState = "RECONNECTING"
	StateError           ConnectionState = "ERROR"
)

// PipelinePhase идентифицирует шаги интерактивного конвейера подключения (Connection Pipeline).
type PipelinePhase string

const (
	PhaseDNS       PipelinePhase = "DNS"
	PhaseVKAPI     PipelinePhase = "VK_API"
	PhaseCaptcha   PipelinePhase = "CAPTCHA"
	PhaseWrapAEAD  PipelinePhase = "WRAP_AEAD"
	PhaseTURNRelay PipelinePhase = "TURN_RELAY"
	PhaseWorkers   PipelinePhase = "WORKERS"
	PhaseTUN       PipelinePhase = "TUN_ADAPTER"
	PhaseActive    PipelinePhase = "ACTIVE"
)

// PhaseStatus статус конкретного шага конвейера.
type PhaseStatus string

const (
	PhaseStatusPending PhaseStatus = "PENDING"
	PhaseStatusRunning PhaseStatus = "RUNNING"
	PhaseStatusOK      PhaseStatus = "OK"
	PhaseStatusFailed  PhaseStatus = "FAILED"
)

// PipelineStepEvent описывает событие смены состояния шага конвейера для UI.
type PipelineStepEvent struct {
	Phase   PipelinePhase `json:"phase"`
	Status  PhaseStatus   `json:"status"`
	Message string        `json:"message,omitempty"`
	Time    time.Time     `json:"time"`
}

// ObfsType тип медиа-обфускации RTP.
type ObfsType string

const (
	ObfsAudio ObfsType = "audio" // Opus, PayloadType 111, pad <= 24
	ObfsVideo ObfsType = "video" // VP8, PayloadType 96, pad <= 60
)

// CaptchaMode режим решения капчи.
type CaptchaMode string

const (
	CaptchaModeAuto   CaptchaMode = "auto"   // Level 1: чистый Go (PoW)
	CaptchaModeHybrid CaptchaMode = "hybrid" // Level 1 -> Level 2 Headless
	CaptchaModeManual CaptchaMode = "manual" // Level 3: ручной ввод/диалог
)

// EngineConfig содержит параметры запуска туннеля NetTurn.
type EngineConfig struct {
	// Peer адрес VPS сервера (host:port)
	Peer string `json:"peer"`

	// Password общий ключ аутентификации и деривации WRAP
	Password string `json:"password"`

	// DeviceID уникальный идентификатор устройства клиента
	DeviceID string `json:"device_id"`

	// VKLinks список ссылок на звонки ВКонтакте (https://vk.com/call/join/...)
	VKLinks []string `json:"vk_links"`

	// Workers количество параллельных TURN воркеров (по умолчанию 9, рекомендуется кратно 9)
	Workers int `json:"workers"`

	// Obfs режим маскировки трафика (audio/video)
	Obfs ObfsType `json:"obfs"`

	// CaptchaMode стратегия решения капчи
	CaptchaMode CaptchaMode `json:"captcha_mode"`

	// DNS предпочитаемый DNS сервер внутри туннеля (по умолчанию 1.1.1.1)
	DNS string `json:"dns"`

	// MTU размер виртуального адаптера (по умолчанию 1280)
	MTU int `json:"mtu"`

	// TunName имя создаваемого TUN интерфейса (на Linux, по умолчанию netturn0)
	TunName string `json:"tun_name"`

	// AndroidFd файловый дескриптор Android VpnService (если запуск на Android)
	AndroidFd int `json:"android_fd,omitempty"`

	// LANBridgeEnabled флаг включения локального сетевого моста
	LANBridgeEnabled bool `json:"lan_bridge_enabled"`

	// LANBridgePort порт локального прокси-моста (по умолчанию 24066)
	LANBridgePort int `json:"lan_bridge_port"`

	// TurnTCP принудительное использование TCP для соединения с TURN
	TurnTCP bool `json:"turn_tcp"`
}

// DefaultEngineConfig возвращает конфигурацию по умолчанию.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		Workers:          9,
		Obfs:             ObfsAudio,
		CaptchaMode:      CaptchaModeAuto,
		DNS:              "1.1.1.1",
		MTU:              1280,
		TunName:          "netturn0",
		LANBridgeEnabled: false,
		LANBridgePort:    24066,
		TurnTCP:          false,
	}
}

// EngineMetrics текущие счетчики производительности и статистика сессии.
type EngineMetrics struct {
	State           ConnectionState `json:"state"`
	AssignedIP      string          `json:"assigned_ip"`
	ExternalIP      string          `json:"external_ip,omitempty"`
	UploadBytes     int64           `json:"upload_bytes"`
	DownloadBytes   int64           `json:"download_bytes"`
	UploadRateBps   int64           `json:"upload_rate_bps"`
	DownloadRateBps int64           `json:"download_rate_bps"`
	ActiveWorkers   int             `json:"active_workers"`
	TotalWorkers    int             `json:"total_workers"`
	LatencyMs       int64           `json:"latency_ms"`
	UptimeSeconds   int64           `json:"uptime_seconds"`
}
