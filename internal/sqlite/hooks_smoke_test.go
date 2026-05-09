package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
)

// TestStoreHookExecutedSmoke wires the full path: Store emits an event
// to a real bus, the hooks engine matches and dispatches the exec
// action async, the script writes the event JSON to a file, and the
// engine then records hook.executed via Store.RecordEntityEvent. Asserts
// hook.executed lands AFTER the script wrote (post-action), with
// success=true.
func TestStoreHookExecutedSmoke(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	scriptOut := filepath.Join(tmp, "stdin.json")

	store, project := openStoreWithProject(ctx, t)
	tru := true
	settings := config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
	}
	store.SetEventsPolicy(settings)
	bus := events.NewInProcessBus(settings)
	store.SetEventBus(bus)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	hookEntries := []hooks.Hook{{
		On: domain.EventTypeTaskCreated,
		Do: "exec",
		Args: map[string]any{
			"argv":       []any{"sh", "-c", "cat > " + scriptOut},
			"timeout_ms": 5000,
		},
	}}
	engine := hooks.NewEngine(hookEntries, registry, settings, store)
	engine.Start(bus)
	defer engine.Stop()

	// Trigger a real task.created emit through the Store.
	if _, err := store.CreateTask(ctx, project.ID, "smoke", "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	// Wait for the script to finish writing.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(scriptOut); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(scriptOut); err != nil {
		t.Fatalf("script never wrote stdin file: %v", err)
	}

	// Wait for hook.executed to be recorded (engine emits AFTER action returns).
	var hookEvents []domain.Event
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.ListRecentEvents(ctx, domain.EventTypeHookExecuted, 10)
		if err != nil {
			t.Fatalf("ListRecentEvents = %v", err)
		}
		if len(got) > 0 {
			hookEvents = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(hookEvents) == 0 {
		t.Fatalf("hook.executed never recorded")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(hookEvents[0].Payload), &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if payload["success"] != true {
		t.Fatalf("payload.success = %v, want true", payload["success"])
	}
	if payload["action"] != "exec" {
		t.Fatalf("payload.action = %v, want exec", payload["action"])
	}
	if payload["event_type"] != domain.EventTypeTaskCreated {
		t.Fatalf("payload.event_type = %v, want task.created", payload["event_type"])
	}
}

// TestStoreHookGateClosedSkipsDispatch confirms the engine respects the
// per-event-type Hook channel: with hook=false in overrides, the engine
// must not dispatch and must not emit hook.executed.
func TestStoreHookGateClosedSkipsDispatch(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	tru := true
	fal := false
	settings := config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
		Overrides: map[string]config.EventChannelSettings{
			domain.EventTypeTaskCreated: {Hook: &fal},
		},
	}
	store.SetEventsPolicy(settings)
	bus := events.NewInProcessBus(settings)
	store.SetEventBus(bus)

	registry := hooks.NewActionRegistry()
	registry.Register(actions.Noop{})
	var ran sync.Map
	registry.Register(testAction{ran: &ran})
	hookEntries := []hooks.Hook{{On: domain.EventTypeTaskCreated, Do: "test"}}
	engine := hooks.NewEngine(hookEntries, registry, settings, store)
	engine.Start(bus)
	defer engine.Stop()

	if _, err := store.CreateTask(ctx, project.ID, "smoke", "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := ran.Load("test"); ok {
		t.Fatalf("action ran despite hook gate=false")
	}
	got, _ := store.ListRecentEvents(ctx, domain.EventTypeHookExecuted, 5)
	if len(got) != 0 {
		t.Fatalf("hook.executed recorded despite hook gate=false: %v", got)
	}
}

type testAction struct{ ran *sync.Map }

func (testAction) Name() string { return "test" }
func (a testAction) Execute(_ context.Context, _ domain.Event, _ map[string]any) error {
	a.ran.Store("test", true)
	return nil
}
