package vk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNoCallsAvailable = errors.New("vk: no available or healthy VK calls in pool")
)

type callEntry struct {
	hash         string
	consecErrors int
	lastUsed     time.Time
	isBlacklist  bool
}

// CallPool управляет множеством ссылок на звонки VK, кэширует креденшелы и производит авторотацию.
type CallPool struct {
	mu          sync.RWMutex
	client      *HTTPClient
	calls       []*callEntry
	credsCache  map[string]*TurnCredentials
	cacheMu     sync.RWMutex
	currIndex   int
	maxErrors   int
	cooldownTTL time.Duration
}

// NewCallPool инициализирует пул ссылок на звонки VK.
func NewCallPool(client *HTTPClient, links []string) *CallPool {
	entries := make([]*callEntry, 0, len(links))
	for _, l := range links {
		clean := CleanCallHash(l)
		if clean != "" {
			entries = append(entries, &callEntry{
				hash: clean,
			})
		}
	}

	return &CallPool{
		client:      client,
		calls:       entries,
		credsCache:  make(map[string]*TurnCredentials),
		maxErrors:   3,
		cooldownTTL: 5 * time.Minute,
	}
}

// AddLink добавляет новую ссылку в пул.
func (p *CallPool) AddLink(link string) {
	clean := CleanCallHash(link)
	if clean == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.calls {
		if entry.hash == clean {
			entry.isBlacklist = false
			entry.consecErrors = 0
			return
		}
	}

	p.calls = append(p.calls, &callEntry{hash: clean})
}

// GetTurnCredentials возвращает актуальные TURN-креденшелы, используя кэш или запрашивая новые.
func (p *CallPool) GetTurnCredentials(ctx context.Context) (*TurnCredentials, error) {
	entry, err := p.pickNextHealthy()
	if err != nil {
		return nil, err
	}

	// 1. Проверяем кэш
	p.cacheMu.RLock()
	cached, ok := p.credsCache[entry.hash]
	p.cacheMu.RUnlock()

	if ok && cached != nil && !cached.IsExpired() {
		return cached, nil
	}

	// 2. Запрашиваем свежие креденшелы
	creds, err := FetchTurnCredentials(ctx, p.client, entry.hash)
	if err != nil {
		p.recordFailure(entry.hash)
		return nil, fmt.Errorf("pool fetch failed for hash %s: %w", entry.hash, err)
	}

	// 3. Сохраняем в кэш
	p.cacheMu.Lock()
	p.credsCache[entry.hash] = creds
	p.cacheMu.Unlock()

	p.recordSuccess(entry.hash)
	return creds, nil
}

func (p *CallPool) pickNextHealthy() (*callEntry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.calls)
	if n == 0 {
		return nil, ErrNoCallsAvailable
	}

	now := time.Now()
	// Пробуем найти здоровый звонок
	for i := 0; i < n; i++ {
		idx := (p.currIndex + i) % n
		entry := p.calls[idx]

		// Если звонок был временно заблокирован, проверяем время остывания
		if entry.isBlacklist && now.Sub(entry.lastUsed) > p.cooldownTTL {
			entry.isBlacklist = false
			entry.consecErrors = 0
		}

		if !entry.isBlacklist {
			p.currIndex = (idx + 1) % n
			entry.lastUsed = now
			return entry, nil
		}
	}

	return nil, ErrNoCallsAvailable
}

func (p *CallPool) recordFailure(hash string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.calls {
		if entry.hash == hash {
			entry.consecErrors++
			if entry.consecErrors >= p.maxErrors {
				entry.isBlacklist = true
			}
			return
		}
	}
}

func (p *CallPool) recordSuccess(hash string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, entry := range p.calls {
		if entry.hash == hash {
			entry.consecErrors = 0
			entry.isBlacklist = false
			return
		}
	}
}
