package sqlite

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
)

// fakeSweepClock is a deterministic time source for the sweep. `step`
// advances the clock on every read, which is how the wall-clock cap is
// exercised without sleeping; cadence tests use step=0 and drive time
// explicitly with Advance.
type fakeSweepClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func newFakeSweepClock(step time.Duration) *fakeSweepClock {
	return &fakeSweepClock{now: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC), step: step}
}

func (c *fakeSweepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.now
	c.now = c.now.Add(c.step)
	return current
}

func (c *fakeSweepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// sweepPolicy builds a resolved policy with generous caps; individual
// tests narrow the knob they are asserting on.
func sweepPolicy() config.ResolvedOrphanSweep {
	return config.ResolvedOrphanSweep{
		Enabled:        true,
		Interval:       24 * time.Hour,
		RetryInterval:  time.Hour,
		BatchRows:      100,
		MaxRowsPerPass: 500,
		MaxDuration:    time.Hour,
	}
}

func newSweepStore(t *testing.T, clock *fakeSweepClock, policy config.ResolvedOrphanSweep) *Store {
	t.Helper()
	store, err := Open(context.Background(), t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if clock != nil {
		store.orphanSweepNow = clock.Now
	}
	store.SetOrphanSweepPolicy(policy)
	return store
}

// insertEvent writes one row straight into `events`, bypassing every
// emission path so the fixture can express project_id shapes (NULL, 0,
// a dangling id) the public API refuses to produce.
func insertEvent(t *testing.T, store *Store, projectID any, eventType string) int64 {
	t.Helper()
	row := store.db.QueryRowContext(context.Background(), `
INSERT INTO events(entity_type, entity_id, project_id, event_type, body)
VALUES ('system', 0, ?, ?, 'body')
RETURNING id
`, projectID, eventType)
	var id int64
	if err := row.Scan(&id); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	return id
}

func insertEventWithID(t *testing.T, store *Store, id int64, projectID int64, eventType string) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), `
INSERT INTO events(id, entity_type, entity_id, project_id, event_type, body)
VALUES (?, 'system', 0, ?, ?, 'body')
`, id, projectID, eventType); err != nil {
		t.Fatalf("insert event id=%d: %v", id, err)
	}
}

func eventExists(t *testing.T, store *Store, id int64) bool {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("count event %d: %v", id, err)
	}
	return count == 1
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

func archiveProject(t *testing.T, store *Store, id int64) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), `UPDATE projects SET archived_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		t.Fatalf("archive project %d: %v", id, err)
	}
}

// TestSweepOrphanEventsScope is the AC 5 anti-join contract: only rows
// with a positive project_id that resolves to nothing are in scope.
func TestSweepOrphanEventsScope(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	live, err := store.UpsertProject(ctx, "live", "live", "/live")
	if err != nil {
		t.Fatalf("UpsertProject live: %v", err)
	}
	archived, err := store.UpsertProject(ctx, "archived", "archived", "/archived")
	if err != nil {
		t.Fatalf("UpsertProject archived: %v", err)
	}
	archiveProject(t, store, archived.ID)

	nullEvent := insertEvent(t, store, nil, "system.note")
	zeroEvent := insertEvent(t, store, 0, "system.note")
	liveEvent := insertEvent(t, store, live.ID, "task.created")
	archivedEvent := insertEvent(t, store, archived.ID, "task.created")
	orphanEvent := insertEvent(t, store, int64(9999), "task.created")

	report, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}
	if report.DeletedRows != 1 {
		t.Fatalf("DeletedRows = %d, want 1", report.DeletedRows)
	}
	if report.LastID != orphanEvent {
		t.Fatalf("LastID = %d, want %d", report.LastID, orphanEvent)
	}
	if report.Limited {
		t.Fatalf("Limited = true, want false for a drained pass")
	}
	if report.Batches != 1 {
		t.Fatalf("Batches = %d, want 1", report.Batches)
	}

	for name, id := range map[string]int64{
		"null project_id":  nullEvent,
		"zero project_id":  zeroEvent,
		"live project":     liveEvent,
		"archived project": archivedEvent,
	} {
		if !eventExists(t, store, id) {
			t.Fatalf("%s event %d was deleted; the sweep must preserve it", name, id)
		}
	}
	if eventExists(t, store, orphanEvent) {
		t.Fatalf("orphan event %d survived the sweep", orphanEvent)
	}
}

// TestSweepOrphanEventsNoAgeGrace pins AC 5's "no age grace": a freshly
// created orphan is swept on the very next pass.
func TestSweepOrphanEventsNoAgeGrace(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	fresh := insertEvent(t, store, int64(4242), "task.created")
	if _, err := store.SweepOrphanEvents(ctx); err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}
	if eventExists(t, store, fresh) {
		t.Fatalf("orphan created moments ago survived; the sweep must apply no age grace")
	}
}

// TestSweepOrphanEventsCascadesDependents covers AC 5's dependent
// cleanup: event tags go through the FK cascade and the comment FTS row
// through the AFTER DELETE trigger, with no extra SQL in the sweep.
func TestSweepOrphanEventsCascadesDependents(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	orphanComment := insertEvent(t, store, int64(9999), "comment")
	tag, err := store.FindOrCreateTag(ctx, "sweepable", "Sweepable")
	if err != nil {
		t.Fatalf("FindOrCreateTag: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO event_tags(event_id, tag_id) VALUES (?, ?)`, orphanComment, tag.ID); err != nil {
		t.Fatalf("insert event_tags: %v", err)
	}

	if got := countRows(t, store, `SELECT COUNT(*) FROM event_tags WHERE event_id = ?`, orphanComment); got != 1 {
		t.Fatalf("event_tags precondition = %d, want 1", got)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM search_index WHERE entity_type = 'comment' AND entity_id = ?`, orphanComment); got != 1 {
		t.Fatalf("search_index precondition = %d, want 1", got)
	}

	if _, err := store.SweepOrphanEvents(ctx); err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}

	if got := countRows(t, store, `SELECT COUNT(*) FROM event_tags WHERE event_id = ?`, orphanComment); got != 0 {
		t.Fatalf("event_tags after sweep = %d, want 0 (FK cascade)", got)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM search_index WHERE entity_type = 'comment' AND entity_id = ?`, orphanComment); got != 0 {
		t.Fatalf("search_index after sweep = %d, want 0 (AFTER DELETE trigger)", got)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM tags WHERE id = ?`, tag.ID); got != 1 {
		t.Fatalf("tags row = %d, want 1; the cascade must stop at the bridge table", got)
	}
}

// TestSweepOrphanEventsRowCapPerPass is the SMART success criterion: a
// fixture larger than the cap removes exactly the cap and reports a
// limited pass; the leftovers drain on the following pass.
func TestSweepOrphanEventsRowCapPerPass(t *testing.T) {
	ctx := context.Background()
	policy := sweepPolicy()
	policy.BatchRows = 100
	policy.MaxRowsPerPass = 500
	store := newSweepStore(t, newFakeSweepClock(0), policy)

	const total = 620
	for i := 0; i < total; i++ {
		insertEvent(t, store, int64(9999), "task.created")
	}

	first, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("first SweepOrphanEvents: %v", err)
	}
	if first.DeletedRows != 500 {
		t.Fatalf("first pass DeletedRows = %d, want 500 (per-pass cap)", first.DeletedRows)
	}
	if first.Batches != 5 {
		t.Fatalf("first pass Batches = %d, want 5 (500 rows / 100-row batches)", first.Batches)
	}
	if !first.Limited {
		t.Fatalf("first pass Limited = false, want true")
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM events WHERE project_id = 9999`); got != total-500 {
		t.Fatalf("remaining orphans = %d, want %d", got, total-500)
	}

	second, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("second SweepOrphanEvents: %v", err)
	}
	if second.DeletedRows != total-500 {
		t.Fatalf("second pass DeletedRows = %d, want %d", second.DeletedRows, total-500)
	}
	if second.Limited {
		t.Fatalf("second pass Limited = true, want false (backlog drained)")
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM events WHERE project_id = 9999`); got != 0 {
		t.Fatalf("orphans left after draining = %d, want 0", got)
	}
}

// TestSweepOrphanEventsWallClockCap proves the 50ms budget stops a pass
// even when the row cap is nowhere near reached.
func TestSweepOrphanEventsWallClockCap(t *testing.T) {
	ctx := context.Background()
	clock := newFakeSweepClock(20 * time.Millisecond)
	policy := sweepPolicy()
	policy.BatchRows = 1
	policy.MaxRowsPerPass = 500
	policy.MaxDuration = 50 * time.Millisecond
	store := newSweepStore(t, clock, policy)

	for i := 0; i < 20; i++ {
		insertEvent(t, store, int64(9999), "task.created")
	}

	report, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}
	if !report.Limited {
		t.Fatalf("Limited = false, want true (wall-clock cap)")
	}
	if report.DeletedRows >= 20 {
		t.Fatalf("DeletedRows = %d; the 50ms budget must stop the pass well before the fixture drains", report.DeletedRows)
	}
	if report.Elapsed < policy.MaxDuration {
		t.Fatalf("Elapsed = %v, want >= %v", report.Elapsed, policy.MaxDuration)
	}
	if report.DeletedRows >= policy.MaxRowsPerPass {
		t.Fatalf("DeletedRows = %d hit the row cap; this test must exercise the time cap", report.DeletedRows)
	}
}

// TestSweepOrphanEventsCursorResetsEachPass proves the cursor is
// pass-local. A row landing BELOW the previous pass's high-water mark —
// what a restored backup or an id-replaying writer produces — must still
// be reachable, which is only true if the next pass starts from zero.
func TestSweepOrphanEventsCursorResetsEachPass(t *testing.T) {
	ctx := context.Background()
	policy := sweepPolicy()
	policy.BatchRows = 1
	policy.MaxRowsPerPass = 1
	store := newSweepStore(t, newFakeSweepClock(0), policy)

	insertEventWithID(t, store, 10, 9999, "task.created")
	insertEventWithID(t, store, 20, 9999, "task.created")

	first, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("first SweepOrphanEvents: %v", err)
	}
	if first.LastID != 10 {
		t.Fatalf("first pass LastID = %d, want 10 (ascending id order)", first.LastID)
	}

	insertEventWithID(t, store, 5, 9999, "task.created")

	second, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("second SweepOrphanEvents: %v", err)
	}
	if second.LastID != 5 {
		t.Fatalf("second pass LastID = %d, want 5; a cursor carried across passes would skip it forever", second.LastID)
	}
	if eventExists(t, store, 5) {
		t.Fatalf("below-cursor orphan 5 survived; the cursor must reset each pass")
	}
}

// TestSweepOrphanEventsLateInsertFromStaleProcess models the finding the
// sweep exists for: a long-lived process writes an event for a project
// that was already deleted. The next pass reclaims it.
func TestSweepOrphanEventsLateInsertFromStaleProcess(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	project, err := store.UpsertProject(ctx, "doomed", "doomed", "/doomed")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	if err := store.DeleteProject(ctx, project.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	drained, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents after canonical delete: %v", err)
	}
	if drained.DeletedRows != 0 {
		t.Fatalf("DeletedRows = %d after canonical deletion, want 0; the sweep must find nothing to reconcile", drained.DeletedRows)
	}

	// The stale process still holds the old project id and writes on.
	late := insertEvent(t, store, project.ID, "task.created")

	report, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents after late insert: %v", err)
	}
	if report.DeletedRows != 1 || report.LastID != late {
		t.Fatalf("report = %+v, want 1 row with LastID %d", report, late)
	}
}

// TestSweepOrphanEventsIsIdempotent covers AC 7: repeated passes over a
// clean database are no-ops.
func TestSweepOrphanEventsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	insertEvent(t, store, int64(9999), "task.created")
	if _, err := store.SweepOrphanEvents(ctx); err != nil {
		t.Fatalf("first SweepOrphanEvents: %v", err)
	}
	for i := 0; i < 3; i++ {
		report, err := store.SweepOrphanEvents(ctx)
		if err != nil {
			t.Fatalf("repeat SweepOrphanEvents: %v", err)
		}
		if report.DeletedRows != 0 || report.LastID != 0 {
			t.Fatalf("repeat pass %d report = %+v, want an empty report", i, report)
		}
	}
}

// TestSweepOrphanEventsDisabledPolicy proves the kill switch.
func TestSweepOrphanEventsDisabledPolicy(t *testing.T) {
	ctx := context.Background()
	policy := sweepPolicy()
	policy.Enabled = false
	store := newSweepStore(t, newFakeSweepClock(0), policy)

	orphan := insertEvent(t, store, int64(9999), "task.created")
	report, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}
	if report != (OrphanSweepReport{}) {
		t.Fatalf("report = %+v, want zero value when disabled", report)
	}
	if !eventExists(t, store, orphan) {
		t.Fatalf("orphan deleted while the sweep is disabled")
	}
}

// TestSweepOrphanEventsRejectsUnboundedPolicy guards the terminating
// contract: a policy with a non-positive bound must do nothing rather
// than spin. The pass runs on the caller's goroutine, so a livelock here
// would wedge whichever surface triggered it.
func TestSweepOrphanEventsRejectsUnboundedPolicy(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(*config.ResolvedOrphanSweep){
		"zero batch":        func(p *config.ResolvedOrphanSweep) { p.BatchRows = 0 },
		"zero pass cap":     func(p *config.ResolvedOrphanSweep) { p.MaxRowsPerPass = 0 },
		"zero duration":     func(p *config.ResolvedOrphanSweep) { p.MaxDuration = 0 },
		"zero interval":     func(p *config.ResolvedOrphanSweep) { p.Interval = 0 },
		"zero retry":        func(p *config.ResolvedOrphanSweep) { p.RetryInterval = 0 },
		"negative pass cap": func(p *config.ResolvedOrphanSweep) { p.MaxRowsPerPass = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := sweepPolicy()
			mutate(&policy)
			store := newSweepStore(t, newFakeSweepClock(0), policy)
			orphan := insertEvent(t, store, int64(9999), "task.created")

			done := make(chan struct{})
			go func() {
				defer close(done)
				if _, err := store.SweepOrphanEvents(ctx); err != nil {
					t.Errorf("SweepOrphanEvents: %v", err)
				}
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("sweep did not terminate under an unbounded policy")
			}
			if !eventExists(t, store, orphan) {
				t.Fatalf("orphan deleted under an unbounded policy")
			}
		})
	}
}

// TestSweepOrphanEventsForcedAfterApplyConfig covers the AC 3 forced
// pass and the schedule it arms.
func TestSweepOrphanEventsForcedAfterApplyConfig(t *testing.T) {
	ctx := context.Background()
	clock := newFakeSweepClock(0)
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	store.orphanSweepNow = clock.Now

	orphan := insertEvent(t, store, int64(9999), "task.created")

	if err := store.ApplyConfig(ctx, ConfigKnobs{
		BusyTimeoutMs:            5000,
		EventsDefaultRecentLimit: 50,
		EventsPolicy:             config.EventsSettings{},
	}); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if eventExists(t, store, orphan) {
		t.Fatalf("orphan survived ApplyConfig; the forced pass must run")
	}

	// The forced pass arms the steady cadence, so the very next
	// activity-log write must not sweep again.
	late := insertEvent(t, store, int64(9999), "task.created")
	if _, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:     domain.ActivitySourceCLI,
		Entrypoint: "test",
		Operation:  "test",
		Status:     "running",
	}); err != nil {
		t.Fatalf("BeginActivityLog: %v", err)
	}
	if !eventExists(t, store, late) {
		t.Fatalf("opportunistic sweep ran before the 24h cadence elapsed")
	}
}

// TestSweepOrphanEventsDueAfterActivityLogPrune covers AC 3's
// opportunistic path: the activity-log write is the heartbeat, and it
// only sweeps once the cadence has elapsed.
func TestSweepOrphanEventsDueAfterActivityLogPrune(t *testing.T) {
	ctx := context.Background()
	clock := newFakeSweepClock(0)
	store := newSweepStore(t, clock, sweepPolicy())

	orphan := insertEvent(t, store, int64(9999), "task.created")

	beginLog := func() {
		t.Helper()
		if _, err := store.BeginActivityLog(ctx, domain.ActivityLog{
			Source:     domain.ActivitySourceMCP,
			Entrypoint: "test",
			Operation:  "test",
			Status:     "running",
		}); err != nil {
			t.Fatalf("BeginActivityLog: %v", err)
		}
	}

	beginLog()
	if !eventExists(t, store, orphan) {
		t.Fatalf("sweep ran before the cadence elapsed")
	}

	clock.Advance(25 * time.Hour)
	beginLog()
	if eventExists(t, store, orphan) {
		t.Fatalf("sweep did not run after the cadence elapsed")
	}
}

// TestSweepOrphanEventsRetryScheduling covers AC 7: a capped pass
// reschedules on the retry cadence instead of the steady interval.
func TestSweepOrphanEventsRetryScheduling(t *testing.T) {
	ctx := context.Background()
	clock := newFakeSweepClock(0)
	policy := sweepPolicy()
	policy.BatchRows = 1
	policy.MaxRowsPerPass = 1
	store := newSweepStore(t, clock, policy)

	insertEvent(t, store, int64(9999), "task.created")
	insertEvent(t, store, int64(9999), "task.created")
	insertEvent(t, store, int64(9999), "task.created")

	capped, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}
	if !capped.Limited {
		t.Fatalf("Limited = false, want true")
	}
	if store.orphanSweepDue() {
		t.Fatalf("sweep is due immediately after a capped pass; the retry cadence must apply")
	}
	clock.Advance(59 * time.Minute)
	if store.orphanSweepDue() {
		t.Fatalf("sweep is due 59m after a capped pass, want the 1h retry cadence")
	}
	clock.Advance(2 * time.Minute)
	if !store.orphanSweepDue() {
		t.Fatalf("sweep is not due 61m after a capped pass; the retry cadence is 1h")
	}

	// Drain, then confirm the successful pass falls back to the steady
	// 24h interval rather than staying on the retry cadence.
	policy.MaxRowsPerPass = 500
	policy.BatchRows = 100
	store.SetOrphanSweepPolicy(policy)
	drained, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("draining SweepOrphanEvents: %v", err)
	}
	if drained.Limited {
		t.Fatalf("draining pass Limited = true, want false")
	}
	clock.Advance(23 * time.Hour)
	if store.orphanSweepDue() {
		t.Fatalf("sweep is due 23h after a clean pass, want the 24h interval")
	}
	clock.Advance(2 * time.Hour)
	if !store.orphanSweepDue() {
		t.Fatalf("sweep is not due 25h after a clean pass")
	}
}

// TestSweepOrphanEventsConcurrentCallsSerialize covers AC 4: concurrent
// in-process callers never double-delete and never error; the loser of
// the guard returns an empty report instead of blocking.
func TestSweepOrphanEventsConcurrentCallsSerialize(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	const total = 200
	for i := 0; i < total; i++ {
		insertEvent(t, store, int64(9999), "task.created")
	}

	const callers = 8
	reports := make([]OrphanSweepReport, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			reports[idx], errs[idx] = store.SweepOrphanEvents(ctx)
		}(i)
	}
	close(start)
	wg.Wait()

	deleted := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
		deleted += reports[i].DeletedRows
	}
	if deleted != total {
		t.Fatalf("total DeletedRows across callers = %d, want %d (no row may be counted twice)", deleted, total)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM events WHERE project_id = 9999`); got != 0 {
		t.Fatalf("orphans left = %d, want 0", got)
	}
}

// TestSweepOrphanEventsEmitsNoDomainEvent covers AC 6/7: the sweep is
// silent — no row is inserted and no subscriber is notified.
func TestSweepOrphanEventsEmitsNoDomainEvent(t *testing.T) {
	ctx := context.Background()
	store := newSweepStore(t, newFakeSweepClock(0), sweepPolicy())

	var mu sync.Mutex
	var published []domain.Event
	bus := events.NewInProcessBus(config.EventsSettings{})
	bus.Subscribe(events.Filter{}, func(_ context.Context, ev domain.Event) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, ev)
	})
	store.SetEventBus(bus)

	insertEvent(t, store, int64(9999), "task.created")
	before := countRows(t, store, `SELECT COUNT(*) FROM events`)

	if _, err := store.SweepOrphanEvents(ctx); err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}

	after := countRows(t, store, `SELECT COUNT(*) FROM events`)
	if after != before-1 {
		t.Fatalf("event count %d -> %d; the sweep must only remove rows, never add one", before, after)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(published) != 0 {
		t.Fatalf("published = %+v, want no domain event", published)
	}
}

// TestSweepOrphanEventsWarningsAreSurfaceSafe covers AC 6: the default
// sink swallows everything (no byte may reach a terminal stream under
// the TUI alternate screen), while an operator-provided sink receives
// the failure. The forced entry point still returns the error so the
// internal caller can react.
func TestSweepOrphanEventsWarningsAreSurfaceSafe(t *testing.T) {
	ctx := context.Background()
	clock := newFakeSweepClock(0)
	store := newSweepStore(t, clock, sweepPolicy())

	// Removing `projects` makes the anti-join unresolvable, which is the
	// simplest reproducible sweep failure.
	if _, err := store.db.ExecContext(ctx, `DROP TABLE projects`); err != nil {
		t.Fatalf("DROP TABLE projects: %v", err)
	}

	if _, err := store.SweepOrphanEvents(ctx); err == nil {
		t.Fatalf("SweepOrphanEvents = nil error, want the failure surfaced to the internal caller")
	}
	// Default sink: no writer wired, so nothing may be emitted anywhere.
	store.sweepOrphanEventsIfDue(ctx)

	var sink bytes.Buffer
	store.SetOrphanSweepWarnWriter(&sink)
	clock.Advance(2 * time.Hour)
	store.sweepOrphanEventsIfDue(ctx)
	if sink.Len() == 0 {
		t.Fatalf("configured warn sink received nothing, want the sweep failure")
	}
	if got := sink.String(); !bytes.Contains([]byte(got), []byte("orphan-event sweep failed")) {
		t.Fatalf("warn sink = %q, want the sweep failure line", got)
	}
}

// TestSweepOrphanEventsToleratesCrossProcessDeletes models AC 4's
// cross-process case: a second connection removes the same candidates
// between two batches. The pass must finish cleanly and delete each row
// exactly once.
func TestSweepOrphanEventsToleratesCrossProcessDeletes(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/omakiten.db"
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	store.orphanSweepNow = newFakeSweepClock(0).Now
	policy := sweepPolicy()
	policy.BatchRows = 10
	store.SetOrphanSweepPolicy(policy)

	other, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open second store: %v", err)
	}
	defer func() { _ = other.Close() }()

	for i := 0; i < 100; i++ {
		insertEvent(t, store, int64(9999), "task.created")
	}
	// The "other process" wins the race for half the backlog.
	if _, err := other.db.ExecContext(ctx, `DELETE FROM events WHERE project_id = 9999 AND id % 2 = 0`); err != nil {
		t.Fatalf("competing delete: %v", err)
	}

	report, err := store.SweepOrphanEvents(ctx)
	if err != nil {
		t.Fatalf("SweepOrphanEvents: %v", err)
	}
	if report.DeletedRows != 50 {
		t.Fatalf("DeletedRows = %d, want 50 (the rows the other process left behind)", report.DeletedRows)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM events WHERE project_id = 9999`); got != 0 {
		t.Fatalf("orphans left = %d, want 0", got)
	}
}
