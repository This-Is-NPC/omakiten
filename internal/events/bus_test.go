package events

import (
	"context"
	"sync"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func tru() *bool {
	v := true
	return &v
}

func fal() *bool {
	v := false
	return &v
}

func defaultSettings() config.EventsSettings {
	return config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: tru(), Broadcast: tru(), Hook: tru()},
	}
}

func TestBusPublishRoundTrip(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	got := []string{}
	var mu sync.Mutex
	bus.Subscribe(Filter{}, func(_ context.Context, ev domain.Event) {
		mu.Lock()
		got = append(got, ev.EventType)
		mu.Unlock()
	})
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated}); err != nil {
		t.Fatalf("Publish = %v", err)
	}
	if len(got) != 1 || got[0] != domain.EventTypeTaskCreated {
		t.Fatalf("got = %v, want one task.created", got)
	}
}

func TestBusFilterByEventType(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	got := []string{}
	bus.Subscribe(Filter{EventTypes: []string{domain.EventTypeTaskCreated}}, func(_ context.Context, ev domain.Event) {
		got = append(got, ev.EventType)
	})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskMoved})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	if len(got) != 1 || got[0] != domain.EventTypeTaskCreated {
		t.Fatalf("got = %v, want only task.created", got)
	}
}

func TestBusFilterByPayloadEq(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	got := []string{}
	bus.Subscribe(Filter{PayloadEq: map[string]string{"operation": "task.delete"}}, func(_ context.Context, ev domain.Event) {
		got = append(got, ev.EventType)
	})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.archive"}`})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.delete"}`})
	if len(got) != 1 {
		t.Fatalf("got = %v, want 1 match", got)
	}
}

func TestBusFilterCombinedAND(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	count := 0
	bus.Subscribe(Filter{
		EventTypes: []string{domain.EventTypeGuardViolated},
		PayloadEq:  map[string]string{"operation": "task.delete"},
	}, func(_ context.Context, _ domain.Event) {
		count++
	})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.archive"}`})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, Payload: `{"operation":"task.delete"}`})
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.delete"}`})
	if count != 1 {
		t.Fatalf("count = %d, want 1 (AND of type+payload)", count)
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	var a, b int
	bus.Subscribe(Filter{}, func(_ context.Context, _ domain.Event) { a++ })
	bus.Subscribe(Filter{}, func(_ context.Context, _ domain.Event) { b++ })
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	if a != 1 || b != 1 {
		t.Fatalf("a=%d b=%d, want both 1", a, b)
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	count := 0
	sub := bus.Subscribe(Filter{}, func(_ context.Context, _ domain.Event) { count++ })
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	sub.Unsubscribe()
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	if count != 1 {
		t.Fatalf("count = %d, want 1 (unsubscribe should silence subsequent publishes)", count)
	}
	sub.Unsubscribe() // idempotent
}

func TestBusPanicInHandlerRecovered(t *testing.T) {
	bus := NewInProcessBus(defaultSettings())
	calmCount := 0
	bus.Subscribe(Filter{}, func(_ context.Context, _ domain.Event) {
		panic("boom")
	})
	bus.Subscribe(Filter{}, func(_ context.Context, _ domain.Event) {
		calmCount++
	})
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated}); err != nil {
		t.Fatalf("Publish should not surface handler panics, got %v", err)
	}
	if calmCount != 1 {
		t.Fatalf("calm subscriber should still observe; got %d", calmCount)
	}
}

func TestBusBroadcastGate(t *testing.T) {
	settings := config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: tru(), Broadcast: tru(), Hook: tru()},
		Overrides: map[string]config.EventChannelSettings{
			domain.EventTypeTaskCreated: {Broadcast: fal()},
		},
	}
	bus := NewInProcessBus(settings)
	count := 0
	bus.Subscribe(Filter{}, func(_ context.Context, _ domain.Event) { count++ })
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	if count != 0 {
		t.Fatalf("count = %d, want 0 (broadcast gated by config)", count)
	}
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskMoved})
	if count != 1 {
		t.Fatalf("count = %d, want 1 (other event types still broadcast)", count)
	}
}
