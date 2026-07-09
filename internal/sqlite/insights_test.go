package sqlite

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// seedInsightsFixture lays a deterministic, fixed-offset history into a fresh
// store so each insight has known expected values. It mirrors the shape of
// scripts/seed_insights.sql but in miniature, using relative datetime offsets
// so the assertions are stable regardless of wall-clock.
//
// Board after seeding (project_id = 1):
//   - task 1: dev,    last move 10d ago  -> STUCK (10 > 7)
//   - task 2: review, last move  3d ago  -> not stuck
//   - task 3: dev,    last move  1d ago  -> not stuck (active WIP)
//   - task 4: done,   last move  2d ago  -> terminal, never stuck
//   - task 5: backlog (no moves)         -> WIP backlog, no move history
//
// task.moved history (drives cycle time + per-model dwell):
//   - task 1 (opus):   backlog->dev @ -12d, dev->review @ -11d, review->dev @ -10d
//     dwell from=backlog: 1d, from=dev: 1d, from=review: 1d
//   - task 2 (sonnet): dev->review @ -3d  (first move, no prev -> no dwell)
//   - task 4 (opus):   review->done @ -2d (first move, no prev -> no dwell)
func seedInsightsFixture(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed exec %q: %v", q, err)
		}
	}

	// A project row (id 1) so the project-scoped path has something to scope to.
	exec(`INSERT INTO projects(id, name, slug, root_path) VALUES (1, 'P', 'p', '/p')`)

	// Migration 020 dropped the workflows/workflow_buckets tables and rebuilt
	// `tasks` without the bucket FK — bucket_id is now a free integer holding
	// the bundle-local bucket id (1=backlog 2=dev 3=review 4=done). So the
	// fixture inserts task rows directly with no workflow chain to satisfy.
	//
	// Tasks. state CHECK allows only ('active','archived'); "done" = bucket 4.
	exec(`INSERT INTO tasks(id, project_id, bucket_id, priority_id, state, title, completed_at) VALUES
		(1, 1, 2, 2, 'active', 'stuck in dev',  NULL),
		(2, 1, 3, 2, 'active', 'fresh review',  NULL),
		(3, 1, 2, 2, 'active', 'active dev',    NULL),
		(4, 1, 4, 2, 'active', 'done task',     datetime('now','-2 days')),
		(5, 1, 1, 2, 'active', 'backlog item',  NULL)`)

	// task.moved history.
	moved := func(taskID int64, from, to string, daysAgo int, model string) {
		t.Helper()
		exec(`INSERT INTO events
			(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
			VALUES ('task', ?, 1, 'task.moved', json_object('from', ?, 'to', ?), 'agent', ?, datetime('now', ?))`,
			taskID, from, to, model, daysAgoArg(daysAgo))
	}
	moved(1, "backlog", "dev", 12, "claude-opus-4-8")
	moved(1, "dev", "review", 11, "claude-opus-4-8")
	moved(1, "review", "dev", 10, "claude-opus-4-8") // last move -> stuck @ 10d
	moved(2, "dev", "review", 3, "claude-sonnet-4-6")
	moved(4, "review", "done", 2, "claude-opus-4-8")

	// guard.violated history (rule/tag hotspots + per-model guard counts).
	guard := func(taskID int64, rule, tag string, daysAgo int, model string) {
		t.Helper()
		exec(`INSERT INTO events
			(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
			VALUES ('task', ?, 1, 'guard.violated', json_object('rule', ?, 'tag', ?), 'agent', ?, datetime('now', ?))`,
			taskID, rule, tag, model, daysAgoArg(daysAgo))
	}
	guard(1, "comments_tagged", "self-branch", 10, "claude-opus-4-8")
	guard(2, "comments_tagged", "self-branch", 3, "claude-sonnet-4-6") // recent (<=7d)
	guard(3, "comments_tagged", "self-branch", 1, "claude-opus-4-8")   // recent
	guard(1, "workflow_transition", "invalid-move", 9, "claude-opus-4-8")

	// A non-agent (human) guard row must be EXCLUDED from per-model contrast.
	exec(`INSERT INTO events
		(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
		VALUES ('task', 1, 1, 'guard.violated', json_object('rule','comments_tagged','tag','documentation'), 'human', '', datetime('now','-1 days'))`)

	// A stamped NON-task event (entity_type='error') for sonnet. SampleSize is
	// defined as stamped TASK events (task 1353 AC), so this row must never
	// inflate the partial-state gate input — without the entity_type filter it
	// would flip sonnet's expected sample from 2 to 3.
	exec(`INSERT INTO events
		(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
		VALUES ('error', 1, 1, 'error.recorded', '{}', 'agent', 'claude-sonnet-4-6', datetime('now','-4 days'))`)

	// errors + solutions (error loop). 3 errors, 1 resolved (success=1).
	exec(`INSERT INTO errors(id, description, project_id, created_at) VALUES
		(1, 'err one', 1, datetime('now','-9 days')),
		(2, 'err two', 1, datetime('now','-5 days')),
		(3, 'err three', 1, datetime('now','-2 days'))`)
	exec(`INSERT INTO solutions(error_id, description, success, created_at) VALUES
		(1, 'fixed it', 1, datetime('now','-8 days')),
		(2, 'did not work', 0, datetime('now','-4 days'))`)
}

// daysAgoArg renders the SQLite relative modifier for N days in the past.
func daysAgoArg(days int) string {
	return "-" + strconv.Itoa(days) + " days"
}

func openInsightsStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "omakiten.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestInsightsStuckTasks(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if !got.Stuck.HasData {
		t.Fatalf("Stuck.HasData = false, want true (move history exists)")
	}
	if len(got.Stuck.Tasks) != 1 {
		t.Fatalf("stuck tasks = %d, want 1: %+v", len(got.Stuck.Tasks), got.Stuck.Tasks)
	}
	st := got.Stuck.Tasks[0]
	if st.TaskID != 1 {
		t.Fatalf("stuck task id = %d, want 1", st.TaskID)
	}
	if st.BucketID != 2 {
		t.Fatalf("stuck bucket = %d, want 2 (dev)", st.BucketID)
	}
	if st.DaysStuck < 9 || st.DaysStuck > 11 {
		t.Fatalf("days_stuck = %d, want ~10", st.DaysStuck)
	}
	if st.Title != "stuck in dev" {
		t.Fatalf("title = %q, want 'stuck in dev'", st.Title)
	}
}

func TestInsightsCycleTime(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if !got.CycleTime.HasData {
		t.Fatalf("CycleTime.HasData = false, want true")
	}
	// task 1 has 3 moves: backlog->dev, dev->review, review->dev. Only moves
	// with a LAG prev yield a dwell interval, so the FIRST move (from=backlog)
	// is never measured — dwell is attributed to from=dev (2nd move) and
	// from=review (3rd move), each ~1d. backlog correctly never appears
	// because it is always a task's first move.
	byBucket := map[string]float64{}
	samples := map[string]int{}
	for _, b := range got.CycleTime.Buckets {
		byBucket[b.FromBucket] = b.AvgDwellDays
		samples[b.FromBucket] = b.Samples
	}
	if _, ok := byBucket["backlog"]; ok {
		t.Fatalf("from=backlog should never have a dwell interval (always first move): %+v", got.CycleTime.Buckets)
	}
	for _, fb := range []string{"dev", "review"} {
		avg, ok := byBucket[fb]
		if !ok {
			t.Fatalf("missing from-bucket %q in cycle time: %+v", fb, got.CycleTime.Buckets)
		}
		if avg < 0.5 || avg > 1.5 {
			t.Fatalf("from=%s avg_dwell = %.2f, want ~1.0", fb, avg)
		}
		if samples[fb] != 1 {
			t.Fatalf("from=%s samples = %d, want 1", fb, samples[fb])
		}
	}
	if got.CycleTime.Bottleneck == "" {
		t.Fatalf("Bottleneck empty, want a from-bucket")
	}
}

func TestInsightsWIP(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if !got.WIP.HasData {
		t.Fatalf("WIP.HasData = false, want true")
	}
	// In-progress tasks: backlog(1)=1, dev(2)=2, review(3)=1. Task 4 sits in
	// the terminal bucket with completed_at set — completed work is NOT WIP
	// and must be excluded.
	want := map[int64]int{1: 1, 2: 2, 3: 1}
	got2 := map[int64]int{}
	for _, b := range got.WIP.Buckets {
		got2[b.BucketID] = b.Count
	}
	for bucket, count := range want {
		if got2[bucket] != count {
			t.Fatalf("WIP bucket %d = %d, want %d (all: %+v)", bucket, got2[bucket], count, got.WIP.Buckets)
		}
	}
	if _, ok := got2[4]; ok {
		t.Fatalf("WIP includes the done bucket (completed task leaked): %+v", got.WIP.Buckets)
	}
}

func TestInsightsGuardHotspots(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if !got.Guards.HasData {
		t.Fatalf("Guards.HasData = false, want true")
	}
	var selfBranch *struct {
		Hits, Recent int
	}
	for _, h := range got.Guards.Hotspots {
		if h.Rule == "comments_tagged" && h.Tag == "self-branch" {
			selfBranch = &struct{ Hits, Recent int }{h.Hits, h.Recent7d}
		}
	}
	if selfBranch == nil {
		t.Fatalf("comments_tagged/self-branch hotspot missing: %+v", got.Guards.Hotspots)
	}
	// 3 self-branch hits total (10d, 3d, 1d); 2 within the last 7 days.
	if selfBranch.Hits != 3 {
		t.Fatalf("self-branch hits = %d, want 3", selfBranch.Hits)
	}
	if selfBranch.Recent != 2 {
		t.Fatalf("self-branch recent_7d = %d, want 2", selfBranch.Recent)
	}
	// Top hotspot (ordered by hits desc) is self-branch with 3.
	if got.Guards.Hotspots[0].Tag != "self-branch" {
		t.Fatalf("top hotspot tag = %q, want self-branch", got.Guards.Hotspots[0].Tag)
	}
}

func TestInsightsErrorLoop(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if !got.ErrorLoop.HasData {
		t.Fatalf("ErrorLoop.HasData = false, want true")
	}
	if got.ErrorLoop.Total != 3 {
		t.Fatalf("error total = %d, want 3", got.ErrorLoop.Total)
	}
	if got.ErrorLoop.Resolved != 1 {
		t.Fatalf("error resolved = %d, want 1", got.ErrorLoop.Resolved)
	}
	if got.ErrorLoop.Open != 2 {
		t.Fatalf("error open = %d, want 2", got.ErrorLoop.Open)
	}
}

func TestInsightsPerModelExcludesNonAgent(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if !got.PerModel.HasData {
		t.Fatalf("PerModel.HasData = false, want true")
	}
	byModel := map[string]domain.ModelContrast{}
	for _, m := range got.PerModel.Models {
		byModel[m.AgentModel] = m
	}

	// Non-agent ('') row must be excluded.
	if _, ok := byModel[""]; ok {
		t.Fatalf("empty agent_model leaked into per-model contrast: %+v", got.PerModel.Models)
	}

	opus, ok := byModel["claude-opus-4-8"]
	if !ok {
		t.Fatalf("claude-opus-4-8 missing from per-model: %+v", got.PerModel.Models)
	}
	// Opus: 2 guard violations (self-branch @10d, workflow @9d... plus
	// self-branch @1d = 3 total guard rows on opus). Re-check: opus guards =
	// self-branch(10d), workflow(9d), self-branch(1d) = 3.
	if opus.GuardViolations != 3 {
		t.Fatalf("opus guard violations = %d, want 3", opus.GuardViolations)
	}
	// Opus dwell samples: task 1 has 3 moves -> 2 dwell intervals from opus
	// (backlog->dev gives no prev; the 2nd and 3rd moves each have a prev).
	if opus.DwellSamples != 2 {
		t.Fatalf("opus dwell samples = %d, want 2", opus.DwellSamples)
	}
	if opus.AvgDwellDays < 0.5 || opus.AvgDwellDays > 1.5 {
		t.Fatalf("opus avg dwell = %.2f, want ~1.0", opus.AvgDwellDays)
	}
	// Opus stamped events: 4 task.moved (tasks 1×3, 4×1) + 3 guard.violated = 7
	// (>= MinModelSampleSize) -> NOT partial; it shows a confident reading.
	if opus.SampleSize != 7 {
		t.Fatalf("opus sample size = %d, want 7 (4 moves + 3 guards)", opus.SampleSize)
	}
	if opus.Partial {
		t.Fatalf("opus must NOT be partial: sample %d >= N=%d", opus.SampleSize, domain.MinModelSampleSize)
	}
	if opus.FirstStampedAt == "" {
		t.Fatalf("opus first_stamped_at empty, want the earliest stamped date")
	}
	// Opus guards span 3 distinct tasks (1, 3, 1) -> tasks {1,3} = 2 -> 3/2.
	if opus.GuardsPerTask < 1.4 || opus.GuardsPerTask > 1.6 {
		t.Fatalf("opus guards/task = %.2f, want 1.5 (3 violations / 2 tasks)", opus.GuardsPerTask)
	}

	sonnet, ok := byModel["claude-sonnet-4-6"]
	if !ok {
		t.Fatalf("claude-sonnet-4-6 missing from per-model: %+v", got.PerModel.Models)
	}
	// Sonnet: 1 guard (self-branch @3d), 0 dwell samples (its only move is a
	// first move with no prev) -> dwell 0.
	if sonnet.GuardViolations != 1 {
		t.Fatalf("sonnet guard violations = %d, want 1", sonnet.GuardViolations)
	}
	if sonnet.DwellSamples != 0 {
		t.Fatalf("sonnet dwell samples = %d, want 0", sonnet.DwellSamples)
	}
	if sonnet.AvgDwellDays != 0 {
		t.Fatalf("sonnet avg dwell = %.2f, want 0 (no completed interval)", sonnet.AvgDwellDays)
	}
	// Sonnet stamped TASK events: 1 task.moved (task 2) + 1 guard.violated = 2
	// (< MinModelSampleSize=5) -> PARTIAL. The fixture also plants a stamped
	// entity_type='error' event for sonnet — it must NOT count (the gate input
	// is stamped task events only), so 2 here also pins the entity filter.
	if sonnet.SampleSize != 2 {
		t.Fatalf("sonnet sample size = %d, want 2 (1 move + 1 guard)", sonnet.SampleSize)
	}
	if !sonnet.Partial {
		t.Fatalf("sonnet must be partial: sample %d < N=%d", sonnet.SampleSize, domain.MinModelSampleSize)
	}
	if sonnet.FirstStampedAt == "" {
		t.Fatalf("sonnet first_stamped_at empty, want the earliest stamped date for partial label")
	}
}

// TestInsightsEmptyStateDistinguishesNoData asserts that a store with no
// history reports HasData=false on every insight rather than a silent zero.
func TestInsightsEmptyStateDistinguishesNoData(t *testing.T) {
	store := openInsightsStore(t)

	got, err := store.Insights(context.Background(), 0, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if got.Stuck.HasData {
		t.Errorf("Stuck.HasData = true on empty store, want false")
	}
	if got.CycleTime.HasData {
		t.Errorf("CycleTime.HasData = true on empty store, want false")
	}
	if got.WIP.HasData {
		t.Errorf("WIP.HasData = true on empty store, want false")
	}
	if got.Guards.HasData {
		t.Errorf("Guards.HasData = true on empty store, want false")
	}
	if got.ErrorLoop.HasData {
		t.Errorf("ErrorLoop.HasData = true on empty store, want false")
	}
	if got.PerModel.HasData {
		t.Errorf("PerModel.HasData = true on empty store, want false")
	}
	// Empty slices, not nil, so JSON renders [] not null.
	if got.Stuck.Tasks == nil || got.WIP.Buckets == nil || got.Guards.Hotspots == nil {
		t.Errorf("empty-state slices should be non-nil for stable JSON")
	}
}

func TestInsightsDefaultStuckDaysFallback(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	// stuckDays <= 0 must fall back to DefaultStuckDays (7), so the 10d task
	// still reports stuck and StuckDays echoes 7.
	got, err := store.Insights(context.Background(), 1, 0, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if got.StuckDays != DefaultStuckDays {
		t.Fatalf("StuckDays = %d, want %d (fallback)", got.StuckDays, DefaultStuckDays)
	}
	if len(got.Stuck.Tasks) != 1 {
		t.Fatalf("stuck tasks under fallback = %d, want 1", len(got.Stuck.Tasks))
	}

	// A large threshold (30d) clears the 10d task.
	got, err = store.Insights(context.Background(), 1, 30, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if len(got.Stuck.Tasks) != 0 {
		t.Fatalf("stuck tasks under 30d threshold = %d, want 0", len(got.Stuck.Tasks))
	}
	// HasData stays true: there IS move history, just nothing stuck.
	if !got.Stuck.HasData {
		t.Fatalf("Stuck.HasData = false under 30d threshold, want true (history exists)")
	}
}

// TestInsightsQueryPlansAreIndexBacked asserts the insight queries ride an
// index on the EVENTS table — the one that grows unbounded — rather than
// full-scanning it at scale. The guard is "no SCAN events full table": the
// planner must reach through idx_events_entity / idx_events_type_started /
// idx_events_agent_type for each event-touching query.
//
// The `tasks` table is deliberately NOT guarded: it is bounded by the active
// board size (orders of magnitude smaller than events), so insightWIP and the
// stuck join full-scanning tasks is acceptable and expected — forcing an index
// there would be premature. The assertion below targets `SCAN events` only.
func TestInsightsQueryPlansAreIndexBacked(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	// EXPLAIN the exact production SQL via the insight*SQL builders — a
	// hand-copied probe would silently drift from the queries it claims to
	// cover.
	type planCase struct {
		name  string
		query string
		args  []any
	}
	var cases []planCase
	add := func(name string, query string, args []any) {
		cases = append(cases, planCase{name: name, query: query, args: args})
	}
	{
		q, a := insightStuckSQL(1, DefaultStuckDays, defaultStuckBuckets)
		add("stuck", q, a)
	}
	{
		q, a := insightCycleTimeSQL(1)
		add("cycle time", q, a)
	}
	{
		q, a := insightWIPSQL(1)
		add("wip", q, a)
	}
	{
		q, a := insightGuardsSQL(1)
		add("guards", q, a)
	}
	{
		q, a := insightErrorLoopTotalSQL(1)
		add("error loop total", q, a)
	}
	{
		q, a := insightErrorLoopResolvedSQL(1)
		add("error loop resolved", q, a)
	}
	{
		q, a := insightPerModelSQL(1)
		add("per-model", q, a)
	}
	{
		q, a := hasTaskMoveHistorySQL(1)
		add("stuck has-data probe", q, a)
	}
	for _, tc := range cases {
		plan := explainQueryPlan(t, store, tc.query, tc.args...)
		if strings.Contains(plan, "SCAN events") && !strings.Contains(plan, "USING INDEX") && !strings.Contains(plan, "USING COVERING INDEX") {
			t.Errorf("%s: plan full-scans events (no index):\n%s", tc.name, plan)
		}
	}
}

// TestInsightsProjectScopeIsolatesData seeds a second project (id 2) with its
// own stuck task, guard, and error, then asserts a projectID=1 call sees only
// project 1's rows and a projectID=2 call sees only project 2's. This pins the
// project-filter branch of every insight (the global view is covered by the
// projectID=0 paths elsewhere).
func TestInsightsProjectScopeIsolatesData(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(context.Background(), q, args...); err != nil {
			t.Fatalf("seed exec %q: %v", q, err)
		}
	}
	// Project 2 with its own stuck dev task, one guard, one open error.
	exec(`INSERT INTO projects(id, name, slug, root_path) VALUES (2, 'P2', 'p2', '/p2')`)
	exec(`INSERT INTO tasks(id, project_id, bucket_id, priority_id, state, title) VALUES
		(101, 2, 2, 2, 'active', 'p2 stuck dev')`)
	exec(`INSERT INTO events
		(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
		VALUES ('task', 101, 2, 'task.moved', json_object('from','backlog','to','dev'), 'agent', 'openai/gpt-5.5', datetime('now','-20 days'))`)
	exec(`INSERT INTO events
		(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
		VALUES ('task', 101, 2, 'guard.violated', json_object('rule','workflow_transition','tag','p2-only'), 'agent', 'openai/gpt-5.5', datetime('now','-2 days'))`)
	exec(`INSERT INTO errors(id, description, project_id, created_at) VALUES (101, 'p2 err', 2, datetime('now','-1 days'))`)

	p1, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights(p1): %v", err)
	}
	p2, err := store.Insights(context.Background(), 2, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights(p2): %v", err)
	}

	// Stuck: p1 has only task 1; p2 has only task 101.
	if len(p1.Stuck.Tasks) != 1 || p1.Stuck.Tasks[0].TaskID != 1 {
		t.Fatalf("p1 stuck = %+v, want only task 1", p1.Stuck.Tasks)
	}
	if len(p2.Stuck.Tasks) != 1 || p2.Stuck.Tasks[0].TaskID != 101 {
		t.Fatalf("p2 stuck = %+v, want only task 101", p2.Stuck.Tasks)
	}
	// Guards: the p2-only tag must not appear in p1.
	for _, h := range p1.Guards.Hotspots {
		if h.Tag == "p2-only" {
			t.Fatalf("p2-only guard leaked into p1: %+v", p1.Guards.Hotspots)
		}
	}
	// Error loop: p1 has 3 errors, p2 has 1.
	if p1.ErrorLoop.Total != 3 {
		t.Fatalf("p1 error total = %d, want 3", p1.ErrorLoop.Total)
	}
	if p2.ErrorLoop.Total != 1 {
		t.Fatalf("p2 error total = %d, want 1", p2.ErrorLoop.Total)
	}
}

// TestInsightsStuckBucketsFollowWorkflow pins the caller-supplied in-flight
// roster: the stuck scan targets exactly the bucket ids it is handed, so a
// preset whose workflow differs from the canonical dev(2)/review(3) pair
// scans its own stages instead of the hardcoded fallback.
func TestInsightsStuckBucketsFollowWorkflow(t *testing.T) {
	store := openInsightsStore(t)
	seedInsightsFixture(t, store)

	// Roster {3} (review only): task 1 sits in dev — must NOT report stuck.
	got, err := store.Insights(context.Background(), 1, DefaultStuckDays, []int64{3})
	if err != nil {
		t.Fatalf("Insights(buckets={3}): %v", err)
	}
	if len(got.Stuck.Tasks) != 0 {
		t.Fatalf("stuck with roster {3} = %+v, want none (task 1 is in dev)", got.Stuck.Tasks)
	}

	// Roster {2} (dev only): task 1 reports stuck again.
	got, err = store.Insights(context.Background(), 1, DefaultStuckDays, []int64{2})
	if err != nil {
		t.Fatalf("Insights(buckets={2}): %v", err)
	}
	if len(got.Stuck.Tasks) != 1 || got.Stuck.Tasks[0].TaskID != 1 {
		t.Fatalf("stuck with roster {2} = %+v, want only task 1", got.Stuck.Tasks)
	}

	// Tri-state contract:
	//   nil            -> canonical fallback {2,3}: task 1 (in dev) is stuck.
	//   non-nil, empty -> authoritative "no in-flight stage": nothing stuck,
	//                     and NO invalid `IN ()` SQL. HasData still true
	//                     (move history exists).
	gotNil, err := store.Insights(context.Background(), 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights(nil buckets): %v", err)
	}
	if len(gotNil.Stuck.Tasks) != 1 || gotNil.Stuck.Tasks[0].TaskID != 1 {
		t.Fatalf("nil roster should use canonical fallback {2,3}: %+v", gotNil.Stuck.Tasks)
	}
	gotEmpty, err := store.Insights(context.Background(), 1, DefaultStuckDays, []int64{})
	if err != nil {
		t.Fatalf("Insights(empty buckets): %v", err)
	}
	if len(gotEmpty.Stuck.Tasks) != 0 {
		t.Fatalf("authoritative-empty roster should report nothing stuck: %+v", gotEmpty.Stuck.Tasks)
	}
	if !gotEmpty.Stuck.HasData {
		t.Fatalf("authoritative-empty roster: HasData should stay true (move history exists)")
	}
}

// TestInsightsPerModelDwellSpansHumanMoves pins the all-moves LAG stream:
// when a human move interleaves between two agent moves, the agent's dwell
// interval measures from the HUMAN move (the task's actual last transition),
// not from the agent's own earlier move. LAGging over an agent-only stream
// would silently stretch the interval across the human hop (~2d instead of
// ~1d here) and diverge from insight 2's all-moves dwell.
func TestInsightsPerModelDwellSpansHumanMoves(t *testing.T) {
	store := openInsightsStore(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed exec %q: %v", q, err)
		}
	}
	exec(`INSERT INTO projects(id, name, slug, root_path) VALUES (1, 'P', 'p', '/p')`)
	exec(`INSERT INTO tasks(id, project_id, bucket_id, priority_id, state, title) VALUES
		(1, 1, 2, 2, 'active', 'interleaved')`)
	move := func(from, to string, daysAgo int, authorType, model string) {
		t.Helper()
		exec(`INSERT INTO events
			(entity_type, entity_id, project_id, event_type, payload, author_type, agent_model, created_at)
			VALUES ('task', 1, 1, 'task.moved', json_object('from', ?, 'to', ?), ?, ?, datetime('now', ?))`,
			from, to, authorType, model, daysAgoArg(daysAgo))
	}
	move("backlog", "dev", 6, "agent", "claude-opus-4-8")
	move("dev", "review", 5, "human", "") // interleaved human move
	move("review", "dev", 4, "agent", "claude-opus-4-8")

	got, err := store.Insights(ctx, 1, DefaultStuckDays, nil)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	var opus *domain.ModelContrast
	for i := range got.PerModel.Models {
		if got.PerModel.Models[i].AgentModel == "claude-opus-4-8" {
			opus = &got.PerModel.Models[i]
		}
	}
	if opus == nil {
		t.Fatalf("opus missing from per-model: %+v", got.PerModel.Models)
	}
	// One agent-leaving interval with a prev: the -4d move, measured from the
	// human's -5d move -> ~1d. (The -6d move has no prev; the human's -5d row
	// is excluded from attribution but participates in LAG.)
	if opus.DwellSamples != 1 {
		t.Fatalf("opus dwell samples = %d, want 1", opus.DwellSamples)
	}
	if opus.AvgDwellDays < 0.7 || opus.AvgDwellDays > 1.3 {
		t.Fatalf("opus avg dwell = %.2f, want ~1.0 (measured from the interleaved human move, not ~2.0)", opus.AvgDwellDays)
	}
	// The human row itself must not earn a roster entry.
	for _, m := range got.PerModel.Models {
		if m.AgentModel == "" {
			t.Fatalf("empty agent_model leaked into roster: %+v", got.PerModel.Models)
		}
	}
}
