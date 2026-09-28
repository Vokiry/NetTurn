package network

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var (
	ErrNoDefaultGateway = errors.New("network: default physical gateway not found")
)

// GatewayInfo содержит параметры физического шлюза по умолчанию.
type GatewayInfo struct {
	Interface string
	GatewayIP net.IP
}

// FindPhysicalDefaultGateway парсит системную таблицу /proc/net/route ядра Linux и находит физический шлюз.
func FindPhysicalDefaultGateway() (*GatewayInfo, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, fmt.Errorf("open /proc/net/route: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		iface := fields[0]
		destHex := fields[1]
		gwHex := fields[2]
		maskHex := fields[7]

		// Игнорируем виртуальные туннельные интерфейсы
		if strings.HasPrefix(iface, "netturn") || strings.HasPrefix(iface, "tun") || strings.HasPrefix(iface, "wg") {
			continue
		}

		// Дефолтный маршрут имеет Destination == 00000000 и Mask == 00000000
		if destHex == "00000000" && maskHex == "00000000" && gwHex != "00000000" {
			gwBytes, err := hex.DecodeString(gwHex)
			if err != nil || len(gwBytes) != 4 {
				continue
			}

			// В /proc/net/route IP хранится в little-endian формате
			gwIP := net.IPv4(gwBytes[3], gwBytes[2], gwBytes[1], gwBytes[0])

			return &GatewayInfo{
				Interface: iface,
				GatewayIP: gwIP,
			}, nil
		}
	}

	return nil, ErrNoDefaultGateway
}

// RouteManager управляет маршрутами на Linux Desktop, предотвращая петли маршрутизации.
type RouteManager struct {
	mu           sync.Mutex
	gwInfo       *GatewayInfo
	tunName      string
	protectedIPs []string
	isActive     bool
}

// NewRouteManager создает новый менеджер маршрутизации для интерфейса tunName.
func NewRouteManager(tunName string) *RouteManager {
	return &RouteManager{
		tunName:      tunName,
		protectedIPs: make([]string, 0),
	}
}

// ProtectIP добавляет исключение /32 через физический шлюз до указанного IP адреса (VPS или TURN-релея).
func (m *RouteManager) ProtectIP(ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gwInfo == nil {
		gw, err := FindPhysicalDefaultGateway()
		if err != nil {
			return err
		}
		m.gwInfo = gw
	}

	cleanIP := strings.TrimSpace(ip)
	if idx := strings.Index(cleanIP, ":"); idx != -1 {
		cleanIP = cleanIP[:idx]
	}

	// ip route replace <IP>/32 via <GatewayIP> dev <Interface>
	cmd := exec.Command("ip", "route", "replace", cleanIP+"/32", "via", m.gwInfo.GatewayIP.String(), "dev", m.gwInfo.Interface)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("protect route %s: %w (%s)", cleanIP, err, strings.TrimSpace(string(out)))
	}

	m.protectedIPs = append(m.protectedIPs, cleanIP)
	return nil
}

// EnableTunnelRoutes включает перехват всего интернет-трафика через префиксы 0.0.0.0/1 и 128.0.0.0/1.
func (m *RouteManager) EnableTunnelRoutes() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		cmd := exec.Command("ip", "route", "replace", prefix, "dev", m.tunName)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("enable tunnel route %s: %w (%s)", prefix, err, strings.TrimSpace(string(out)))
		}
	}

	m.isActive = true
	return nil
}

// DisableTunnelRoutes удаляет туннельные префиксы и возвращает маршрутизацию в исходное состояние.
func (m *RouteManager) DisableTunnelRoutes() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isActive {
		return
	}

	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		_ = exec.Command("ip", "route", "del", prefix, "dev", m.tunName).Run()
	}

	// Удаляем защищенные /32 исключения
	for _, ip := range m.protectedIPs {
		_ = exec.Command("ip", "route", "del", ip+"/32").Run()
	}
	m.protectedIPs = nil
	m.isActive = false
}

// ParseIPv4 parses a dotted-decimal IPv4 address into uint32 big-endian.
func ParseIPv4(ipStr string) (uint32, error) {
	ip := net.ParseIP(strings.TrimSpace(ipStr)).To4()
	if ip == nil {
		return 0, errors.New("invalid IPv4 address")
	}
	return binary.BigEndian.Uint32(ip), nil
}
