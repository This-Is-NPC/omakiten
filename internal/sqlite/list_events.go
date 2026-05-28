package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"omakiten/internal/domain"
)

// sqliteTimestampLayout is the TEXT shape SQLite's CURRENT_TIMESTAMP
// writes into events.created_at ("YYYY-MM-DD HH:MM:SS" UTC). Predicate
// values must be formatted the same way so string comparison gives the
// expected ordering — Go's default time.Time.String() includes a
// timezone suffix and would not compare correctly.
const sqliteTimestampLayout = "2006-01-02 15:04:05"

// MaxListEventsLimit is the server-side hard ceiling on ListEvents row
// counts. Callers can request fewer rows via EventFilter.Limit, but
// anything above this (or 0 / negative, meaning "no caller cap") gets
// clamped here so an MCP client requesting `limit=1_000_000` cannot
// exhaust memory by streaming the entire events table.
const MaxListEventsLimit = 10_000

// ListEvents is the generic Logs inspector read path. It returns rows
// from the unified `events` table filtered by domain.EventFilter axes
// and shaped as domain.EventRow values for consumption by TUI / CLI /
// MCP surfaces.
//
// Filter semantics — every axis degrades to "no filter" at its zero
// value (see EventFilter godoc):
//
//   - ProjectID = 0  → no project filter (system-wide view).
//   - Categories empty → no category filter; every event_type is included.
//   - Since zero-value → no time floor.
//   - Limit <= 0 or > MaxListEventsLimit (10000) → capped at MaxListEventsLimit.
//   - Order "" or "desc" → newest-first; "asc" → oldest-first. Within
//     equal timestamps, id is the tiebreaker so paging stays stable.
//
// Category expansion: each requested EventCategory is expanded via
// domain.EventTypesForCategory — the inversion is computed in the
// domain layer from KnownEventTypes + EventCategoryOf, so adding a new
// event_type with a category arm propagates here automatically. The
// expanded set becomes a `event_type IN (?, ?, ...)`
// predicate so the planner reuses `idx_events_type_started`
// (event_type, created_at) from migration 009 — the same index
// activity_logs.go relies on. No new index is added by this read path.
//
// EXPLAIN QUERY PLAN for the canonical category-filtered query reports:
//
//	SEARCH events USING INDEX idx_events_type_started (event_type=?)
//
// confirming the planner uses the existing index and avoids a full
// table scan. When Categories is empty the planner falls back to a
// covering scan over `events` — acceptable because the caller is
// asking for everything; the `Since` predicate still narrows the row
// set when present.
//
// When every supplied category is unknown the helper would emit an
// empty IN list (`IN ()`), which SQLite rejects as a syntax error. We
// short-circuit and return an empty slice instead so callers receive a
// predictable "nothing matches" result.
func (s *Store) ListEvents(ctx context.Context, filter domain.EventFilter) ([]domain.EventRow, error) {
	conds := []string{}
	args := []any{}

	if filter.ProjectID > 0 {
		conds = append(conds, "project_id = ?")
		args = append(args, filter.ProjectID)
	}

	if len(filter.Categories) > 0 {
		// Expand each requested category into its concrete event_type
		// set via the domain helper. Dedup across categories with a set
		// — the partition guarantees no overlap today, but a defensive
		// dedup means a future overlapping category can't produce a
		// duplicate-laden IN list. Final order is sorted so the SQL
		// shape stays stable for EXPLAIN diff review.
		seen := make(map[string]struct{})
		for _, cat := range filter.Categories {
			for _, et := range domain.EventTypesForCategory(cat) {
				seen[et] = struct{}{}
			}
		}
		if len(seen) == 0 {
			// Every supplied category was unknown — return an empty
			// slice rather than build an invalid `IN ()` clause.
			return nil, nil
		}
		eventTypes := make([]string, 0, len(seen))
		for et := range seen {
			eventTypes = append(eventTypes, et)
		}
		sort.Strings(eventTypes)
		ph := make([]string, len(eventTypes))
		for i, et := range eventTypes {
			ph[i] = "?"
			args = append(args, et)
		}
		conds = append(conds, "event_type IN ("+strings.Join(ph, ",")+")")
	}

	if !filter.Since.IsZero() {
		conds = append(conds, "created_at >= ?")
		args = append(args, filter.Since.UTC().Format(sqliteTimestampLayout))
	}

	query := "SELECT id, entity_type, COALESCE(entity_id, 0), COALESCE(project_id, 0), COALESCE(project_slug, ''), event_type, COALESCE(body, ''), COALESCE(payload, ''), COALESCE(author_type, ''), COALESCE(source, ''), COALESCE(status, ''), COALESCE(duration_ms, 0), COALESCE(error_message, ''), created_at, COALESCE(finished_at, ''), COALESCE(agent_model, '') FROM events"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}

	direction := "DESC"
	if strings.EqualFold(filter.Order, "asc") {
		direction = "ASC"
	}
	query += " ORDER BY created_at " + direction + ", id " + direction

	effective := filter.Limit
	if effective <= 0 || effective > MaxListEventsLimit {
		effective = MaxListEventsLimit
	}
	query += " LIMIT ?"
	args = append(args, effective)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []domain.EventRow
	for rows.Next() {
		var row domain.EventRow
		var durationMs sql.NullInt64
		if err := rows.Scan(
			&row.ID,
			&row.EntityType,
			&row.EntityID,
			&row.ProjectID,
			&row.ProjectSlug,
			&row.EventType,
			&row.Body,
			&row.Payload,
			&row.AuthorType,
			&row.Source,
			&row.Status,
			&durationMs,
			&row.ErrorMessage,
			&row.CreatedAt,
			&row.FinishedAt,
			&row.AgentModel,
		); err != nil {
			return nil, err
		}
		row.DurationMs = int(durationMs.Int64)
		out = append(out, row)
	}
	return out, rows.Err()
}

// EventCategoryCounts returns a count of events per known category over
// the requested time window. Every category in domain.KnownEventCategories
// is present in the result map — categories with no matching rows
// return 0 so renderers can build a stable table without filling in
// defaults. EventCategoryUnknown is omitted; rows whose event_type is
// outside KnownEventTypes (legacy / forward-compat) do not contribute
// to any bucket.
//
// projectID = 0 disables the project filter (system-wide). since
// zero-value disables the time floor. The implementation runs a single
// `GROUP BY event_type` aggregate and folds rows into category buckets
// in Go — keeps the SQL trivial and avoids threading the category
// switch into a CASE expression that would have to be kept in sync
// with domain/event_category.go by hand.
func (s *Store) EventCategoryCounts(ctx context.Context, projectID int64, since time.Time) (map[domain.EventCategory]int, error) {
	conds := []string{}
	args := []any{}
	if projectID > 0 {
		conds = append(conds, "project_id = ?")
		args = append(args, projectID)
	}
	if !since.IsZero() {
		conds = append(conds, "created_at >= ?")
		args = append(args, since.UTC().Format(sqliteTimestampLayout))
	}

	query := "SELECT event_type, COUNT(*) FROM events"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " GROUP BY event_type"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[domain.EventCategory]int, len(domain.KnownEventCategories))
	for _, c := range domain.KnownEventCategories {
		counts[c] = 0
	}
	for rows.Next() {
		var eventType string
		var n int
		if err := rows.Scan(&eventType, &n); err != nil {
			return nil, err
		}
		cat := domain.EventCategoryOf(eventType)
		if cat == domain.EventCategoryUnknown {
			continue
		}
		counts[cat] += n
	}
	return counts, rows.Err()
}

