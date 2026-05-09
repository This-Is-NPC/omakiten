// Package events declares the in-process event bus that downstream
// consumers (hooks engine, future buddies, future TUI live views)
// subscribe to. The bus is panic-safe and synchronous: subscribers run
// on the publisher's goroutine so UI callers can rely on
// "publish returned" meaning "every subscriber observed it" — except
// for hooks, which intentionally fan out to a fire-and-forget goroutine
// from inside their own engine.
package events

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// Filter narrows the events a subscriber wants to receive. EventTypes
// is exact-match string equality; an empty slice matches every type.
// PayloadEq matches top-level JSON keys in domain.Event.Payload (a JSON
// object string) for exact string equality; an empty/nil map matches
// every payload. The two dimensions AND together — both must pass.
type Filter struct {
	EventTypes []string
	PayloadEq  map[string]string
}

// Handler is the subscriber callback. Run on the publisher's goroutine;
// keep it fast and panic-free. Panics are recovered by the bus so a
// rogue handler cannot derail other subscribers.
type Handler func(ctx context.Context, ev domain.Event)

// Subscription is the handle returned from Subscribe. Call Unsubscribe
// once to detach; subsequent calls are no-ops.
type Subscription interface {
	Unsubscribe()
}

// Bus is the abstract event bus. The default in-process implementation
// is created via NewInProcessBus.
type Bus interface {
	Publish(ctx context.Context, ev domain.Event) error
	Subscribe(filter Filter, handler Handler) Subscription
	// SetSettings refreshes the broadcast policy. Call once at composition
	// root; safe to call again on config reload.
	SetSettings(settings config.EventsSettings)
}

type subscription struct {
	id      uint64
	filter  Filter
	handler Handler
	bus     *inProcessBus
	dead    atomic.Bool
}

func (s *subscription) Unsubscribe() {
	if s.dead.Swap(true) {
		return
	}
	s.bus.remove(s.id)
}

type inProcessBus struct {
	mu       sync.RWMutex
	subs     map[uint64]*subscription
	nextID   uint64
	settings config.EventsSettings
}

// NewInProcessBus constructs the default goroutine-local bus. Settings
// gate broadcast per event type; the zero value broadcasts everything.
func NewInProcessBus(settings config.EventsSettings) Bus {
	return &inProcessBus{subs: map[uint64]*subscription{}, settings: settings}
}

func (b *inProcessBus) SetSettings(settings config.EventsSettings) {
	b.mu.Lock()
	b.settings = settings
	b.mu.Unlock()
}

func (b *inProcessBus) Subscribe(filter Filter, handler Handler) Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	sub := &subscription{id: id, filter: filter, handler: handler, bus: b}
	b.subs[id] = sub
	return sub
}

func (b *inProcessBus) remove(id uint64) {
	b.mu.Lock()
	delete(b.subs, id)
	b.mu.Unlock()
}

func (b *inProcessBus) Publish(ctx context.Context, ev domain.Event) error {
	b.mu.RLock()
	if !b.settings.ResolveBroadcast(ev.EventType) {
		b.mu.RUnlock()
		return nil
	}
	matched := b.collectMatches(ev)
	b.mu.RUnlock()

	for _, sub := range matched {
		if sub.dead.Load() {
			continue
		}
		runSafely(ctx, ev, sub.handler)
	}
	return nil
}

// collectMatches snapshots every subscription whose filter accepts ev.
// Caller holds the read lock; the returned slice is owned by the caller
// so handlers can run without keeping the lock.
func (b *inProcessBus) collectMatches(ev domain.Event) []*subscription {
	if len(b.subs) == 0 {
		return nil
	}
	var payload map[string]any
	out := make([]*subscription, 0, len(b.subs))
	for _, sub := range b.subs {
		if !matchEventType(sub.filter.EventTypes, ev.EventType) {
			continue
		}
		if len(sub.filter.PayloadEq) > 0 {
			if payload == nil {
				payload = decodePayload(ev.Payload)
			}
			if !matchPayload(sub.filter.PayloadEq, payload) {
				continue
			}
		}
		out = append(out, sub)
	}
	return out
}

func matchEventType(allowed []string, eventType string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, t := range allowed {
		if t == eventType {
			return true
		}
	}
	return false
}

func matchPayload(want map[string]string, got map[string]any) bool {
	for key, val := range want {
		raw, ok := got[key]
		if !ok {
			return false
		}
		if !payloadStringEq(val, raw) {
			return false
		}
	}
	return true
}

func payloadStringEq(want string, got any) bool {
	switch v := got.(type) {
	case string:
		return v == want
	case bool:
		if want == "true" {
			return v
		}
		if want == "false" {
			return !v
		}
		return false
	case float64:
		// JSON numbers decode as float64; compare via Go's strconv-style
		// formatting so {"count":5} matches `count: "5"` declarations.
		return jsonNumberEq(v, want)
	}
	return false
}

func jsonNumberEq(num float64, want string) bool {
	// Avoid importing strconv just to format — a JSON number that
	// matches an integer string is the only common case worth handling.
	// Cheap path: re-encode through encoding/json and string-compare.
	encoded, err := json.Marshal(num)
	if err != nil {
		return false
	}
	return string(encoded) == want
}

func decodePayload(payload string) map[string]any {
	if payload == "" {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return nil
	}
	return out
}

func runSafely(ctx context.Context, ev domain.Event, h Handler) {
	defer func() {
		_ = recover() // swallow; subscriber owns its own logging
	}()
	h(ctx, ev)
}
