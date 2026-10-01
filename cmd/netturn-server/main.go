package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Vokiry/NetTurn/core/crypto"
	"github.com/Vokiry/NetTurn/core/protocol"
	"github.com/Vokiry/NetTurn/core/rawtun"
)

const (
	defaultPort    = "56003"
	defaultTunName = "netturn-raw"
	serverTunIP    = "10.70.66.1"
	serverTunCIDR  = "10.70.66.1/16"
	clientSubnet   = "10.70.0.0/16"
	serverMTU      = 1280
)

type clientSession struct {
	deviceID   string
	remoteAddr net.Addr
	key        []byte
	assignedIP string
	obfsCfg    *protocol.RTPConfig
	obfsState  *protocol.RTPState
	lastSeen   atomic.Int64
}

type Server struct {
	listenAddr string
	password   string
	key        []byte
	tun        rawtun.TunDevice
	conn       net.PacketConn
	sessionsMu sync.RWMutex
	byRemote   map[string]*clientSession
	byIP       map[string][]*clientSession
	ipByDevice map[string]string
	ipCounter  atomic.Uint32
	ctx        context.Context
	cancel     context.CancelFunc
}

func main() {
	listenFlag := flag.String("listen", "0.0.0.0:"+defaultPort, "UDP listen address for WRAP RTP traffic")
	passFlag := flag.String("password", "", "Server main authentication password")
	tunFlag := flag.String("tun", defaultTunName, "TUN interface name on VPS")
	flag.Parse()

	if *passFlag == "" {
		log.Fatalf("Error: -password flag is required")
	}

	key, err := crypto.DeriveKey(*passFlag)
	if err != nil {
		log.Fatalf("Derive key failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &Server{
		listenAddr: *listenFlag,
		password:   *passFlag,
		key:        key,
		byRemote:   make(map[string]*clientSession),
		byIP:       make(map[string][]*clientSession),
		ipByDevice: make(map[string]string),
		ctx:        ctx,
		cancel:     cancel,
	}

	// 1. Создание и настройка TUN интерфейса на VPS
	tunDev, err := srv.setupTun(*tunFlag)
	if err != nil {
		log.Fatalf("Setup TUN failed: %v", err)
	}
	srv.tun = tunDev
	defer srv.tun.Close()

	// 2. Настройка NAT (iptables MASQUERADE) и MSS Clamping
	srv.setupFirewall()

	// 3. Запуск UDP слушателя
	lAddr, err := net.ResolveUDPAddr("udp", *listenFlag)
	if err != nil {
		log.Fatalf("Resolve listen addr: %v", err)
	}

	pc, err := net.ListenUDP("udp", lAddr)
	if err != nil {
		log.Fatalf("Listen UDP: %v", err)
	}
	srv.conn = pc
	defer srv.conn.Close()

	_ = pc.SetReadBuffer(8 * 1024 * 1024)
	_ = pc.SetWriteBuffer(4 * 1024 * 1024)

	log.Printf("[NetTurn Server] Listening on %s, TUN %s (%s)", *listenFlag, *tunFlag, serverTunCIDR)

	// Запуск циклов приема трафика
	go srv.uplinkLoop()
	go srv.downlinkLoop()
	go srv.cleanupStaleSessionsLoop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[NetTurn Server] Shutting down...")
	cancel()
}

func (s *Server) setupTun(name string) (rawtun.TunDevice, error) {
	_ = exec.Command("ip", "link", "del", name).Run()

	tun, err := rawtun.CreateLinuxTun(name, serverMTU)
	if err != nil {
		return nil, err
	}

	for _, args := range [][]string{
		{"addr", "add", serverTunCIDR, "dev", name},
		{"link", "set", "mtu", strconv.Itoa(serverMTU), "dev", name},
		{"link", "set", name, "up"},
	} {
		out, err := exec.Command("ip", args...).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "File exists") {
			return nil, fmt.Errorf("ip %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}

	return tun, nil
}

func (s *Server) setupFirewall() {
	_ = os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)

	_ = exec.Command("iptables", "-t", "nat", "-I", "POSTROUTING", "1", "-s", clientSubnet, "-j", "MASQUERADE").Run()
	_ = exec.Command("iptables", "-I", "FORWARD", "1", "-s", clientSubnet, "-j", "ACCEPT").Run()
	_ = exec.Command("iptables", "-I", "FORWARD", "1", "-d", clientSubnet, "-j", "ACCEPT").Run()
	_ = exec.Command("iptables", "-t", "mangle", "-A", "FORWARD", "-p", "tcp", "--tcp-flags", "SYN,RST", "SYN", "-j", "TCPMSS", "--clamp-mss-to-pmtu").Run()

	log.Println("[NetTurn Server] NAT rules & TCP MSS Clamping applied")
}

func (s *Server) allocateClientIP() string {
	n := s.ipCounter.Add(1)
	b3 := int((n / 254) % 256)
	b4 := int(n%254) + 1
	ip := fmt.Sprintf("10.70.%d.%d", b3, b4)
	if ip == serverTunIP {
		return s.allocateClientIP()
	}
	return ip
}

// uplinkLoop принимает зашифрованные датаграммы от клиентов (через TURN релеи), дешифрует и пишет в TUN.
func (s *Server) uplinkLoop() {
	buf := make([]byte, 2048)
	plainBuf := make([]byte, 2048)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		n, remoteAddr, err := s.conn.ReadFrom(buf)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}

		if n == 0 {
			continue
		}

		raw := buf[:n]

		// Обработка 1-байтового keepalive от клиента (0xFF)
		if n == 1 && raw[0] == 0xFF {
			s.touchSession(remoteAddr)
			_, _ = s.conn.WriteTo([]byte{0xFF}, remoteAddr)
			continue
		}

		// Дешифрация RTP пакета
		decLen, err := protocol.UnwrapPacket(s.key, raw, plainBuf)
		if err != nil {
			continue
		}

		payload := plainBuf[:decLen]
		sess := s.getOrCreateSession(remoteAddr, raw)

		// Проверка служебного запроса согласования RAWCONF
		if strings.HasPrefix(string(payload), "RAWCONF:") {
			s.handleRawConf(remoteAddr, sess, string(payload))
			continue
		}

		// Если это RA-кадр с порядковым номером
		if protocol.IsRAFrame(payload) {
			_, ipPkt, ok := protocol.DecodeRAFrame(payload)
			if ok && len(ipPkt) >= 20 {
				_, _ = s.tun.Write(ipPkt)
			}
			continue
		}

		// Если это сырой IPv4 пакет
		if len(payload) >= 20 && payload[0]>>4 == 4 {
			_, _ = s.tun.Write(payload)
		}
	}
}

func (s *Server) handleRawConf(remote net.Addr, sess *clientSession, req string) {
	parts := strings.Split(strings.TrimPrefix(req, "RAWCONF:"), "|")
	deviceID := "client"
	if len(parts) >= 1 && parts[0] != "" {
		deviceID = parts[0]
	}
	if len(parts) >= 2 {
		pass := parts[1]
		if pass != s.password {
			_, _ = s.conn.WriteTo([]byte("DENIED:wrong_password"), remote)
			return
		}
	}

	sess.deviceID = deviceID

	s.sessionsMu.Lock()
	assigned, exists := s.ipByDevice[deviceID]
	if !exists {
		assigned = s.allocateClientIP()
		s.ipByDevice[deviceID] = assigned
	}
	sess.assignedIP = assigned

	// Добавляем сессию воркера в список воркеров для этого IP
	found := false
	for _, ws := range s.byIP[assigned] {
		if ws == sess {
			found = true
			break
		}
	}
	if !found {
		s.byIP[assigned] = append(s.byIP[assigned], sess)
	}
	s.sessionsMu.Unlock()

	log.Printf("[NetTurn Server] Worker authenticated from %s -> Device %s, Assigned IP %s",
		remote.String(), deviceID, assigned)

	resp := fmt.Sprintf("IP = %s\nDNS = 1.1.1.1\nMTU = %d\nCAPS = CHUNK1\n", assigned, serverMTU)
	wire, err := protocol.WrapPacket(s.key, []byte(resp), sess.obfsCfg, sess.obfsState)
	if err == nil {
		_, _ = s.conn.WriteTo(wire, remote)
	}
}

// downlinkLoop читает обратный трафик из системного TUN и отправляет клиентам в соответствующие TURN-каналы.
func (s *Server) downlinkLoop() {
	buf := make([]byte, 2048)
	var rr atomic.Uint32

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		n, err := s.tun.Read(buf)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			continue
		}

		if n < 20 || buf[0]>>4 != 4 {
			continue
		}

		pkt := buf[:n]
		dstIP := net.IPv4(pkt[16], pkt[17], pkt[18], pkt[19]).String()

		s.sessionsMu.RLock()
		workers := s.byIP[dstIP]
		s.sessionsMu.RUnlock()

		if len(workers) == 0 {
			continue
		}

		// Выбираем активный воркер устройства
		idx := int(rr.Add(1) % uint32(len(workers)))
		targetSess := workers[idx]

		wire, err := protocol.WrapPacket(s.key, pkt, targetSess.obfsCfg, targetSess.obfsState)
		if err == nil && targetSess.remoteAddr != nil {
			wn, wErr := s.conn.WriteTo(wire, targetSess.remoteAddr)
			log.Printf("[NetTurn Server] Downlink to %s -> worker %s: wrote %d bytes (err=%v)",
				dstIP, targetSess.remoteAddr.String(), wn, wErr)
		}
	}
}

func (s *Server) getOrCreateSession(remote net.Addr, firstPacket []byte) *clientSession {
	remoteStr := remote.String()

	s.sessionsMu.RLock()
	sess, ok := s.byRemote[remoteStr]
	s.sessionsMu.RUnlock()

	if ok && sess != nil {
		sess.lastSeen.Store(time.Now().UnixNano())
		return sess
	}

	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	if sess, ok = s.byRemote[remoteStr]; ok && sess != nil {
		sess.lastSeen.Store(time.Now().UnixNano())
		return sess
	}

	obfsCfg := protocol.NewRTPConfig(protocol.ObfsAudio)
	if len(firstPacket) > 1 && (firstPacket[1]&0x7F) == protocol.PayloadTypeVideo {
		obfsCfg = protocol.NewRTPConfig(protocol.ObfsVideo)
	}

	newSess := &clientSession{
		remoteAddr: remote,
		key:        s.key,
		obfsCfg:    obfsCfg,
		obfsState:  protocol.NewRTPState(),
	}
	newSess.lastSeen.Store(time.Now().UnixNano())

	s.byRemote[remoteStr] = newSess
	return newSess
}

func (s *Server) touchSession(remote net.Addr) {
	s.sessionsMu.RLock()
	sess, ok := s.byRemote[remote.String()]
	s.sessionsMu.RUnlock()
	if ok && sess != nil {
		sess.lastSeen.Store(time.Now().UnixNano())
	}
}

func (s *Server) cleanupStaleSessionsLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			cutoff := now.Add(-5 * time.Minute).UnixNano()

			s.sessionsMu.Lock()
			for remote, sess := range s.byRemote {
				if sess.lastSeen.Load() < cutoff {
					delete(s.byRemote, remote)
					if sess.assignedIP != "" {
						list := s.byIP[sess.assignedIP]
						filtered := make([]*clientSession, 0, len(list))
						for _, item := range list {
							if item != sess {
								filtered = append(filtered, item)
							}
						}
						if len(filtered) == 0 {
							delete(s.byIP, sess.assignedIP)
							delete(s.ipByDevice, sess.deviceID)
						} else {
							s.byIP[sess.assignedIP] = filtered
						}
					}
				}
			}
			s.sessionsMu.Unlock()
		}
	}
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	_, _ = io.ReadFull(rand.Reader, b)
	return hex.EncodeToString(b)
}
