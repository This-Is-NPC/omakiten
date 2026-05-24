package sqlite

import (
	"context"
	"database/sql"

	"omakiten/internal/domain"
)

// EventScope selects whether the lifecycle helper persists the emitted
// row through insertEntityEvent (plan / non-task entities) or
// insertTaskEvent (task feed). Filled in by the upcoming
// implementation commit.
type EventScope int

const (
	// EventScopeEntity persists the event via insertEntityEvent. Stub
	// today; the implementation commit wires this branch.
	EventScopeEntity EventScope = iota
	// EventScopeTask persists the event via insertTaskEvent. Stub today.
	EventScopeTask
)

// TxMutation is the shape the migrated callsites will consume. Stub
// today — every field is declared so the unit tests in
// txevent_test.go compile against the final API while txMutateAndEmit
// returns zero values; tests fail until the implementation commit
// fills the body.
type TxMutation[T any] struct {
	Scope      EventScope
	EntityType string
	EventType  string
	ProjectID  int64
	EntityID   func(entity T) int64
	Body       func(entity T) string
	Mutate     func(ctx context.Context, tx *sql.Tx) (T, error)
	Payload    func(entity T) (string, error)
	ShouldLog  func() bool
}

// txMutateAndEmit is the lifecycle helper — STUB. The next commit
// fills BeginTx → Mutate → Payload → emit → Commit → publish; the
// stub returns zero so the test file compiles.
func txMutateAndEmit[T any](_ context.Context, _ *Store, _ TxMutation[T]) (T, error) {
	var zero T
	return zero, nil
}

// keep imports live for the stub so gofmt + go vet stay happy until
// the implementation commit references them in the helper body.
var _ = domain.EventEntityTask
