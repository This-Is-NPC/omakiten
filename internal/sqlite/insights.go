package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// DefaultStuckDays mirrors domain.DefaultStuckDays for callers inside the
// sqlite package; the canonical value lives in domain so the app layer and
// this repository can never disagree on the fallback threshold.
const DefaultStuckDays = domain.DefaultStuckDays

// defaultStuckBuckets are the fallback in-flight bucket ids the stuck scan
// uses when the caller supplies none: dev(2) and review(3) in the canonical
// bundle (1=backlog 2=dev 3=review 4=done). Callers that know the active
// workflow (TUI, MCP service) pass the real in-flight roster via
// Workflow.InFlightBucketIDs so presets with different bucket ids scan the
// right stages; the fallback only covers legacy/headless callers.
var defaultStuckBuckets = []int64{2, 3}

// Insights computes all six today-insights on demand (no cache) and returns
// them in one struct. stuckDays parameterises insight 1; pass
// DefaultStuckDays for the standard threshold. projectID > 0 scopes the
// task-shaped insights (stuck, cycle time, WIP, guards, per-model) to one
// project; 0 returns the global view. The error-loop insight reads the
// errors/solutions tables, which carry their own project_id column.
//
// stuckBuckets is tri-state, so a caller that resolved its workflow to an
// EMPTY in-flight set is not silently handed the canonical fallback:
//   - nil            → caller could not resolve a workflow: fall back to the
//                      canonical dev/review ids (legacy/headless callers).
//   - non-nil, empty → workflow known, no in-flight stage exists (1-/2-bucket
//                      preset): the stuck scan matches nothing, not the
//                      fallback.
//   - non-nil, filled → scan exactly these bucket ids.
//
// Every sub-insight is built so an empty history yields HasData=false rather
// than a silent zero: the caller can then render "no data yet" instead of
// mistaking a fresh board for a perfectly healthy one.
func (s *Store) Insights(ctx context.Context, projectID int64, stuckDays int, stuckBuckets []int64) (domain.Insights, error) {
	if stuckDays <= 0 {
		stuckDays = DefaultStuckDays
	}
	if stuckBuckets == nil {
		stuckBuckets = defaultStuckBuckets
	}
	out := domain.Insights{StuckDays: stuckDays}

	var err error
	if out.Stuck, err = s.insightStuck(ctx, projectID, stuckDays, stuckBuckets); err != nil {
		return out, err
	}
	if out.CycleTime, err = s.insightCycleTime(ctx, projectID); err != nil {
		return out, err
	}
	if out.WIP, err = s.insightWIP(ctx, projectID); err != nil {
		return out, err
	}
	if out.Guards, err = s.insightGuards(ctx, projectID); err != nil {
		return out, err
	}
	if out.ErrorLoop, err = s.insightErrorLoop(ctx, projectID); err != nil {
		return out, err
	}
	if out.PerModel, err = s.insightPerModel(ctx, projectID); err != nil {
		return out, err
	}
	return out, nil
}

// projectFilter appends `<kw> <col> = ?` to a query when projectID > 0,
// returning the (possibly extended) args slice. col is the fully-qualified
// column (e.g. "t.project_id" / "project_id") so callers that join can
// disambiguate. kw is the joining keyword — "AND" when the clause already
// has a WHERE, "WHERE" when it does not — so every query shape in this file
// flows through the same helper instead of hand-rolling the conditional.
func projectFilter(clause string, args []any, col string, projectID int64, kw string) (string, []any) {
	if projectID <= 0 {
		return clause, args
	}
	return clause + " " + kw + " " + col + " = ?", append(args, projectID)
}

// The insight*SQL builders below return the exact (query, args) pair the
// Store methods execute. They are package-level (not inlined in the
// methods) so TestInsightsQueryPlansAreIndexBacked can EXPLAIN the
// production SQL itself — a hand-copied probe would silently drift from
// the queries it claims to cover.

// insightStuckSQL — INSIGHT 1 query. Tasks in the supplied in-flight
// buckets whose last task.moved event is older than stuckDays. last_move is
// MAX(created_at) over the task.moved rows per task; days_stuck is
// julianday('now') minus that.
func insightStuckSQL(projectID int64, stuckDays int, stuckBuckets []int64) (string, []any) {
	in := make([]string, len(stuckBuckets))
	args := make([]any, 0, len(stuckBuckets)+2)
	for i, id := range stuckBuckets {
		in[i] = "?"
		args = append(args, id)
	}
	where := `
WITH last_move AS (
  SELECT entity_id AS task_id, MAX(created_at) AS moved_at
  FROM events
  WHERE entity_type = 'task' AND event_type = 'task.moved'
  GROUP BY entity_id
)
SELECT t.id, t.bucket_id,
  CAST(julianday('now') - julianday(lm.moved_at) AS INTEGER) AS days_stuck,
  t.title
FROM tasks t
JOIN last_move lm ON lm.task_id = t.id
WHERE t.state = 'active'
  AND t.bucket_id IN (` + strings.Join(in, ", ") + `)
  AND julianday('now') - julianday(lm.moved_at) > ?`
	args = append(args, stuckDays)
	where, args = projectFilter(where, args, "t.project_id", projectID, "AND")
	return where + `
ORDER BY days_stuck DESC, t.id`, args
}

// insightStuck — INSIGHT 1. HasData is true whenever there is any
// task.moved history to reason about, so "0 stuck" reads as a genuine
// all-clear rather than "no data".
func (s *Store) insightStuck(ctx context.Context, projectID int64, stuckDays int, stuckBuckets []int64) (domain.StuckInsight, error) {
	out := domain.StuckInsight{Tasks: []domain.StuckTask{}}

	// HasData probe: is there any task.moved history (in scope) at all?
	hasData, err := s.hasTaskMoveHistory(ctx, projectID)
	if err != nil {
		return out, err
	}
	out.HasData = hasData

	// An authoritative-empty roster (workflow known, no in-flight stage)
	// means nothing can be stuck — and an `IN ()` clause is invalid SQL, so
	// short-circuit rather than build a query. HasData still reflects whether
	// any move history exists, so the surface reads "nothing stuck" vs "no
	// history" correctly.
	if len(stuckBuckets) == 0 {
		return out, nil
	}

	query, args := insightStuckSQL(projectID, stuckDays, stuckBuckets)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("insight stuck: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var st domain.StuckTask
		if err := rows.Scan(&st.TaskID, &st.BucketID, &st.DaysStuck, &st.Title); err != nil {
			return out, err
		}
		out.Tasks = append(out.Tasks, st)
	}
	return out, rows.Err()
}

// insightCycleTimeSQL — INSIGHT 2 query. Average dwell per from-bucket.
// Dwell is the gap between the move that entered a bucket (LAG over
// created_at, partitioned by task) and the move that left it, attributed to
// json_extract(payload,'$.from'). The first move of a task has a NULL prev
// and is dropped (no entry timestamp to measure from).
func insightCycleTimeSQL(projectID int64) (string, []any) {
	inner := `
  SELECT entity_id,
    json_extract(payload, '$.from') AS from_bucket,
    created_at,
    LAG(created_at) OVER (PARTITION BY entity_id ORDER BY created_at) AS prev_at
  FROM events
  WHERE entity_type = 'task' AND event_type = 'task.moved'`
	args := []any{}
	inner, args = projectFilter(inner, args, "project_id", projectID, "AND")

	return `
WITH moves AS (` + inner + `
)
SELECT from_bucket,
  COUNT(*) AS samples,
  AVG(julianday(created_at) - julianday(prev_at)) AS avg_dwell_days
FROM moves
WHERE prev_at IS NOT NULL AND from_bucket IS NOT NULL
GROUP BY from_bucket
ORDER BY avg_dwell_days DESC`, args
}

// insightCycleTime — INSIGHT 2. The slowest bucket is the bottleneck.
// HasData is false only when no completed dwell interval exists.
func (s *Store) insightCycleTime(ctx context.Context, projectID int64) (domain.CycleTimeInsight, error) {
	out := domain.CycleTimeInsight{Buckets: []domain.BucketDwell{}}

	query, args := insightCycleTimeSQL(projectID)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("insight cycle time: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var b domain.BucketDwell
		var avg sql.NullFloat64
		if err := rows.Scan(&b.FromBucket, &b.Samples, &avg); err != nil {
			return out, err
		}
		b.AvgDwellDays = avg.Float64
		out.Buckets = append(out.Buckets, b)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Buckets) > 0 {
		out.HasData = true
		// Rows arrive ordered by avg_dwell_days DESC, so the first is the
		// bottleneck. Guard the slice access defensively all the same.
		out.Bottleneck = out.Buckets[0].FromBucket
	}
	return out, nil
}

// insightWIPSQL — INSIGHT 3 query. Count of state='active', not-yet-completed
// tasks per bucket_id. completed_at IS NULL excludes tasks sitting in the
// terminal bucket ("done" tasks stay state='active' by schema design), so
// the reading is genuinely work-in-progress rather than the whole board.
func insightWIPSQL(projectID int64) (string, []any) {
	where := `
SELECT bucket_id, COUNT(*) AS wip
FROM tasks
WHERE state = 'active' AND completed_at IS NULL`
	args := []any{}
	where, args = projectFilter(where, args, "project_id", projectID, "AND")
	return where + `
GROUP BY bucket_id
ORDER BY bucket_id`, args
}

// insightWIP — INSIGHT 3. This is the live board shape, so HasData is true
// whenever any in-progress task exists; a fully empty board reads
// HasData=false.
func (s *Store) insightWIP(ctx context.Context, projectID int64) (domain.WIPInsight, error) {
	out := domain.WIPInsight{Buckets: []domain.BucketWIP{}}

	query, args := insightWIPSQL(projectID)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("insight wip: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var b domain.BucketWIP
		var bucketID sql.NullInt64
		if err := rows.Scan(&bucketID, &b.Count); err != nil {
			return out, err
		}
		b.BucketID = bucketID.Int64
		out.Buckets = append(out.Buckets, b)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.HasData = len(out.Buckets) > 0
	return out, nil
}

// insightGuardsSQL — INSIGHT 4 query. guard.violated events grouped by
// (rule, tag) extracted from the JSON payload, with an all-time Hits count
// and a Recent7d trend (hits in the last 7 days). Built with
// sqlutil.ConditionalCount so the trend tally shares the metrics layer's
// counting idiom.
func insightGuardsSQL(projectID int64) (string, []any) {
	recent7d := sqlutil.ConditionalCount("created_at >= datetime('now', '-7 days')")
	where := `
SELECT
  json_extract(payload, '$.rule') AS rule,
  json_extract(payload, '$.tag') AS tag,
  COUNT(*) AS hits,
  ` + recent7d + ` AS recent_7d
FROM events
WHERE event_type = 'guard.violated'`
	args := []any{}
	where, args = projectFilter(where, args, "project_id", projectID, "AND")
	return where + `
GROUP BY rule, tag
ORDER BY hits DESC, rule, tag`, args
}

// insightGuards — INSIGHT 4. HasData is false only when no guard has ever
// tripped.
func (s *Store) insightGuards(ctx context.Context, projectID int64) (domain.GuardInsight, error) {
	out := domain.GuardInsight{Hotspots: []domain.GuardHotspot{}}

	query, args := insightGuardsSQL(projectID)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("insight guards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var h domain.GuardHotspot
		var rule, tag sql.NullString
		if err := rows.Scan(&rule, &tag, &h.Hits, &h.Recent7d); err != nil {
			return out, err
		}
		h.Rule = rule.String
		h.Tag = tag.String
		out.Hotspots = append(out.Hotspots, h)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.HasData = len(out.Hotspots) > 0
	return out, nil
}

// insightErrorLoopTotalSQL / insightErrorLoopResolvedSQL — INSIGHT 5
// queries. Total counts every recorded error; Resolved counts distinct
// errors that have at least one solution with success=1.
func insightErrorLoopTotalSQL(projectID int64) (string, []any) {
	return projectFilter("SELECT COUNT(*) FROM errors", []any{}, "project_id", projectID, "WHERE")
}

func insightErrorLoopResolvedSQL(projectID int64) (string, []any) {
	return projectFilter(`
SELECT COUNT(DISTINCT e.id)
FROM errors e
JOIN solutions s ON s.error_id = e.id AND s.success = 1`, []any{}, "e.project_id", projectID, "WHERE")
}

// insightErrorLoop — INSIGHT 5. Open = Total - Resolved (clamped at 0).
// HasData is true whenever any error has been recorded, so "0 open" reads
// as "all resolved" rather than "no errors yet".
func (s *Store) insightErrorLoop(ctx context.Context, projectID int64) (domain.ErrorLoopInsight, error) {
	var out domain.ErrorLoopInsight

	totalQuery, totalArgs := insightErrorLoopTotalSQL(projectID)
	if err := s.db.QueryRowContext(ctx, totalQuery, totalArgs...).Scan(&out.Total); err != nil {
		return out, fmt.Errorf("insight error loop total: %w", err)
	}

	resolvedQuery, resolvedArgs := insightErrorLoopResolvedSQL(projectID)
	if err := s.db.QueryRowContext(ctx, resolvedQuery, resolvedArgs...).Scan(&out.Resolved); err != nil {
		return out, fmt.Errorf("insight error loop resolved: %w", err)
	}

	out.Open = out.Total - out.Resolved
	if out.Open < 0 {
		out.Open = 0
	}
	out.HasData = out.Total > 0
	return out, nil
}

// insightPerModelSQL — INSIGHT 6 query (task 1353, partial-state gated).
//
// The moves CTE LAGs over ALL task.moved rows — agent AND human/system —
// so an interval always measures the gap since the immediately preceding
// move of that task. The dwell aggregation then keeps only the intervals
// whose LEAVING row is agent-stamped: the model is charged for the time
// since the last move by anyone, matching insight 2's all-moves dwell
// semantics. Filtering the inner stream to agent rows instead would let an
// interleaved human move silently stretch a model's interval across
// multiple buckets.
//
// The samples roster counts stamped TASK events only (entity_type='task') —
// task 1353's AC defines SampleSize as stamped task events, so stamped
// error/solution traffic must never inflate the partial-state gate input.
func insightPerModelSQL(projectID int64) (string, []any) {
	// Dwell stream: every task.moved row, so LAG sees human moves too.
	dwellInner := `
    SELECT entity_id, agent_model, created_at,
      LAG(created_at) OVER (PARTITION BY entity_id ORDER BY created_at) AS prev_at
    FROM events
    WHERE entity_type = 'task' AND event_type = 'task.moved'`
	dwellArgs := []any{}
	dwellInner, dwellArgs = projectFilter(dwellInner, dwellArgs, "project_id", projectID, "AND")

	// Guard violations per model, attribution-filtered. We keep the raw
	// count AND the distinct task count so the presenter can express a
	// per-task rate that is comparable across models of different volume.
	guardWhere := `
  SELECT agent_model,
    COUNT(*) AS guard_violations,
    COUNT(DISTINCT entity_id) AS guard_tasks
  FROM events
  WHERE event_type = 'guard.violated' AND ` + sqlutil.AgentAttributedFilter
	guardArgs := []any{}
	guardWhere, guardArgs = projectFilter(guardWhere, guardArgs, "project_id", projectID, "AND")

	// Sample roster: every stamped agent_model, with the count of stamped
	// TASK events feeding it (the partial-state gate input) and its earliest
	// stamped timestamp (the "sample since" date). This doubles as the model
	// roster: every model that surfaces has at least one stamped task event,
	// so a model with guards-but-no-dwell still appears with the missing
	// side zero.
	sampleWhere := `
  SELECT agent_model,
    COUNT(*) AS sample_size,
    MIN(created_at) AS first_stamped_at
  FROM events
  WHERE entity_type = 'task' AND ` + sqlutil.AgentAttributedFilter
	sampleArgs := []any{}
	sampleWhere, sampleArgs = projectFilter(sampleWhere, sampleArgs, "project_id", projectID, "AND")

	// Arg order follows the CTE definition order in the SQL below:
	// samples, then dwell, then guards.
	args := append([]any{}, sampleArgs...)
	args = append(args, dwellArgs...)
	args = append(args, guardArgs...)

	return `
WITH samples AS (` + sampleWhere + `
  GROUP BY agent_model
),
moves AS (` + dwellInner + `
),
dwell AS (
  SELECT agent_model,
    AVG(julianday(created_at) - julianday(prev_at)) AS avg_dwell,
    COUNT(*) AS dwell_samples
  FROM moves
  WHERE prev_at IS NOT NULL AND ` + sqlutil.AgentAttributedFilter + `
  GROUP BY agent_model
),
guards AS (` + guardWhere + `
  GROUP BY agent_model
)
SELECT s.agent_model,
  COALESCE(d.avg_dwell, 0.0) AS avg_dwell_days,
  COALESCE(d.dwell_samples, 0) AS dwell_samples,
  COALESCE(g.guard_violations, 0) AS guard_violations,
  COALESCE(g.guard_tasks, 0) AS guard_tasks,
  s.sample_size,
  s.first_stamped_at
FROM samples s
LEFT JOIN dwell d ON d.agent_model = s.agent_model
LEFT JOIN guards g ON g.agent_model = s.agent_model
ORDER BY guard_violations DESC, avg_dwell_days DESC, s.agent_model`, args
}

// insightPerModel — INSIGHT 6. Per agent_model: cycle time (avg LAG-based
// dwell over the all-moves stream, attributed to the leaving model) and
// guard violations per task (raw count + a per-task rate). Non-agent rows
// (agent_model='') — including every pre-stamp event — are excluded from
// the roster and the dwell attribution via sqlutil.AgentAttributedFilter so
// human/system/pre-stamp traffic never earns a per-model row.
//
// Each row carries SampleSize (count of stamped TASK events feeding the
// model) and FirstStampedAt (the model's earliest stamped task event). A row
// whose SampleSize is below domain.MinModelSampleSize is flagged Partial so
// both surfaces render "sample since <date>, N rows" instead of a confident
// average on a tiny n — never a silent/misleading zero.
//
// DEFERRED (out of scope, task 1353): a model-score fusing
// closes-fast-AND-no-rework needs a `rework` signal not yet defined in the
// event log; no score is computed here until that signal exists.
func (s *Store) insightPerModel(ctx context.Context, projectID int64) (domain.PerModelInsight, error) {
	out := domain.PerModelInsight{Models: []domain.ModelContrast{}}

	query, args := insightPerModelSQL(projectID)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, fmt.Errorf("insight per-model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var m domain.ModelContrast
		var guardTasks int
		var firstStamped sql.NullString
		if err := rows.Scan(&m.AgentModel, &m.AvgDwellDays, &m.DwellSamples, &m.GuardViolations, &guardTasks, &m.SampleSize, &firstStamped); err != nil {
			return out, err
		}
		m.FirstStampedAt = firstStamped.String
		if guardTasks > 0 {
			m.GuardsPerTask = float64(m.GuardViolations) / float64(guardTasks)
		}
		m.Partial = m.SampleSize < domain.MinModelSampleSize
		out.Models = append(out.Models, m)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.HasData = len(out.Models) > 0
	return out, nil
}

// hasTaskMoveHistorySQL — HasData probe query for insight 1.
func hasTaskMoveHistorySQL(projectID int64) (string, []any) {
	where := `SELECT EXISTS(
  SELECT 1 FROM events
  WHERE entity_type = 'task' AND event_type = 'task.moved'`
	args := []any{}
	where, args = projectFilter(where, args, "project_id", projectID, "AND")
	return where + ")", args
}

// hasTaskMoveHistory reports whether any task.moved event exists in scope.
// The stuck insight uses it to set HasData independently of whether any task
// crossed the staleness threshold, so an empty board (no moves) is
// distinguishable from a board where nothing is stuck.
func (s *Store) hasTaskMoveHistory(ctx context.Context, projectID int64) (bool, error) {
	query, args := hasTaskMoveHistorySQL(projectID)
	var exists int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("insight stuck has-data probe: %w", err)
	}
	return exists == 1, nil
}
