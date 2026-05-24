package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
)

// busSink wires a Subscribe(all) handler that records every published
// Event into a slice — the spec for txMutateAndEmit asserts on the
// EXACT bus traffic the helper produces, so we want a synchronous,
// goroutine-safe receiver the tests can introspect after the
// helper returns.
type busSink struct {
	mu       sync.Mutex
	received []domain.Event
}

func attachBusSink(t *testing.T, store *storeFixture) *busSink {
	t.Helper()
	tru := true
	settings := config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: &tru, Broadcast: &tru, Hook: &tru},
	}
	store.SetEventsPolicy(settings)
	bus := events.NewInProcessBus(settings)
	store.SetEventBus(bus)

	sink := &busSink{}
	sub := bus.Subscribe(events.Filter{}, func(_ context.Context, ev domain.Event) {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		sink.received = append(sink.received, ev)
	})
	t.Cleanup(sub.Unsubscribe)
	return sink
}

func (b *busSink) snapshot() []domain.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]domain.Event, len(b.received))
	copy(out, b.received)
	return out
}

// txTestRow is the trivial generic payload threaded through the
// helper's Mutate → Payload → EntityID/Body chain. The id field flows
// out of an INSERT RETURNING in the entity-scope test and is hardcoded
// for the rollback/no-row tests where Mutate never reaches a write.
type txTestRow struct {
	id   int64
	body string
}

func setupTxEvent(t *testing.T) (context.Context, *storeFixture, domain.ProjectContext, *busSink) {
	t.Helper()
	ctx, store, project := setupPlans(t)
	sink := attachBusSink(t, store)
	return ctx, store, project, sink
}

// TestTxMutateAndEmit_EmitsBothScopes pins the happy path for both
// EventScopeEntity (insertEntityEvent → 'plan'/'task' rows) and
// EventScopeTask (insertTaskEvent → 'task' rows with body honoured).
// Each scope must persist exactly one event row AND publish exactly
// one envelope to the bus.
func TestTxMutateAndEmit_EmitsBothScopes(t *testing.T) {
	ctx, store, project, sink := setupTxEvent(t)
	// Reuse CreatePlan's downstream side effects to provision a task
	// the task-scope helper can attribute its event to.
	plan, err := store.CreatePlan(ctx, project.ID, "tx-plan", "Plan", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "TaskScope", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// CreatePlan + CreateTask both publish; drain the recorded
	// envelopes so this test's assertions read the helper's emissions
	// only.
	sink.mu.Lock()
	sink.received = nil
	sink.mu.Unlock()

	// Entity-scope: write a plan.goal_edited row tied to plan.ID.
	entityOut, err := txMutateAndEmit(ctx, store.Store, TxMutation[txTestRow]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanGoalEdited,
		ProjectID:  project.ID,
		EntityID:   func(r txTestRow) int64 { return r.id },
		Mutate: func(ctx context.Context, tx *sql.Tx) (txTestRow, error) {
			return txTestRow{id: plan.ID}, nil
		},
		Payload: func(_ txTestRow) (string, error) {
			return `{"length":42}`, nil
		},
	})
	if err != nil {
		t.Fatalf("entity-scope txMutateAndEmit: %v", err)
	}
	if entityOut.id != plan.ID {
		t.Fatalf("entity-scope returned id = %d, want %d", entityOut.id, plan.ID)
	}

	// Task-scope: write a task.created row with a body string.
	taskOut, err := txMutateAndEmit(ctx, store.Store, TxMutation[txTestRow]{
		Scope:     EventScopeTask,
		EventType: domain.EventTypeTaskMoved,
		ProjectID: project.ID,
		EntityID:  func(r txTestRow) int64 { return r.id },
		Body:      func(r txTestRow) string { return r.body },
		Mutate: func(ctx context.Context, tx *sql.Tx) (txTestRow, error) {
			return txTestRow{id: task.ID, body: "task-body"}, nil
		},
		Payload: func(_ txTestRow) (string, error) {
			return `{"from":"backlog","to":"dev"}`, nil
		},
	})
	if err != nil {
		t.Fatalf("task-scope txMutateAndEmit: %v", err)
	}
	if taskOut.id != task.ID {
		t.Fatalf("task-scope returned id = %d, want %d", taskOut.id, task.ID)
	}

	// Bus should have observed exactly 2 envelopes (one per scope).
	got := sink.snapshot()
	if len(got) != 2 {
		t.Fatalf("bus published %d envelopes after both scopes, want 2: %+v", len(got), got)
	}
	if got[0].EventType != domain.EventTypePlanGoalEdited || got[0].EntityType != domain.EventEntityPlan {
		t.Fatalf("first envelope = %+v, want plan.goal_edited / plan", got[0])
	}
	if got[1].EventType != domain.EventTypeTaskMoved || got[1].EntityType != domain.EventEntityTask {
		t.Fatalf("second envelope = %+v, want task.moved / task", got[1])
	}

	// Events table should carry both rows.
	planRows, err := store.ListRecentEvents(ctx, domain.EventTypePlanGoalEdited, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents plan.goal_edited: %v", err)
	}
	if len(planRows) != 1 {
		t.Fatalf("plan.goal_edited rows = %d, want 1", len(planRows))
	}
	taskRows, err := store.ListRecentEvents(ctx, domain.EventTypeTaskMoved, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents task.moved: %v", err)
	}
	if len(taskRows) != 1 {
		t.Fatalf("task.moved rows = %d, want 1", len(taskRows))
	}
}

// TestTxMutateAndEmit_RollbackOnMutateFailure asserts that when Mutate
// returns an error, the helper neither inserts an event row nor
// publishes — the surrounding transaction's deferred Rollback wipes
// every write before the function returns.
func TestTxMutateAndEmit_RollbackOnMutateFailure(t *testing.T) {
	ctx, store, project, sink := setupTxEvent(t)
	sink.mu.Lock()
	sink.received = nil
	sink.mu.Unlock()

	wantErr := errors.New("mutate boom")
	_, err := txMutateAndEmit(ctx, store.Store, TxMutation[txTestRow]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanCreated,
		ProjectID:  project.ID,
		EntityID:   func(r txTestRow) int64 { return r.id },
		Mutate: func(_ context.Context, _ *sql.Tx) (txTestRow, error) {
			return txTestRow{}, wantErr
		},
		Payload: func(_ txTestRow) (string, error) {
			return `{}`, nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}

	if got := sink.snapshot(); len(got) != 0 {
		t.Fatalf("bus published %d envelopes after Mutate failure, want 0", len(got))
	}
	rows, err := store.ListRecentEvents(ctx, domain.EventTypePlanCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("plan.created rows = %d, want 0 (rollback should have wiped them)", len(rows))
	}
}

// TestTxMutateAndEmit_RollbackOnEventInsertFailure asserts that when
// insertEntityEvent itself fails (here: the helper's inserter receives
// an entity_type='' value which the helper passes through; instead of
// relying on schema quirks we force the failure by cancelling the
// helper's context AFTER Mutate succeeds — QueryRowContext for the
// events INSERT then bails out with context.Canceled and the
// surrounding transaction rolls back).
func TestTxMutateAndEmit_RollbackOnEventInsertFailure(t *testing.T) {
	parent, store, project, sink := setupTxEvent(t)
	sink.mu.Lock()
	sink.received = nil
	sink.mu.Unlock()

	if _, err := store.CreatePlan(parent, project.ID, "ev-fail", "Plan", ""); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	sink.mu.Lock()
	sink.received = nil
	sink.mu.Unlock()

	cancelCtx, cancel := context.WithCancel(parent)
	defer cancel()

	var sentinelTitle = "txevent-rollback-canary"
	_, err := txMutateAndEmit(cancelCtx, store.Store, TxMutation[txTestRow]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanCreated,
		ProjectID:  project.ID,
		EntityID:   func(r txTestRow) int64 { return r.id },
		Mutate: func(ctx context.Context, tx *sql.Tx) (txTestRow, error) {
			// Write a task row that should disappear on rollback.
			row := tx.QueryRowContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id)
VALUES (?, NULL, ?, '', 2)
RETURNING id
`, project.ID, sentinelTitle)
			var id int64
			if err := row.Scan(&id); err != nil {
				return txTestRow{}, err
			}
			// Cancel mid-flight so the subsequent event-INSERT bails.
			cancel()
			return txTestRow{id: id}, nil
		},
		Payload: func(_ txTestRow) (string, error) {
			return `{}`, nil
		},
	})
	if err == nil {
		t.Fatal("expected event-insert failure, got nil")
	}

	// The canary task must NOT be on disk — rollback wiped it.
	var stillThere int
	if err := store.db.QueryRowContext(parent, `SELECT COUNT(*) FROM tasks WHERE project_id = ? AND title = ?`, project.ID, sentinelTitle).Scan(&stillThere); err != nil {
		t.Fatalf("count canary rows: %v", err)
	}
	if stillThere != 0 {
		t.Fatalf("canary task rows = %d, want 0 (rollback should have wiped Mutate's INSERT)", stillThere)
	}

	if got := sink.snapshot(); len(got) != 0 {
		t.Fatalf("bus published %d envelopes after event-insert failure, want 0", len(got))
	}
}

// TestTxMutateAndEmit_PublishesOnlyOnCommit pins the post-commit
// invariant: when ANY step between BeginTx and tx.Commit fails, the
// helper must NOT publish — subscribers can rely on "publish observed
// ⇒ row committed". Verified via the Payload-returns-error path
// (analogous to a JSON marshal failure surfaced before the event row
// hits disk): the helper returns the error, the deferred Rollback
// fires, and the bus stays silent.
//
// The strict commit-time-failure case (insert succeeded → Commit
// errors) is exercised indirectly: the helper's flow only publishes
// after `committed = true` is set, and the deferred Rollback fires on
// any non-commit return path. Forcing Commit to fail on a real
// SQLite engine requires deferred-constraint plumbing that the rest
// of the package does not depend on; the Payload-error path covers
// the same publish-iff-commit invariant from the opposite direction.
func TestTxMutateAndEmit_PublishesOnlyOnCommit(t *testing.T) {
	ctx, store, project, sink := setupTxEvent(t)
	sink.mu.Lock()
	sink.received = nil
	sink.mu.Unlock()

	wantErr := errors.New("payload marshal failed")
	_, err := txMutateAndEmit(ctx, store.Store, TxMutation[txTestRow]{
		Scope:      EventScopeEntity,
		EntityType: domain.EventEntityPlan,
		EventType:  domain.EventTypePlanCreated,
		ProjectID:  project.ID,
		EntityID:   func(r txTestRow) int64 { return r.id },
		Mutate: func(ctx context.Context, tx *sql.Tx) (txTestRow, error) {
			// Real write that should disappear when Payload errors.
			_, mutErr := tx.ExecContext(ctx, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id)
VALUES (?, NULL, 'publish-gate-canary', '', 2)
`, project.ID)
			if mutErr != nil {
				return txTestRow{}, mutErr
			}
			return txTestRow{id: 1}, nil
		},
		Payload: func(_ txTestRow) (string, error) {
			return "", wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}

	if got := sink.snapshot(); len(got) != 0 {
		t.Fatalf("bus published %d envelopes after payload failure, want 0", len(got))
	}

	// Mutate's INSERT must have been rolled back.
	var stillThere int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE project_id = ? AND title = 'publish-gate-canary'`, project.ID).Scan(&stillThere); err != nil {
		t.Fatalf("count canary rows: %v", err)
	}
	if stillThere != 0 {
		t.Fatalf("canary task rows = %d, want 0 (rollback should have wiped Mutate's INSERT)", stillThere)
	}
}
