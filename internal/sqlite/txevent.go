package sqlite

import (
	"context"
	"database/sql"

	"omakiten/internal/domain"
)

// EventScope selects whether the lifecycle helper persists the emitted
// row through insertEntityEvent (plan / non-task entities) or
// insertTaskEvent (task feed). Both routes share the same
// BeginTx → mutate → emit → Commit → publish lifecycle; the scope only
// differs in which inserter writes the events row and which fields
// (Body for task scope; EntityType for entity scope) carry meaning.
type EventScope int

const (
	// EventScopeEntity persists the event via insertEntityEvent with the
	// caller-supplied EntityType (plan / task / system). Body is unused.
	EventScopeEntity EventScope = iota
	// EventScopeTask persists the event via insertTaskEvent against the
	// 'task' entity_type. EntityType on the mutation is ignored — the
	// inserter hardcodes it to 'task'. Body is honoured.
	EventScopeTask
)

// TxMutation describes one BeginTx → mutate → emit → Commit → publish
// cycle. The generic T threads the mutated entity through Mutate →
// Payload → EntityID/Body so derived event fields read the post-mutation
// row (id from RETURNING, server-stamped timestamps, etc.) without a
// follow-up SELECT.
//
// Scope picks the inserter. EntityType only matters for
// EventScopeEntity. Body only matters for EventScopeTask. Mutate runs
// inside the helper's transaction; Payload runs after Mutate but before
// the event insert so callers can fail-fast on JSON marshal errors and
// have the helper roll the mutation back. The event publish is
// post-commit by construction — subscribers never observe a
// rolled-back row.
type TxMutation[T any] struct {
	// Scope selects insertEntityEvent vs insertTaskEvent.
	Scope EventScope
	// EntityType is the entity_type column written for EventScopeEntity
	// (e.g. "plan", "task"). Ignored for EventScopeTask, which hardcodes
	// "task" inside insertTaskEvent.
	EntityType string
	// EventType is the event_type column written for the emitted row.
	EventType string
	// ProjectID scopes the emitted event row to a project. Required.
	ProjectID int64
	// EntityID resolves the FK the event row points at (plan id for
	// plan events, task id for task events, etc.) from the mutated
	// entity. Letting callers compute this from T post-mutate means
	// INSERT ... RETURNING id paths flow naturally without a separate
	// "id" field on every TxMutation construction.
	EntityID func(entity T) int64
	// Body returns the events.body column for EventScopeTask rows.
	// Ignored for EventScopeEntity. Optional — callers that emit empty
	// bodies can leave it nil and the helper substitutes "".
	Body func(entity T) string
	// Mutate runs the persistence write inside the helper's transaction.
	// The returned entity flows through Payload and EntityID/Body.
	// Mutate's error is bubbled up; the surrounding deferred Rollback
	// reverses every write before the helper returns.
	Mutate func(ctx context.Context, tx *sql.Tx) (T, error)
	// Payload builds the events.payload JSON string from the mutated
	// entity. Returning a non-nil error rolls the mutation back —
	// useful for json.Marshal failures the helper would otherwise mask.
	Payload func(entity T) (string, error)
	// ShouldLog gates the event insertion. When non-nil and returning
	// false, the helper skips the events-row INSERT and publishes a
	// synthetic Event so subscribers still observe the action — this
	// preserves the pre-helper semantics that several callsites
	// (CreateTask, MoveTask, SetTaskState, UpdateComment,
	// RebindOrphanedTasks) implement inline today. nil ShouldLog means
	// "always insert"; the entity-scope callsites that never gated take
	// this path.
	ShouldLog func() bool
}

// txMutateAndEmit owns the BeginTx → mutate → emit → Commit → publish
// lifecycle. Migrated callsites collapse from ~25 LOC of bookkeeping
// (transaction lifecycle, commit gating, post-commit publish ordering)
// down to a TxMutation literal + one call.
//
// Contract notes:
//   - Mutate runs inside the helper's transaction. A non-nil error
//     bubbles up after the deferred Rollback fires.
//   - Payload runs AFTER Mutate but BEFORE the event row insert. A
//     non-nil error rolls back the mutation — protects against JSON
//     marshal failures the inline callsites today couple with their
//     own error returns.
//   - publishEvent fires ONLY after tx.Commit succeeds. A commit
//     failure leaves the events table unchanged AND the bus silent —
//     subscribers can rely on "observed an event ⇒ row is on disk".
//   - ShouldLog=false produces a synthetic Event (no row), publishes
//     it post-commit, and the row count in the events table stays
//     untouched. Matches the inline gated callsites' existing shape.
func txMutateAndEmit[T any](ctx context.Context, s *Store, m TxMutation[T]) (T, error) {
	var zero T
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	entity, err := m.Mutate(ctx, tx)
	if err != nil {
		return zero, err
	}

	payload, err := m.Payload(entity)
	if err != nil {
		return zero, err
	}

	entityID := m.EntityID(entity)
	var body string
	if m.Body != nil {
		body = m.Body(entity)
	}

	logRow := true
	if m.ShouldLog != nil {
		logRow = m.ShouldLog()
	}

	var event domain.Event
	if logRow {
		switch m.Scope {
		case EventScopeEntity:
			event, err = insertEntityEvent(ctx, tx, m.EntityType, entityID, m.ProjectID, m.EventType, payload)
		case EventScopeTask:
			event, err = insertTaskEvent(ctx, tx, m.ProjectID, entityID, m.EventType, body, payload)
		}
		if err != nil {
			return zero, err
		}
	} else {
		// Gate closed: synthesise the broadcast envelope so subscribers
		// still observe the action. The events table stays untouched —
		// mirrors the inline gated callsites (CreateTask, MoveTask, …).
		entityType := m.EntityType
		if m.Scope == EventScopeTask {
			entityType = domain.EventEntityTask
		}
		event = domain.Event{
			EntityType: entityType,
			EntityID:   entityID,
			ProjectID:  m.ProjectID,
			EventType:  m.EventType,
			Body:       body,
			Payload:    payload,
		}
	}

	if err := tx.Commit(); err != nil {
		return zero, err
	}
	committed = true

	s.publishEvent(ctx, event)
	return entity, nil
}
