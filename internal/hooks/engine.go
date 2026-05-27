package hooks

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
)

// EventRecorder is the narrow port the engine uses to emit
// hook.executed. The runtime supplies the sqlite Store; tests can
// substitute an in-memory recorder.
type EventRecorder interface {
	RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, payload string) error
}

// Engine wires the configured hooks to the events bus. Subscriber
// callback runs synchronously on the publisher's goroutine to inspect
// matches; matched hooks then dispatch their action on a dedicated
// goroutine (fire-and-forget) so slow scripts cannot block the
// publisher (UI / CLI / MCP request paths).
type Engine struct {
	hooks    []Hook
	registry *ActionRegistry
	settings config.EventsSettings
	recorder EventRecorder
	// projectID scopes which events this engine reacts to. Phase 3d
	// runs one engine per ProjectRuntime so two projects' hooks never
	// cross-fire. The filter rules:
	//   - engine projectID == 0  -> catch-all (bootstrap / tests)
	//   - event projectID == 0   -> system event, reaches every engine
	//   - otherwise              -> engine.projectID must equal event.ProjectID
	// atomic.Int64 so SetProjectID is safe to call from a different
	// goroutine than dispatch (the composition root sets it before
	// Start, but the contract should not rely on caller ordering).
	projectID atomic.Int64

	mu  sync.Mutex
	sub events.Subscription
	wg  sync.WaitGroup
}

// NewEngine returns a configured but inactive engine. Call Start to
// subscribe to the bus.
func NewEngine(hooks []Hook, registry *ActionRegistry, settings config.EventsSettings, recorder EventRecorder) *Engine {
	return &Engine{hooks: hooks, registry: registry, settings: settings, recorder: recorder}
}

// SetProjectID scopes the engine's dispatch filter to the supplied
// project id. The composition root (BundleCache.buildProjectRuntime)
// calls this once after construction so events targeting other
// projects skip this engine entirely. Zero disables the filter —
// engines built before the composition root resolves a project id
// (bootstrap window, tests) keep receiving every event the bus emits.
// Safe to call concurrently with dispatch: projectID is atomic.
func (e *Engine) SetProjectID(id int64) {
	e.projectID.Store(id)
}

// Start subscribes to the bus. Idempotent: a second call is a no-op
// while the previous subscription is still alive. Stop releases it.
func (e *Engine) Start(bus events.Bus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sub != nil {
		return
	}
	e.sub = bus.Subscribe(events.Filter{}, e.dispatch)
}

// Stop releases the bus subscription and waits for in-flight actions
// to settle. Safe to call multiple times.
func (e *Engine) Stop() {
	e.mu.Lock()
	if e.sub != nil {
		e.sub.Unsubscribe()
		e.sub = nil
	}
	e.mu.Unlock()
	e.wg.Wait()
}

// dispatch runs on the publisher's goroutine. It walks the configured
// hooks for matches and spawns a goroutine per match; never blocks.
func (e *Engine) dispatch(ctx context.Context, ev domain.Event) {
	if !e.matchesProject(ev) {
		return
	}
	if !e.settings.ResolveHook(ev.EventType) {
		return
	}
	for idx, hook := range e.hooks {
		if !matchesSubjectDepth(hook, ev) {
			continue
		}
		if !matches(hook, ev) {
			continue
		}
		action, ok := e.registry.Get(hook.Do)
		if !ok {
			continue
		}
		e.wg.Add(1)
		go e.run(ctx, idx, hook, action, ev)
	}
}

func (e *Engine) run(parent context.Context, idx int, hook Hook, action Action, ev domain.Event) {
	defer e.wg.Done()
	// Detach from the parent's deadline so the hook gets the timeout
	// each action chooses (exec defaults to 30s). We keep cancellation
	// linked so app shutdown still propagates.
	ctx, cancel := context.WithCancel(detachDeadline(parent))
	defer cancel()

	start := time.Now()
	var execErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = panicErr(r)
			}
		}()
		execErr = action.Execute(ctx, ev, hook.Args)
	}()
	duration := time.Since(start)

	payload := map[string]any{
		"hook_index":      idx,
		"action":          hook.Do,
		"event_type":      ev.EventType,
		"target_event_id": ev.ID,
		"resolved_kit":    hook.ResolvedKit,
		"success":         execErr == nil,
		"duration_ms":     duration.Milliseconds(),
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if e.recorder == nil {
		return
	}
	_ = e.recorder.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, ev.ProjectID, domain.EventTypeHookExecuted, string(body))
}

// matchesProject decides whether the engine should consider the event.
// Phase 3 scopes one engine per ProjectRuntime, so a non-zero
// engine.projectID is the per-project filter and a non-zero
// ev.ProjectID identifies the event's owner. Zero on either side opts
// out of the filter so system events (ev.ProjectID == 0 — e.g.
// bundle.swapped, hook.executed) reach every engine and engines built
// for cross-project dispatch (engine.projectID == 0 — used by tests
// and the bootstrap window before a project resolves) keep receiving
// everything.
func (e *Engine) matchesProject(ev domain.Event) bool {
	pid := e.projectID.Load()
	if pid == 0 || ev.ProjectID == 0 {
		return true
	}
	return pid == ev.ProjectID
}

func matches(hook Hook, ev domain.Event) bool {
	if hook.On != "" && hook.On != ev.EventType {
		return false
	}
	if len(hook.When) == 0 {
		return true
	}
	payload := decodePayload(ev.Payload)
	for key, want := range hook.When {
		got, ok := payload[key]
		if !ok {
			return false
		}
		if !payloadStringEq(want, got) {
			return false
		}
	}
	return true
}

func matchesSubjectDepth(hook Hook, ev domain.Event) bool {
	switch hook.SubjectDepth {
	case SubjectDepthRoot:
		depth, ok := subjectDepth(ev)
		return !ok || depth == 0
	case SubjectDepthSubtask:
		depth, ok := subjectDepth(ev)
		return ok && depth >= 1
	default:
		return true
	}
}

// subjectDepth extracts the event subject's depth from its JSON payload.
// json.Unmarshal decodes every numeric JSON value into float64, so the
// type switch only needs the float64 arm (the previous `case int:` arm
// was dead — review finding §C.12 of #297).
func subjectDepth(ev domain.Event) (int, bool) {
	payload := decodePayload(ev.Payload)
	if payload == nil {
		return 0, false
	}
	if v, ok := payload["subject_depth"].(float64); ok {
		return int(v), true
	}
	return 0, false
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
		encoded, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return string(encoded) == want
	}
	return false
}

func panicErr(r any) error {
	if err, ok := r.(error); ok {
		return err
	}
	return panicValue{value: r}
}

type panicValue struct{ value any }

func (p panicValue) Error() string {
	return "action panicked"
}

// detachDeadline returns a context that inherits parent's cancellation
// but drops its deadline so the action's own timeout can take effect
// without being clipped by an unrelated request deadline.
func detachDeadline(parent context.Context) context.Context {
	return noDeadlineCtx{Context: parent}
}

type noDeadlineCtx struct{ context.Context }

func (noDeadlineCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
