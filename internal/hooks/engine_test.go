package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
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
	name    string
	wg      *sync.WaitGroup
	err     error
	ran     *bool
	mu      *sync.Mutex
	counter *int
	events  *[]domain.Event
}

func (s signalAction) Name() string { return s.name }

func (s signalAction) Execute(_ context.Context, _ domain.Event, _ map[string]any) error {
	return s.execute(domain.Event{})
}

func (s signalAction) execute(ev domain.Event) error {
	s.mu.Lock()
	if s.ran != nil {
		*s.ran = true
	}
	if s.counter != nil {
		*s.counter++
	}
	if s.events != nil {
		*s.events = append(*s.events, ev)
	}
	s.mu.Unlock()
	if s.wg != nil {
		s.wg.Done()
	}
	return s.err
}

type captureAction struct{ signalAction }

func (s captureAction) Execute(_ context.Context, ev domain.Event, _ map[string]any) error {
	return s.execute(ev)
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

// TestEngineFiltersByProjectID asserts the Phase 3d isolation rule: an
// engine scoped to project A must never dispatch on an event scoped to
// project B. The catch-all zero-id mode is also covered — both
// engines reach the same bus, but only the project-A engine reacts to
// the project-A event, only the project-B engine reacts to the
// project-B event, and a system event (projectID==0) reaches both.
func TestEngineFiltersByProjectID(t *testing.T) {
	bus := events.NewInProcessBus(defaultSettings())

	var aWG, bWG sync.WaitGroup
	aMu, bMu := sync.Mutex{}, sync.Mutex{}
	aRan, bRan := false, false

	regA := NewActionRegistry()
	regA.Register(signalAction{name: "test", wg: &aWG, ran: &aRan, mu: &aMu})
	regB := NewActionRegistry()
	regB.Register(signalAction{name: "test", wg: &bWG, ran: &bRan, mu: &bMu})

	engineA := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "test"}}, regA, defaultSettings(), &fakeRecorder{})
	engineA.SetProjectID(1)
	engineA.Start(bus)
	defer engineA.Stop()

	engineB := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "test"}}, regB, defaultSettings(), &fakeRecorder{})
	engineB.SetProjectID(2)
	engineB.Start(bus)
	defer engineB.Stop()

	// Event for project A: only engineA should fire.
	aWG.Add(1)
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, ProjectID: 1}); err != nil {
		t.Fatalf("Publish A: %v", err)
	}
	aWG.Wait()
	time.Sleep(20 * time.Millisecond) // give B a chance to mis-fire
	bMu.Lock()
	if bRan {
		bMu.Unlock()
		t.Fatal("engineB fired on project-A event — cross-talk")
	}
	bMu.Unlock()

	// Reset, then send project-B event.
	aMu.Lock()
	aRan = false
	aMu.Unlock()
	bWG.Add(1)
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, ProjectID: 2}); err != nil {
		t.Fatalf("Publish B: %v", err)
	}
	bWG.Wait()
	time.Sleep(20 * time.Millisecond)
	aMu.Lock()
	if aRan {
		aMu.Unlock()
		t.Fatal("engineA fired on project-B event — cross-talk")
	}
	aMu.Unlock()
}

// TestEngineWithZeroProjectIDCatchesAll asserts the backward-compat
// rule: a project-unscoped engine (the bootstrap window before the
// composition root resolved a project id) keeps seeing every event
// regardless of its ProjectID.
func TestEngineWithZeroProjectIDCatchesAll(t *testing.T) {
	bus := events.NewInProcessBus(defaultSettings())

	registry := NewActionRegistry()
	var wg sync.WaitGroup
	wg.Add(2)
	mu := sync.Mutex{}
	count := 0
	registry.Register(signalAction{name: "test", wg: &wg, ran: nil, mu: &mu, counter: &count})

	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "test"}}, registry, defaultSettings(), &fakeRecorder{})
	engine.SetGlobal()
	engine.Start(bus)
	defer engine.Stop()

	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, ProjectID: 1}); err != nil {
		t.Fatalf("Publish 1: %v", err)
	}
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, ProjectID: 2}); err != nil {
		t.Fatalf("Publish 2: %v", err)
	}
	wg.Wait()
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 2 {
		t.Fatalf("catch-all engine count = %d, want 2", got)
	}
}

// TestEngineSystemEventReachesScopedEngine pins the second branch of
// matchesProject: a project-scoped engine still receives events
// emitted with ProjectID==0 (system events like bundle.swapped /
// hook.executed). The catch-all rule lives on the event side, not
// just on the engine side.
func TestEngineSystemEventReachesScopedEngine(t *testing.T) {
	bus := events.NewInProcessBus(defaultSettings())

	registry := NewActionRegistry()
	var wg sync.WaitGroup
	mu := sync.Mutex{}
	count := 0
	registry.Register(signalAction{name: "test", wg: &wg, ran: nil, mu: &mu, counter: &count})

	engine := NewEngine([]Hook{{On: domain.EventTypeBundleSwapped, Do: "test"}}, registry, defaultSettings(), &fakeRecorder{})
	engine.SetProjectID(42)
	engine.Start(bus)
	defer engine.Stop()

	wg.Add(1)
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeBundleSwapped, ProjectID: 0}); err != nil {
		t.Fatalf("Publish system event: %v", err)
	}
	wg.Wait()
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 1 {
		t.Fatalf("scoped engine count for system event = %d, want 1 (catch-all on event-side)", got)
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

func TestEngineFiltersHooksBySubjectDepth(t *testing.T) {
	bus := events.NewInProcessBus(defaultSettings())
	registry := NewActionRegistry()
	rootMu, subMu := sync.Mutex{}, sync.Mutex{}
	rootEvents := []domain.Event{}
	subEvents := []domain.Event{}
	var rootWG, subWG sync.WaitGroup
	registry.Register(captureAction{signalAction{name: "root", wg: &rootWG, mu: &rootMu, events: &rootEvents}})
	registry.Register(captureAction{signalAction{name: "sub", wg: &subWG, mu: &subMu, events: &subEvents}})
	engine := NewEngine([]Hook{
		{On: domain.EventTypeTaskCreated, Do: "root", SubjectDepth: SubjectDepthRoot, ResolvedKit: "root"},
		{On: domain.EventTypeTaskCreated, Do: "sub", SubjectDepth: SubjectDepthSubtask, ResolvedKit: "sub"},
		{On: domain.EventTypeTaskMoved, Do: "sub", SubjectDepth: SubjectDepthSubtask, ResolvedKit: "sub"},
		{On: domain.EventTypeTaskBucketOrphaned, Do: "sub", SubjectDepth: SubjectDepthSubtask, ResolvedKit: "sub"},
		// #301 finding A4: guard.violated payloads now carry
		// subject_depth + resolved_kit so the engine routes a sub-task
		// violation to the sub-kit hook only.
		{On: domain.EventTypeGuardViolated, Do: "sub", SubjectDepth: SubjectDepthSubtask, ResolvedKit: "sub"},
		{On: domain.EventTypeGuardViolated, Do: "root", SubjectDepth: SubjectDepthRoot, ResolvedKit: "root"},
	}, registry, defaultSettings(), &fakeRecorder{})
	engine.Start(bus)
	defer engine.Stop()

	assertRootDepthHook(t, bus, &rootWG, &rootMu, &subMu, &rootEvents, &subEvents)
	assertSubtaskDepthHooks(t, bus, &subWG, &subMu, &rootMu, &rootEvents, &subEvents)
}

func assertRootDepthHook(t *testing.T, bus events.Bus, wg *sync.WaitGroup, rootMu, subMu *sync.Mutex, rootEvents, subEvents *[]domain.Event) {
	t.Helper()
	wg.Add(1)
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, Payload: `{"subject_task_id":1,"subject_depth":0,"resolved_kit":"root"}`}); err != nil {
		t.Fatalf("Publish root created = %v", err)
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	rootMu.Lock()
	gotRoot := len(*rootEvents)
	rootMu.Unlock()
	subMu.Lock()
	gotSub := len(*subEvents)
	subMu.Unlock()
	if gotRoot != 1 {
		t.Fatalf("root hook events = %d, want 1", gotRoot)
	}
	if gotSub != 0 {
		t.Fatalf("sub hook fired for root task: %+v", *subEvents)
	}
}

func assertSubtaskDepthHooks(t *testing.T, bus events.Bus, wg *sync.WaitGroup, subMu, rootMu *sync.Mutex, rootEvents, subEvents *[]domain.Event) {
	t.Helper()
	publish := func(eventType, payload string) {
		wg.Add(1)
		if err := bus.Publish(context.Background(), domain.Event{EventType: eventType, Payload: payload}); err != nil {
			t.Fatalf("Publish %s = %v", eventType, err)
		}
		wg.Wait()
	}
	publish(domain.EventTypeTaskCreated, `{"subject_task_id":2,"subject_parent_id":1,"subject_depth":1,"resolved_kit":"sub"}`)
	subMu.Lock()
	gotSubCreated := len(*subEvents)
	subMu.Unlock()
	if gotSubCreated != 1 {
		t.Fatalf("sub hook events after sub create = %d, want 1", gotSubCreated)
	}
	publish(domain.EventTypeTaskMoved, `{"subject_task_id":2,"subject_parent_id":1,"subject_depth":1,"resolved_kit":"sub"}`)
	publish(domain.EventTypeTaskBucketOrphaned, `{"subject_task_id":2,"subject_parent_id":1,"subject_depth":1,"resolved_kit":"sub"}`)
	publish(domain.EventTypeGuardViolated, `{"operation":"task.archive","rule":"subtasks_complete","subject_task_id":2,"subject_parent_id":1,"subject_depth":1,"resolved_kit":"sub"}`)
	subMu.Lock()
	gotSubTotal := len(*subEvents)
	subMu.Unlock()
	if gotSubTotal != 4 {
		t.Fatalf("sub hook total events = %d, want 4 (created + moved + orphaned + guard.violated)", gotSubTotal)
	}
	rootMu.Lock()
	gotRoot := len(*rootEvents)
	rootMu.Unlock()
	if gotRoot != 1 {
		t.Fatalf("root hook events after sub guard.violated = %d, want 1 (root.created only)", gotRoot)
	}
}

func TestEngineRootHookWithoutSubtaskKitMatchesAnyDepth(t *testing.T) {
	bus := events.NewInProcessBus(defaultSettings())
	registry := NewActionRegistry()
	var wg sync.WaitGroup
	mu := sync.Mutex{}
	count := 0
	registry.Register(signalAction{name: "root", wg: &wg, mu: &mu, counter: &count})
	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "root", SubjectDepth: SubjectDepthAny, ResolvedKit: "root"}}, registry, defaultSettings(), &fakeRecorder{})
	engine.Start(bus)
	defer engine.Stop()

	wg.Add(2)
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, Payload: `{"subject_task_id":1,"subject_depth":0,"resolved_kit":"root"}`}); err != nil {
		t.Fatalf("Publish root = %v", err)
	}
	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated, Payload: `{"subject_task_id":2,"subject_parent_id":1,"subject_depth":1,"resolved_kit":"root"}`}); err != nil {
		t.Fatalf("Publish sub without sub-kit = %v", err)
	}
	wg.Wait()
	mu.Lock()
	got := count
	mu.Unlock()
	if got != 2 {
		t.Fatalf("root hook count = %d, want 2", got)
	}
}

type cancelBlockingAction struct {
	name     string
	started  chan<- struct{}
	finished chan<- struct{}
}

func (a cancelBlockingAction) Name() string { return a.name }

func (a cancelBlockingAction) Execute(ctx context.Context, _ domain.Event, _ map[string]any) error {
	a.started <- struct{}{}
	<-ctx.Done()
	a.finished <- struct{}{}
	return ctx.Err()
}

type nonCooperativeAction struct {
	name     string
	started  chan<- struct{}
	release  <-chan struct{}
	finished chan<- struct{}
}

func (a nonCooperativeAction) Name() string { return a.name }

func (a nonCooperativeAction) Execute(_ context.Context, _ domain.Event, _ map[string]any) error {
	a.started <- struct{}{}
	<-a.release
	a.finished <- struct{}{}
	return nil
}

func TestEngineShutdownCancelsAdmittedAction(t *testing.T) {
	started := make(chan struct{}, 1)
	finished := make(chan struct{}, 1)
	registry := NewActionRegistry()
	registry.Register(cancelBlockingAction{name: "block", started: started, finished: finished})
	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "block"}}, registry, defaultSettings(), nil)
	bus := events.NewInProcessBus(defaultSettings())
	engine.Start(bus)

	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Shutdown returned before the canceled action finished")
	}
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

func TestEngineShutdownTimesOutForNonCooperativeAction(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{}, 1)
	registry := NewActionRegistry()
	registry.Register(nonCooperativeAction{name: "stubborn", started: started, release: release, finished: finished})
	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "stubborn"}}, registry, defaultSettings(), nil)
	bus := events.NewInProcessBus(defaultSettings())
	engine.Start(bus)

	if err := bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	err := engine.Shutdown(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context deadline exceeded", err)
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("non-cooperative action did not finish after release")
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), time.Second)
	defer drainCancel()
	if err := engine.Shutdown(drainCtx); err != nil {
		t.Fatalf("Shutdown after release: %v", err)
	}
}

type staleCallbackBus struct {
	handler events.Handler
	ready   chan<- struct{}
	release <-chan struct{}
}

func (b *staleCallbackBus) Publish(ctx context.Context, ev domain.Event) error {
	b.ready <- struct{}{}
	<-b.release
	b.handler(ctx, ev)
	return nil
}

func (b *staleCallbackBus) Subscribe(_ events.Filter, handler events.Handler) events.Subscription {
	b.handler = handler
	return inertSubscription{}
}

func (*staleCallbackBus) SetSettings(config.EventsSettings) {}

type inertSubscription struct{}

func (inertSubscription) Unsubscribe() {}

type countingAction struct {
	count *atomic.Int64
}

func (countingAction) Name() string { return "count" }

func (a countingAction) Execute(context.Context, domain.Event, map[string]any) error {
	a.count.Add(1)
	return nil
}

func TestEngineShutdownRejectsStaleCallbacksWithoutAddWaitRace(t *testing.T) {
	const iterations = 1000
	var admitted atomic.Int64
	for i := 0; i < iterations; i++ {
		ready := make(chan struct{}, 1)
		release := make(chan struct{})
		bus := &staleCallbackBus{ready: ready, release: release}
		registry := NewActionRegistry()
		registry.Register(countingAction{count: &admitted})
		engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "count"}}, registry, defaultSettings(), nil)
		engine.Start(bus)

		published := make(chan struct{})
		go func() {
			_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
			close(published)
		}()
		<-ready

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := engine.Shutdown(ctx); err != nil {
			cancel()
			t.Fatalf("iteration %d Shutdown: %v", i, err)
		}
		cancel()
		close(release)
		<-published
	}
	runtime.Gosched()
	if got := admitted.Load(); got != 0 {
		t.Fatalf("actions admitted after shutdown = %d, want 0", got)
	}
}

func TestEngineStartAndShutdownAreIdempotentUnderConcurrency(t *testing.T) {
	registry := NewActionRegistry()
	var count atomic.Int64
	registry.Register(countingAction{count: &count})
	engine := NewEngine([]Hook{{On: domain.EventTypeTaskCreated, Do: "count"}}, registry, defaultSettings(), nil)
	bus := events.NewInProcessBus(defaultSettings())

	const cycles = 100
	for cycle := 0; cycle < cycles; cycle++ {
		var starts sync.WaitGroup
		starts.Add(8)
		for range 8 {
			go func() {
				defer starts.Done()
				engine.Start(bus)
			}()
		}
		starts.Wait()

		ready := make(chan struct{})
		var calls sync.WaitGroup
		calls.Add(16)
		for range 8 {
			go func() {
				defer calls.Done()
				<-ready
				_ = bus.Publish(context.Background(), domain.Event{EventType: domain.EventTypeTaskCreated})
			}()
			go func() {
				defer calls.Done()
				<-ready
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := engine.Shutdown(ctx); err != nil {
					t.Errorf("cycle %d Shutdown: %v", cycle, err)
				}
			}()
		}
		close(ready)
		calls.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := engine.Shutdown(ctx); err != nil {
			cancel()
			t.Fatalf("cycle %d final Shutdown: %v", cycle, err)
		}
		cancel()
	}
}
