package api

import (
	"sync"
	"time"
)

// Event types streamed over the events websocket.
const (
	eventTypeOperation = "operation"
	eventTypeLogging   = "logging"
)

// event is a single typed message published to events subscribers.
type event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Metadata  any       `json:"metadata"`
}

// eventsHub fans out events to all connected subscribers. Each subscriber has a
// buffered channel and an optional set of types it wants; a subscriber whose
// buffer is full is skipped for that event (slow consumers drop messages rather
// than block the publisher), matching LXD's best-effort delivery.
type eventsHub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

type subscriber struct {
	ch    chan event
	types map[string]bool // nil/empty means all types
}

func newEventsHub() *eventsHub {
	return &eventsHub{subs: map[*subscriber]struct{}{}}
}

// subscribe registers a subscriber for the given event types (empty = all) and
// returns it along with an unsubscribe function.
func (h *eventsHub) subscribe(types []string) (*subscriber, func()) {
	set := map[string]bool{}
	for _, t := range types {
		set[t] = true
	}
	sub := &subscriber{ch: make(chan event, 64), types: set}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub, func() {
		h.mu.Lock()
		delete(h.subs, sub)
		h.mu.Unlock()
	}
}

// wants reports whether the subscriber wants events of the given type.
func (s *subscriber) wants(eventType string) bool {
	if len(s.types) == 0 {
		return true
	}
	return s.types[eventType]
}

// broadcast delivers an event to every interested subscriber, dropping it for
// any subscriber whose buffer is full.
func (h *eventsHub) broadcast(e event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if !sub.wants(e.Type) {
			continue
		}
		select {
		case sub.ch <- e:
		default:
			// Slow consumer; drop this event for them.
		}
	}
}

// log publishes a logging event with a level and message.
func (h *eventsHub) log(level, message string) {
	h.broadcast(event{
		Type:      eventTypeLogging,
		Timestamp: time.Now(),
		Metadata: map[string]any{
			"level":   level,
			"message": message,
		},
	})
}
