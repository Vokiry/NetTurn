package turn

import (
	"net"
	"time"
)

// WorkerState отражает текущее состояние отдельного TURN-воркера.
type WorkerState string

const (
	WorkerStateIdle         WorkerState = "IDLE"
	WorkerStateConnecting   WorkerState = "CONNECTING"
	WorkerStateAllocated    WorkerState = "ALLOCATED"
	WorkerStateActive       WorkerState = "ACTIVE"
	WorkerStateReconnecting WorkerState = "RECONNECTING"
	WorkerStateClosed       WorkerState = "CLOSED"
	WorkerStateError        WorkerState = "ERROR"
)

// WorkerStats статистика и метрики отдельного воркера.
type WorkerStats struct {
	ID           int         `json:"id"`
	State        WorkerState `json:"state"`
	RelayAddr    string      `json:"relay_addr"`
	BytesSent    int64       `json:"bytes_sent"`
	BytesRecv    int64       `json:"bytes_recv"`
	PacketsSent  int64       `json:"packets_sent"`
	PacketsRecv  int64       `json:"packets_recv"`
	LastActivity time.Time   `json:"last_activity"`
}

// PacketHandler функция обратного вызова при приеме расшифрованного полезного IP-пакета.
type PacketHandler func(packet []byte, workerID int)

// Config конфигурация пула воркеров TURN.
type Config struct {
	NumWorkers     int
	PeerAddr       *net.UDPAddr
	Password       string
	UseTCP         bool
	AllocateGateMs int // интервал каскадного запуска воркеров (по умолчанию 150 мс)
}
