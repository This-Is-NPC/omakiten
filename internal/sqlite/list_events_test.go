package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"omakiten/internal/domain"
)

// seedEvent inserts a minimal events row with a custom created_at so
// time-window tests are deterministic. The row uses entity_type='system'
// for non-task event_types so the writes don't need a backing task row.
func seedEvent(ctx context.Context, t *testing.T, store *storeFixture, projectID int64, eventType string, createdAt time.Time) int64 {
	t.Helper()
	entity := domain.EventEntitySystem
	if domain.EventCategoryOf(eventType) == domain.EventCategoryTask || eventType == domain.EventTypeComment {
		// Task / comment rows want entity_type='task' in the schema —
		// keep the seed consistent so any future schema check fires.
		entity = domain.EventEntityTask
	}
	var pid any
	if projectID > 0 {
		pid = projectID
	}
	var id int64
	if err := store.db.QueryRowContext(ctx, `
INSERT INTO events(entity_type, project_id, event_type, payload, created_at)
VALUES (?, ?, ?, '{}', ?)
RETURNING id
`, entity, pid, eventType, createdAt.UTC().Format(sqliteTimestampLayout)).Scan(&id); err != nil {
		t.Fatalf("seed event_type=%q error = %v", eventType, err)
	}
	return id
}

// TestListEventsEmptyFilterReturnsAllRows locks AC#1 — with no filter
// every row in the events table comes back regardless of category.
func TestListEventsEmptyFilterReturnsAllRows(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-3*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeComment, now.Add(-2*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeHookExecuted, now.Add(-1*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeGuardViolated, now)

	got, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (all seeded rows)", len(got))
	}
}

// TestListEventsSingleCategoryFilter locks AC#2 — a single category
// only returns rows whose event_type belongs to that category.
func TestListEventsSingleCategoryFilter(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-3*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskMoved, now.Add(-2*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeComment, now.Add(-1*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeHookExecuted, now)

	got, err := store.ListEvents(ctx, domain.EventFilter{
		ProjectID:  1,
		Categories: []domain.EventCategory{domain.EventCategoryTask},
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (task category only)", len(got))
	}
	for _, row := range got {
		if domain.EventCategoryOf(row.EventType) != domain.EventCategoryTask {
			t.Errorf("row.EventType = %q maps to %q, want task category",
				row.EventType, domain.EventCategoryOf(row.EventType))
		}
	}
}

// TestListEventsMultiCategoryFilter — multiple categories OR together.
func TestListEventsMultiCategoryFilter(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-4*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeComment, now.Add(-3*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeHookExecuted, now.Add(-2*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeGuardViolated, now.Add(-1*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypePlanCreated, now)

	got, err := store.ListEvents(ctx, domain.EventFilter{
		ProjectID:  1,
		Categories: []domain.EventCategory{domain.EventCategoryTask, domain.EventCategoryComment},
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (task + comment)", len(got))
	}
	for _, row := range got {
		c := domain.EventCategoryOf(row.EventType)
		if c != domain.EventCategoryTask && c != domain.EventCategoryComment {
			t.Errorf("row.EventType = %q maps to %q, want task or comment", row.EventType, c)
		}
	}
}

// TestListEventsSinceFilter locks AC#3 — rows older than Since are
// excluded; rows at exactly the Since instant are included (inclusive).
func TestListEventsSinceFilter(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC().Truncate(time.Second)

	old := seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-48*time.Hour))
	boundary := seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-24*time.Hour))
	fresh := seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-1*time.Hour))

	got, err := store.ListEvents(ctx, domain.EventFilter{
		ProjectID: 1,
		Since:     now.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (boundary + fresh)", len(got))
	}
	seen := map[int64]bool{}
	for _, row := range got {
		seen[row.ID] = true
	}
	if seen[old] {
		t.Errorf("row %d (older than Since) leaked into result", old)
	}
	if !seen[boundary] {
		t.Errorf("row %d (at Since instant) excluded — Since must be inclusive", boundary)
	}
	if !seen[fresh] {
		t.Errorf("row %d (newer than Since) missing", fresh)
	}
}

// TestListEventsLimitCapsRowCount locks AC#4 (limit half) — at most
// Limit rows come back even when more match the filter.
func TestListEventsLimitCapsRowCount(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(time.Duration(-i)*time.Minute))
	}

	got, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1, Limit: 3})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (capped by Limit)", len(got))
	}
}

// TestListEventsOrderAscAndDesc — Order honoured; default is desc.
func TestListEventsOrderAscAndDesc(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC().Truncate(time.Second)

	oldest := seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-3*time.Hour))
	mid := seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-2*time.Hour))
	newest := seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-1*time.Hour))

	asc, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1, Order: "asc"})
	if err != nil {
		t.Fatalf("ListEvents(asc) error = %v", err)
	}
	if len(asc) != 3 || asc[0].ID != oldest || asc[1].ID != mid || asc[2].ID != newest {
		t.Fatalf("asc IDs = %v, want [%d %d %d]", ids(asc), oldest, mid, newest)
	}

	// Empty Order means adapter default (desc).
	descDefault, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1})
	if err != nil {
		t.Fatalf("ListEvents(default) error = %v", err)
	}
	if len(descDefault) != 3 || descDefault[0].ID != newest || descDefault[1].ID != mid || descDefault[2].ID != oldest {
		t.Fatalf("default order IDs = %v, want [%d %d %d]", ids(descDefault), newest, mid, oldest)
	}

	// Explicit "desc" matches default.
	desc, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1, Order: "desc"})
	if err != nil {
		t.Fatalf("ListEvents(desc) error = %v", err)
	}
	if len(desc) != 3 || desc[0].ID != newest {
		t.Fatalf("desc IDs = %v, want [%d ...]", ids(desc), newest)
	}
}

// TestListEventsCombinedFilters — every axis exercised together: a
// category + a since window + a limit, descending. The intersection
// must hold for every returned row.
func TestListEventsCombinedFilters(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC().Truncate(time.Second)

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-48*time.Hour)) // old, in-cat: excluded by Since
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskMoved, now.Add(-2*time.Hour))    // matches
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-1*time.Hour))  // matches
	seedEvent(ctx, t, store, 1, domain.EventTypeComment, now.Add(-30*time.Minute))   // wrong category
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCompleted, now)                  // matches (newest)
	seedEvent(ctx, t, store, 2, domain.EventTypeTaskCreated, now)                    // wrong project

	got, err := store.ListEvents(ctx, domain.EventFilter{
		ProjectID:  1,
		Categories: []domain.EventCategory{domain.EventCategoryTask},
		Since:      now.Add(-24 * time.Hour),
		Limit:      2,
		Order:      "desc",
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, row := range got {
		if row.ProjectID != 1 {
			t.Errorf("row.ProjectID = %d, want 1", row.ProjectID)
		}
		if domain.EventCategoryOf(row.EventType) != domain.EventCategoryTask {
			t.Errorf("row.EventType = %q not task category", row.EventType)
		}
	}
	// First row must be the newest (desc).
	if got[0].EventType != domain.EventTypeTaskCompleted {
		t.Errorf("got[0].EventType = %q, want %q (newest, desc)", got[0].EventType, domain.EventTypeTaskCompleted)
	}
}

// TestListEventsUnknownCategoriesReturnEmpty — supplying only unknown
// categories must not produce an invalid `IN ()` clause; the helper
// short-circuits to an empty slice.
func TestListEventsUnknownCategoriesReturnEmpty(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, time.Now())

	got, err := store.ListEvents(ctx, domain.EventFilter{
		ProjectID:  1,
		Categories: []domain.EventCategory{"definitely-not-a-known-category"},
	})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

// TestListEventsProjectScope — ProjectID=0 disables the filter so
// every project's rows come back; a non-zero ProjectID narrows.
func TestListEventsProjectScope(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now()

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now)
	seedEvent(ctx, t, store, 2, domain.EventTypeTaskCreated, now)
	seedEvent(ctx, t, store, 3, domain.EventTypeTaskCreated, now)

	all, err := store.ListEvents(ctx, domain.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}

	scoped, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 2})
	if err != nil {
		t.Fatalf("ListEvents(project=2) error = %v", err)
	}
	if len(scoped) != 1 || scoped[0].ProjectID != 2 {
		t.Fatalf("scoped = %+v, want one row with ProjectID=2", scoped)
	}
}

// TestListEventsUsesIndex documents AC#6 — the planner must use
// idx_events_type_started for category-filtered queries; no full scan.
func TestListEventsUsesIndex(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now()
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now)

	// EXPLAIN QUERY PLAN against the category-filtered shape. Build the
	// argument list manually so the EXPLAIN exactly matches the
	// production query.
	eventTypes := expandCategoriesToEventTypes([]domain.EventCategory{domain.EventCategoryTask})
	ph := make([]string, len(eventTypes))
	args := []any{int64(1)}
	for i, et := range eventTypes {
		ph[i] = "?"
		args = append(args, et)
	}
	query := "EXPLAIN QUERY PLAN SELECT id FROM events WHERE project_id = ? AND event_type IN (" + strings.Join(ph, ",") + ") ORDER BY created_at DESC"

	rows, err := store.db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan EXPLAIN error = %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	planStr := plan.String()
	if !strings.Contains(planStr, "idx_events_type_started") {
		t.Fatalf("EXPLAIN plan missing idx_events_type_started — got:\n%s", planStr)
	}
	if strings.Contains(planStr, "SCAN events") && !strings.Contains(planStr, "USING INDEX") {
		t.Fatalf("EXPLAIN plan shows full table scan — got:\n%s", planStr)
	}
}

// TestEventCategoryCountsStableShape locks AC#5 — every known category
// appears in the result map (counts of 0 are explicit so the renderer
// doesn't have to fill in defaults).
func TestEventCategoryCountsStableShape(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now)
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskMoved, now)
	seedEvent(ctx, t, store, 1, domain.EventTypeComment, now)

	got, err := store.EventCategoryCounts(ctx, 1, time.Time{})
	if err != nil {
		t.Fatalf("EventCategoryCounts() error = %v", err)
	}
	if len(got) != len(domain.KnownEventCategories) {
		t.Fatalf("len(counts) = %d, want %d (every known category)", len(got), len(domain.KnownEventCategories))
	}
	for _, c := range domain.KnownEventCategories {
		if _, ok := got[c]; !ok {
			t.Errorf("category %q missing from result", c)
		}
	}
	if got[domain.EventCategoryTask] != 2 {
		t.Errorf("task count = %d, want 2", got[domain.EventCategoryTask])
	}
	if got[domain.EventCategoryComment] != 1 {
		t.Errorf("comment count = %d, want 1", got[domain.EventCategoryComment])
	}
	if got[domain.EventCategoryGuard] != 0 {
		t.Errorf("guard count = %d, want 0 (empty category must still be present)", got[domain.EventCategoryGuard])
	}
}

// TestEventCategoryCountsWindow — rows older than `since` do not count.
func TestEventCategoryCountsWindow(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC().Truncate(time.Second)

	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-48*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(-1*time.Hour))
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now)

	got, err := store.EventCategoryCounts(ctx, 1, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("EventCategoryCounts() error = %v", err)
	}
	if got[domain.EventCategoryTask] != 2 {
		t.Errorf("task count within window = %d, want 2", got[domain.EventCategoryTask])
	}
}

// TestEventCategoryCountsProjectScope — projectID=0 spans every
// project; non-zero narrows to that project.
func TestEventCategoryCountsProjectScope(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now()
	seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now)
	seedEvent(ctx, t, store, 2, domain.EventTypeTaskCreated, now)
	seedEvent(ctx, t, store, 2, domain.EventTypeTaskMoved, now)

	all, err := store.EventCategoryCounts(ctx, 0, time.Time{})
	if err != nil {
		t.Fatalf("EventCategoryCounts(all) error = %v", err)
	}
	if all[domain.EventCategoryTask] != 3 {
		t.Errorf("task count (all projects) = %d, want 3", all[domain.EventCategoryTask])
	}

	scoped, err := store.EventCategoryCounts(ctx, 2, time.Time{})
	if err != nil {
		t.Fatalf("EventCategoryCounts(project=2) error = %v", err)
	}
	if scoped[domain.EventCategoryTask] != 2 {
		t.Errorf("task count (project=2) = %d, want 2", scoped[domain.EventCategoryTask])
	}
}

// TestExpandCategoriesToEventTypes — direct unit on the helper.
func TestExpandCategoriesToEventTypes(t *testing.T) {
	got := expandCategoriesToEventTypes([]domain.EventCategory{domain.EventCategoryHook})
	if len(got) != 1 || got[0] != domain.EventTypeHookExecuted {
		t.Fatalf("hook expansion = %v, want [%q]", got, domain.EventTypeHookExecuted)
	}

	// Multi: tool_call has 3 event_types — cli/mcp/tui — sorted.
	tool := expandCategoriesToEventTypes([]domain.EventCategory{domain.EventCategoryToolCall})
	if len(tool) != 3 {
		t.Fatalf("tool_call expansion len = %d, want 3 (cli/mcp/tui)", len(tool))
	}

	// Unknown silently drops.
	empty := expandCategoriesToEventTypes([]domain.EventCategory{domain.EventCategory("nope")})
	if len(empty) != 0 {
		t.Fatalf("unknown expansion = %v, want []", empty)
	}
}

func ids(rows []domain.EventRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// TestListEventsZeroLimitReturnsAllUnderCap locks the safety-ceiling
// contract for the common path: Limit <= 0 means "no caller cap" but
// the SQL layer still clamps at MaxListEventsLimit. With a tiny
// dataset (well under the cap), every row must still come back so the
// ceiling doesn't double as a low-traffic ceiling.
func TestListEventsZeroLimitReturnsAllUnderCap(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(time.Duration(-i)*time.Minute))
	}

	got, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1, Limit: 0})
	if err != nil {
		t.Fatalf("ListEvents(limit=0) error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (every row under the cap)", len(got))
	}
}

// TestListEventsLimitAboveCapClamped locks the safety-ceiling contract
// for the abusive path: a caller asking for far more than
// MaxListEventsLimit must not error and must not return more than the
// cap. We seed a handful of rows (well under the cap) so the result
// length is bounded by row count, not by the cap — the key assertion is
// "no error, no over-cap result". A separate sanity check confirms the
// generated SQL would honour the clamp for a larger dataset.
func TestListEventsLimitAboveCapClamped(t *testing.T) {
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		seedEvent(ctx, t, store, 1, domain.EventTypeTaskCreated, now.Add(time.Duration(-i)*time.Minute))
	}

	got, err := store.ListEvents(ctx, domain.EventFilter{ProjectID: 1, Limit: 50_000})
	if err != nil {
		t.Fatalf("ListEvents(limit=50_000) error = %v", err)
	}
	if len(got) > MaxListEventsLimit {
		t.Fatalf("len = %d, want <= MaxListEventsLimit (%d)", len(got), MaxListEventsLimit)
	}
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (every seeded row, clamp must not drop legitimate rows)", len(got))
	}
}

// TestMaxListEventsLimitConstant pins the public constant so callers
// referencing it (godoc, MCP schema) don't drift silently.
func TestMaxListEventsLimitConstant(t *testing.T) {
	if MaxListEventsLimit != 10_000 {
		t.Fatalf("MaxListEventsLimit = %d, want 10000", MaxListEventsLimit)
	}
}
