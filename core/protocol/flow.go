package protocol

import (
	"encoding/binary"
	"net"
	"sync"
	"time"
)

const (
	// DefaultFlowTTL время жизни привязки потока к воркеру при отсутствии пакетов.
	DefaultFlowTTL = 3 * time.Minute

	// MaxFlowEntries максимальное число одновременных записей в таблице аффинити.
	MaxFlowEntries = 8192
)

// FlowKey вычисляет симметричный 64-битный хеш 5-tuple (SrcIP, DstIP, Proto, SrcPort, DstPort).
// Хеш абсолютно идентичен как для прямого (Client -> Server), так и для обратного (Server -> Client) направления.
func FlowKey(pkt []byte) uint64 {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		// Для не-IPv4 пакетов используем простой FNV-подобный хеш первых байт
		var h uint32
		for i := 0; i < len(pkt) && i < 64; i++ {
			h = h*131 + uint32(pkt[i])
		}
		return uint64(h)
	}

	ihl := int(pkt[0]&0x0f) * 4
	if len(pkt) < ihl {
		return 0
	}

	// Симметричный XOR IP адресов источника и назначения
	srcIP := binary.BigEndian.Uint32(pkt[12:16])
	dstIP := binary.BigEndian.Uint32(pkt[16:20])
	proto := pkt[9]

	h := srcIP ^ dstIP
	h ^= uint32(proto) * 0x9e3779b9

	// Если это TCP (6) или UDP (17), учитываем порты
	if (proto == 6 || proto == 17) && len(pkt) >= ihl+4 {
		p1 := binary.BigEndian.Uint16(pkt[ihl : ihl+2])
		p2 := binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4])

		// Сортировка портов гарантирует симметричность хеша для обоих направлений
		pMin, pMax := p1, p2
		if pMin > pMax {
			pMin, pMax = pMax, pMin
		}

		h ^= (uint32(pMin)<<16 | uint32(pMax)) * 0x9e3779b9
	}

	return uint64(h)
}

// FlowTable управляет привязкой TCP/UDP потоков к конкретным воркерам для анти-джиттера.
type FlowTable[T any] struct {
	mu      sync.RWMutex
	flows   map[uint64]T
	expires map[uint64]int64
	ttl     time.Duration
}

// NewFlowTable создает новую таблицу распределения потоков.
func NewFlowTable[T any](ttl time.Duration) *FlowTable[T] {
	if ttl <= 0 {
		ttl = DefaultFlowTTL
	}
	return &FlowTable[T]{
		flows:   make(map[uint64]T),
		expires: make(map[uint64]int64),
		ttl:     ttl,
	}
}

// Get возвращает воркера, закрепленного за данным 5-tuple хешем, если запись не устарела.
func (t *FlowTable[T]) Get(key uint64) (target T, found bool) {
	now := time.Now().UnixNano()

	t.mu.RLock()
	val, ok := t.flows[key]
	exp, okExp := t.expires[key]
	t.mu.RUnlock()

	if ok && okExp && now <= exp {
		return val, true
	}
	return target, false
}

// Set закрепляет поток за выбранным целевым объектом и обновляет время жизни.
func (t *FlowTable[T]) Set(key uint64, target T) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixNano()
	if len(t.flows) >= MaxFlowEntries {
		t.evictExpiredLocked(now)
	}

	t.flows[key] = target
	t.expires[key] = now + int64(t.ttl)
}

// Touch продлевает время жизни существующей привязки потока.
func (t *FlowTable[T]) Touch(key uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.flows[key]; ok {
		t.expires[key] = time.Now().UnixNano() + int64(t.ttl)
	}
}

// evictExpiredLocked очищает устаревшие потоки при достижении лимита таблицы.
func (t *FlowTable[T]) evictExpiredLocked(now int64) {
	for k, exp := range t.expires {
		if now > exp {
			delete(t.flows, k)
			delete(t.expires, k)
		}
	}
	// Если после очистки устаревших записей места все еще нет, удаляем произвольную запись
	if len(t.flows) >= MaxFlowEntries {
		for k := range t.flows {
			delete(t.flows, k)
			delete(t.expires, k)
			break
		}
	}
}

// IPv4SourceMatches проверяет, соответствует ли IP источника пакета заданному IP-адресу.
func IPv4SourceMatches(pkt []byte, wantIP string) bool {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return false
	}
	expected := net.ParseIP(wantIP).To4()
	if expected == nil {
		return false
	}
	return net.IP(pkt[12:16]).Equal(expected)
}
