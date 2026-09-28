package eventbus

import (
	"sync"
)

// Handler функция обработки событий определенного типа.
type Handler func(event any)

// Subscription дескриптор подписки для отписки.
type Subscription struct {
	topic string
	id    int
	bus   *Bus
}

// Unsubscribe отменяет регистрацию обработчика.
func (s *Subscription) Unsubscribe() {
	if s.bus != nil {
		s.bus.unsubscribe(s.topic, s.id)
		s.bus = nil
	}
}

// Bus потокобезопасная шина событий для реактивной передачи метрик и событий конвейера.
type Bus struct {
	mu          sync.RWMutex
	handlers    map[string]map[int]Handler
	nextID      int
	isBroadcast bool
}

// NewBus создает новый экземпляр шины событий.
func NewBus() *Bus {
	return &Bus{
		handlers: make(map[string]map[int]Handler),
	}
}

// Subscribe подписывается на указанный топик событий.
func (b *Bus) Subscribe(topic string, handler Handler) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := b.nextID

	if _, exists := b.handlers[topic]; !exists {
		b.handlers[topic] = make(map[int]Handler)
	}
	b.handlers[topic][id] = handler

	return &Subscription{
		topic: topic,
		id:    id,
		bus:   b,
	}
}

func (b *Bus) unsubscribe(topic string, id int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if topicHandlers, exists := b.handlers[topic]; exists {
		delete(topicHandlers, id)
		if len(topicHandlers) == 0 {
			delete(b.handlers, topic)
		}
	}
}

// Publish отправляет событие всем зарегистрированным подписчикам топика в неблокирующем режиме.
func (b *Bus) Publish(topic string, event any) {
	b.mu.RLock()
	handlersList := make([]Handler, 0)
	if topicHandlers, exists := b.handlers[topic]; exists {
		for _, h := range topicHandlers {
			handlersList = append(handlersList, h)
		}
	}
	b.mu.RUnlock()

	for _, h := range handlersList {
		go h(event)
	}
}
