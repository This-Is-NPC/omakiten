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
	if _, err := store.CreateTask(ctx, project.ID, "smoke", "", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	waitForHookScript(t, scriptOut)
	assertHookExecutedPayload(t, waitForHookExecuted(t, ctx, store))
}

func waitForHookScript(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("script never wrote stdin file: %v", err)
	}
}

func waitForHookExecuted(t *testing.T, ctx context.Context, store *storeFixture) domain.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.ListRecentEvents(ctx, domain.EventTypeHookExecuted, 10)
		if err != nil {
			t.Fatalf("ListRecentEvents = %v", err)
		}
		if len(got) > 0 {
			return got[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("hook.executed never recorded")
	return domain.Event{}
}

func assertHookExecutedPayload(t *testing.T, event domain.Event) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
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

	if _, err := store.CreateTask(ctx, project.ID, "smoke", "", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
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

// TestActivityLogFinishFiresToolCallHook is the regression for the
// Phase 1 contract that hooks can subscribe to `cli.tool_call` (or
// cli/tui) and filter on payload fields populated by FinishActivityLog.
// Pre-#109 the activity log path bypassed the events bus entirely so
// hooks never fired on tool calls; pre-019 the event_type catch-all
// `operation` could not be subscribed to per-source.
func TestActivityLogFinishFiresToolCallHook(t *testing.T) {
	ctx := context.Background()
	store, _ := openStoreWithProject(ctx, t)
	tru := true
	settings := config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
	}
	store.SetEventsPolicy(settings)
	bus := events.NewInProcessBus(settings)
	store.SetEventBus(bus)

	registry := hooks.NewActionRegistry()
	var ran sync.Map
	registry.Register(testAction{ran: &ran})
	hookEntries := []hooks.Hook{
		{
			On:   domain.EventTypeTUIToolCall,
			When: map[string]string{"tool_name": "tasks.create"},
			Do:   "test",
		},
		{
			On:   domain.EventTypeTUIToolCall,
			When: map[string]string{"tool_name": "tasks.delete"},
			Do:   "test",
		},
	}
	engine := hooks.NewEngine(hookEntries, registry, settings, store)
	engine.Start(bus)
	defer engine.Stop()

	id, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceTUI,
		Entrypoint:    "tools/call",
		Operation:     "tasks.create",
		ProjectID:     1,
		ArgumentsJSON: `{"title":"Hi"}`,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog = %v", err)
	}
	if err := store.FinishActivityLog(ctx, id, "ok", 42, ""); err != nil {
		t.Fatalf("FinishActivityLog = %v", err)
	}

	// Wait for the matching hook to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ran.Load("test"); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := ran.Load("test"); !ok {
		t.Fatalf("matching hook (tool_name=tasks.create) never fired")
	}

	// The non-matching hook (tool_name=tasks.delete) should NOT have
	// dispatched the action; both hooks share the action key so we
	// instead assert via hook.executed event count: exactly one
	// hook.executed for the match, zero for the miss.
	deadline = time.Now().Add(1 * time.Second)
	var executed []domain.Event
	for time.Now().Before(deadline) {
		got, _ := store.ListRecentEvents(ctx, domain.EventTypeHookExecuted, 10)
		if len(got) >= 1 {
			executed = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(executed) != 1 {
		t.Fatalf("hook.executed count = %d, want 1 (the tasks.delete hook must not match)", len(executed))
	}
}
