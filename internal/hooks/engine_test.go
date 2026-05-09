package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
)

func tru() *bool { v := true; return &v }
func fal() *bool { v := false; return &v }

type recordedEvent struct {
	entityType string
	eventType  string
	payload    string
}

type fakeRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (r *fakeRecorder) RecordEntityEvent(_ context.Context, entityType string, _, _ int64, eventType, payload string) error {
	r.mu.Lock()
	r.events = append(r.events, recordedEvent{entityType, eventType, payload})
	r.mu.Unlock()
	return nil
}

func (r *fakeRecorder) get() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedEvent, len(r.events))
	copy(out, r.events)
	return out
}

type signalAction struct {
	name string
	wg   *sync.WaitGroup
	err  error
	ran  *bool
	mu   *sync.Mutex
}

func (s signalAction) Name() string { return s.name }

func (s signalAction) Execute(_ context.Context, _ domain.Event, _ map[string]any) error {
	s.mu.Lock()
	*s.ran = true
	s.mu.Unlock()
	if s.wg != nil {
		s.wg.Done()
	}
	return s.err
}

func defaultSettings() config.EventsSettings {
	return config.EventsSettings{Defaults: config.EventChannelSettings{Log: tru(), Broadcast: tru(), Hook: tru()}}
}

func TestEngineDispatchesAsyncOnMatch(t *testing.T) {
	registry := NewActionRegistry()
	var wg sync.WaitGroup
	wg.Add(1)
	mu := sync.Mutex{}
	ran := false
	registry.Register(signalAction{name: "test", wg: &wg, ran: &ran, mu: &mu})

	rec := &fakeRecorder{}
	hooks := []Hook{{On: domain.EventTypeGuardViolated, When: map[string]string{"operation": "task.delete"}, Do: "test"}}
	engine := NewEngine(hooks, registry, defaultSettings(), rec)
	bus := events.NewInProcessBus(defaultSettings())
	engine.Start(bus)
	defer engine.Stop()

	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.delete"}`}); err != nil {
		t.Fatalf("Publish = %v", err)
	}
	wg.Wait()
	mu.Lock()
	gotRan := ran
	mu.Unlock()
	if !gotRan {
		t.Fatalf("action did not run")
	}
	// Wait for engine to record hook.executed (post-action).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(rec.get()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := rec.get()
	if len(got) != 1 {
		t.Fatalf("len(events) = %d, want 1 (hook.executed)", len(got))
	}
	if got[0].eventType != domain.EventTypeHookExecuted {
		t.Fatalf("eventType = %q, want hook.executed", got[0].eventType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(got[0].payload), &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if payload["success"] != true {
		t.Fatalf("payload.success = %v, want true", payload["success"])
	}
	if payload["action"] != "test" {
		t.Fatalf("payload.action = %v, want test", payload["action"])
	}
}

func TestEngineSkipsWhenHookGateClosed(t *testing.T) {
	registry := NewActionRegistry()
	mu := sync.Mutex{}
	ran := false
	registry.Register(signalAction{name: "test", ran: &ran, mu: &mu})
	settings := config.EventsSettings{
		Defaults:  config.EventChannelSettings{Log: tru(), Broadcast: tru(), Hook: tru()},
		Overrides: map[string]config.EventChannelSettings{domain.EventTypeGuardViolated: {Hook: fal()}},
	}
	rec := &fakeRecorder{}
	engine := NewEngine([]Hook{{On: domain.EventTypeGuardViolated, Do: "test"}}, registry, settings, rec)
	bus := events.NewInProcessBus(settings)
	engine.Start(bus)
	defer engine.Stop()
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated})
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	gotRan := ran
	mu.Unlock()
	if gotRan {
		t.Fatalf("action ran despite hook gate=false")
	}
	if len(rec.get()) != 0 {
		t.Fatalf("events recorded despite hook gate=false: %v", rec.get())
	}
}

type errorAction struct{ name string }

func (e errorAction) Name() string { return e.name }
func (e errorAction) Execute(_ context.Context, _ domain.Event, _ map[string]any) error {
	return errors.New("boom")
}

func TestEngineEmitsHookExecutedFailureOnError(t *testing.T) {
	registry := NewActionRegistry()
	registry.Register(errorAction{name: "fail"})
	rec := &fakeRecorder{}
	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "fail"}}, registry, defaultSettings(), rec)
	bus := events.NewInProcessBus(defaultSettings())
	engine.Start(bus)
	defer engine.Stop()
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(rec.get()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := rec.get()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 hook.executed", len(got))
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(got[0].payload), &payload)
	if payload["success"] != false {
		t.Fatalf("success = %v, want false", payload["success"])
	}
	if payload["error"] == nil || payload["error"] == "" {
		t.Fatalf("error field empty: %v", payload["error"])
	}
}

func TestEngineDoesNotEmitWhenActionMissing(t *testing.T) {
	registry := NewActionRegistry()
	rec := &fakeRecorder{}
	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "missing"}}, registry, defaultSettings(), rec)
	bus := events.NewInProcessBus(defaultSettings())
	engine.Start(bus)
	defer engine.Stop()
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
	time.Sleep(20 * time.Millisecond)
	if len(rec.get()) != 0 {
		t.Fatalf("hook.executed emitted for missing action: %v", rec.get())
	}
}

func TestEngineMatchesWhenPayload(t *testing.T) {
	registry := NewActionRegistry()
	var wg sync.WaitGroup
	wg.Add(1)
	mu := sync.Mutex{}
	ran := false
	registry.Register(signalAction{name: "match", wg: &wg, ran: &ran, mu: &mu})
	rec := &fakeRecorder{}
	hooks := []Hook{{On: domain.EventTypeGuardViolated, When: map[string]string{"operation": "task.delete"}, Do: "match"}}
	engine := NewEngine(hooks, registry, defaultSettings(), rec)
	bus := events.NewInProcessBus(defaultSettings())
	engine.Start(bus)
	defer engine.Stop()
	// Non-match: different operation.
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.archive"}`})
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	gotRan := ran
	mu.Unlock()
	if gotRan {
		t.Fatalf("action ran on non-matching when payload")
	}
	// Match.
	_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeGuardViolated, Payload: `{"operation":"task.delete"}`})
	wg.Wait()
}
