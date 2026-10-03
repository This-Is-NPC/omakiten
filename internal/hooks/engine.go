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
// publisher (UI / CLI / agent request paths).
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

	mu       sync.Mutex
	state    engineState
	ctx      context.Context
	cancel   context.CancelFunc
	sub      events.Subscription
	stopDone chan struct{}
	wg       sync.WaitGroup
}

type engineState uint8

const (
	engineStopped engineState = iota
	engineRunning
	// engineDraining admits no action and lets the admitted ones finish.
	engineDraining
	engineStopping
)

// DefaultShutdownTimeout bounds compatibility callers that use Stop instead
// of supplying their own shutdown context.
const DefaultShutdownTimeout = 5 * time.Second

// GlobalProjectID is the explicit scope used by bootstrap/test runtimes that
// intentionally receive events for every project. A project runtime built
// with a non-zero id must never use this value as an event fallback.
const GlobalProjectID int64 = 0

// NewEngine returns a configured but inactive engine. Call Start to
// subscribe to the bus.
func NewEngine(hooks []Hook, registry *ActionRegistry, settings config.EventsSettings, recorder EventRecorder) *Engine {
	return &Engine{hooks: hooks, registry: registry, settings: settings, recorder: recorder}
}

// SetProjectID scopes the engine's dispatch filter to the supplied
// project id. The composition root (BundleCache.buildProjectRuntime)
// calls this once after construction so events targeting other
// projects skip this engine entirely. Use SetGlobal for an intentional
// projectless/bootstrap scope.
// Safe to call concurrently with dispatch: projectID is atomic.
func (e *Engine) SetProjectID(id int64) {
	e.projectID.Store(id)
}

// SetGlobal makes the engine's catch-all scope explicit for the legacy
// projectless bootstrap path and tests. ProjectRuntime instances created for
// an active project use SetProjectID instead.
func (e *Engine) SetGlobal() {
	e.projectID.Store(GlobalProjectID)
}

// Start subscribes to the bus and creates the cancellation context owned by
// this run of the engine. Repeated and concurrent calls are no-ops while the
// engine is running or draining; a fully stopped engine may be started again.
func (e *Engine) Start(bus events.Bus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state != engineStopped {
		return
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.sub = bus.Subscribe(events.Filter{}, e.dispatch)
	e.state = engineRunning
}

// Stop is the bounded compatibility form of Shutdown. Callers that need to
// propagate a shorter deadline or surface cancellation directly should use
// Shutdown.
func (e *Engine) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()
	return e.Shutdown(ctx)
}

// Drain closes action admission and unsubscribes, lets the admitted actions
// finish on their own until ctx ends, then shuts the engine down,
// cancelling what is left. A process that exits drains, so the hooks of
// its last write still run; a reload shuts down at once.
func (e *Engine) Drain(ctx context.Context) error {
	e.mu.Lock()
	var sub events.Subscription
	if e.state == engineRunning {
		e.state = engineDraining
		sub = e.sub
		e.sub = nil
	}
	e.mu.Unlock()
	if sub != nil {
		sub.Unsubscribe()
	}
	idle := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(idle)
	}()
	select {
	case <-idle:
	case <-ctx.Done():
	}
	return e.Shutdown(ctx)
}

// Shutdown closes action admission, unsubscribes from the bus, cancels every
// admitted action through the Engine-owned context, and waits for the drain.
// The state transition and dispatch's WaitGroup.Add share e.mu, so no Add can
// race with or occur after the drain starts. A context timeout is returned to
// the caller; the engine remains closed and completes its drain asynchronously
// if a non-cooperative action returns later.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	if e.state == engineStopped {
		e.mu.Unlock()
		return nil
	}
	if e.state == engineStopping {
		done := e.stopDone
		e.mu.Unlock()
		return waitForShutdown(ctx, done)
	}

	e.state = engineStopping
	sub := e.sub
	e.sub = nil
	cancel := e.cancel
	done := make(chan struct{})
	e.stopDone = done
	e.mu.Unlock()

	if sub != nil {
		sub.Unsubscribe()
	}
	if cancel != nil {
		cancel()
	}
	go e.finishShutdown(done)
	return waitForShutdown(ctx, done)
}

func (e *Engine) finishShutdown(done chan struct{}) {
	e.wg.Wait()
	e.mu.Lock()
	if e.stopDone == done {
		e.state = engineStopped
		e.ctx = nil
		e.cancel = nil
		e.stopDone = nil
		close(done)
	}
	e.mu.Unlock()
}

func waitForShutdown(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
		e.mu.Lock()
		if e.state != engineRunning {
			e.mu.Unlock()
			return
		}
		lifecycle := e.ctx
		e.wg.Add(1)
		e.mu.Unlock()
		go e.run(lifecycle, ctx, idx, hook, action, ev)
	}
}

func (e *Engine) run(lifecycle, parent context.Context, idx int, hook Hook, action Action, ev domain.Event) {
	defer e.wg.Done()
	// The action keeps the publisher's values (its attribution) but not its
	// deadline or cancellation: an HTTP request ends as soon as its write
	// answers, and the action takes the timeout it chooses (exec defaults
	// to 30s). Only this Engine run's owned context cancels it.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	stopLifecycle := context.AfterFunc(lifecycle, cancel)
	if lifecycle.Err() != nil {
		cancel()
	}
	defer stopLifecycle()
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
// out of the filter so intentionally global system events reach every
// engine. BundleCache emits bundle.imported with the importing runtime's
// non-zero project id; its entity type being system does not make it global.
func (e *Engine) matchesProject(ev domain.Event) bool {
	pid := e.projectID.Load()
	if pid == GlobalProjectID || ev.ProjectID == GlobalProjectID {
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
