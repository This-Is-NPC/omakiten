package domain

import "time"

// EventRow is the read shape returned by the unified Logs inspector
// query (`internal/sqlite.Store.ListEvents`). It mirrors the Event
// struct's column projection but in a form callers can hold inside the
// app/TUI layer without dragging JSON tag baggage onto every consumer.
//
// Fields are populated on a best-effort basis — different event_types
// fill different subsets. Comments carry Body+AuthorType; tool calls
// carry Source+Status+DurationMs; system events carry Payload. Treat
// any zero-value field as absent; SummarizeEvent encodes the per-type
// rules for which fields it reads.
type EventRow struct {
	ID           int64
	EntityType   string
	EntityID     int64
	ProjectID    int64
	ProjectSlug  string
	EventType    string
	Body         string
	Payload      string
	AuthorType   string
	Source       string
	Status       string
	DurationMs   int
	ErrorMessage string
	CreatedAt    string
	FinishedAt   string
	AgentModel   string
}

// EventFilter scopes a ListEvents call. Zero values are intentional:
// they mean "no filter on this axis" so callers can compose subsets
// without juggling sentinel constants.
//
//   - Categories empty  → every category is included (no category filter).
//   - Since zero-value  → no time floor.
//   - Limit <= 0        → no row cap.
//   - Order ""          → adapter default (descending by created_at /
//     started_at, matching the existing activity-log path).
type EventFilter struct {
	// ProjectID scopes the query to one project. Zero means "no
	// project filter"; the caller is expected to already know whether
	// it wants a per-project or system-wide view.
	ProjectID int64
	// Categories restricts results to the given event categories.
	// Empty = no category filter. Unknown / duplicate entries are
	// adapter responsibilities — domain just carries the list.
	Categories []EventCategory
	// Since is the inclusive lower bound on the event timestamp.
	// Zero-value means "no time floor"; adapters must check
	// `Since.IsZero()` before emitting a SQL predicate.
	Since time.Time
	// Limit caps the number of rows returned. Values <= 0 mean
	// "no cap"; adapters may still impose a hard ceiling for safety.
	Limit int
	// Order is the sort direction key understood by the adapter.
	// Empty means adapter default. Conventional values: "desc"
	// (newest first) and "asc" (oldest first).
	Order string
}
