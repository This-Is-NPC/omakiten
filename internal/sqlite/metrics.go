package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// AgentMetricsSummary aggregates the domain events emitted by the
// ErrorService into per-model counters. periodClause is "7d", "30d", or
// "all" (anything else falls back to "30d"). projectID > 0 scopes results
// to one project; 0 returns the global view (errors and solutions are
// already cross-project by design).
//
// Returns the per-model rows plus the timestamp the period started at so
// callers can echo it back. Models with `agent_model=""` are excluded —
// those rows are non-agent traffic (TUI human, system internals) and
// would distort the benchmark.
//
// The list of event_type keys this method counts is derived from the
// YAML-loaded domain.EventDefinitions (any entry whose Metric is
// non-empty contributes a bucket). Adding a new bucket therefore lives
// entirely in the kit YAML — no SQL literal to keep in sync.
func (s *Store) AgentMetricsSummary(ctx context.Context, period string, projectID int64) ([]domain.AgentMetrics, string, error) {
	periodClause, since := periodFilter(period)

	metricDefs := metricBucketDefs()
	if len(metricDefs) == 0 {
		// Registry not loaded (defensive: tests that bypass boot wiring would
		// hit this). Return an empty summary so callers can render a no-data
		// view instead of crashing on a malformed SQL statement.
		return []domain.AgentMetrics{}, since, nil
	}

	inPlaceholders := make([]string, len(metricDefs))
	inArgs := make([]any, len(metricDefs))
	for i, def := range metricDefs {
		inPlaceholders[i] = "?"
		inArgs[i] = def.Key
	}
	whereClauses := []string{
		sqlutil.AgentAttributedFilter,
		"event_type IN (" + strings.Join(inPlaceholders, ",") + ")",
	}
	args := append([]any{}, inArgs...)
	if periodClause != "" {
		whereClauses = append(whereClauses, periodClause)
	}
	if projectID > 0 {
		whereClauses = append(whereClauses, "project_id = ?")
		args = append(args, projectID)
	}

	where := strings.Join(whereClauses, " AND ")

	// Emit one SUM(CASE WHEN event_type = ? THEN 1 ELSE 0 END) per metric
	// def, in the order metricBucketDefs() returns. The scan loop below
	// pulls them back out in the same order.
	caseClauses := make([]string, len(metricDefs))
	caseArgs := make([]any, len(metricDefs))
	for i, def := range metricDefs {
		caseClauses[i] = "  " + sqlutil.ConditionalCount("event_type = ?")
		caseArgs[i] = def.Key
	}

	// Order: SELECT prepends case args before WHERE args.
	queryArgs := append([]any{}, caseArgs...)
	queryArgs = append(queryArgs, args...)

	countQuery := `
SELECT
  agent_model,
` + strings.Join(caseClauses, ",\n") + `
FROM events
WHERE ` + where + `
GROUP BY agent_model
ORDER BY 2 DESC, 1
`

	rows, err := s.db.QueryContext(ctx, countQuery, queryArgs...)
	if err != nil {
		return nil, since, fmt.Errorf("metrics summary counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byModel := []domain.AgentMetrics{}
	for rows.Next() {
		var agentModel string
		counts := make([]sql.NullInt64, len(metricDefs))
		scanDest := []any{&agentModel}
		for i := range counts {
			scanDest = append(scanDest, &counts[i])
		}
		if err := rows.Scan(scanDest...); err != nil {
			return nil, since, err
		}
		m := domain.AgentMetrics{
			AgentModel: agentModel,
			Buckets:    make(map[domain.EventMetricBucket]int, len(metricDefs)),
		}
		for i, def := range metricDefs {
			m.Buckets[domain.EventMetricBucket(def.Metric)] = int(counts[i].Int64)
		}
		added := m.Buckets[domain.MetricBucketSolutionAdded]
		if added > 0 {
			m.LikeRate = float64(m.Buckets[domain.MetricBucketSolutionLiked]) / float64(added)
		}
		byModel = append(byModel, m)
	}
	if err := rows.Err(); err != nil {
		return nil, since, err
	}

	if err := s.fillSearchBeforeRecord(ctx, byModel, periodClause, projectID); err != nil {
		return nil, since, err
	}
	return byModel, since, nil
}

// fillSearchBeforeRecord computes the ratio of errors registered after a
// same-session search within a 30-minute lookback window. Sessionless rows
// are ignored — without an agent_session_id we cannot tell two parallel
// agents apart, and correlating across them would inflate the ratio.
//
// Both event_type keys (the "record" trigger and the "search" lookup) are
// resolved through the YAML registry: the record side is the entry whose
// Metric is MetricBucketErrorRecorded, the search side is the entry whose
// Metric is MetricBucketErrorsResearched. A rename in the YAML therefore
// flows through without touching this query.
func (s *Store) fillSearchBeforeRecord(ctx context.Context, models []domain.AgentMetrics, periodClause string, projectID int64) error {
	if len(models) == 0 {
		return nil
	}
	recordedKey, ok := metricKey(domain.MetricBucketErrorRecorded)
	if !ok {
		return nil
	}
	searchedKey, ok := metricKey(domain.MetricBucketErrorsResearched)
	if !ok {
		return nil
	}

	args := []any{recordedKey}
	whereClauses := []string{
		"r.event_type = ?",
		sqlutil.AgentAttributedFilterFor("r"),
		"r.agent_session_id IS NOT NULL AND r.agent_session_id != ''",
	}
	if periodClause != "" {
		whereClauses = append(whereClauses, "r."+periodClause)
	}
	if projectID > 0 {
		whereClauses = append(whereClauses, "r.project_id = ?")
		args = append(args, projectID)
	}

	where := strings.Join(whereClauses, " AND ")

	// The search-before-record tally is the same conditional-count idiom
	// as the per-bucket counts above, but the predicate is a correlated
	// EXISTS: "this record was preceded, in the same session, by a search
	// within the 30-minute lookback". Built through sqlutil.ConditionalCount
	// so it shares the metrics SELECT list's counting shape.
	searchedBeforeRecord := sqlutil.ConditionalCount(`EXISTS (
      SELECT 1 FROM events s
      WHERE s.event_type = ?
        AND s.agent_session_id = r.agent_session_id
        AND s.id < r.id
        AND s.created_at >= datetime(r.created_at, '-30 minutes')
    )`)

	query := `
SELECT
  r.agent_model,
  COUNT(*),
  ` + searchedBeforeRecord + `
FROM events r
WHERE ` + where + `
GROUP BY r.agent_model
`
	queryArgs := append([]any{searchedKey}, args...)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return fmt.Errorf("search-before-record: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type ratio struct {
		sample, searched int
	}
	byModel := map[string]ratio{}
	for rows.Next() {
		var model string
		var sample, searched sql.NullInt64
		if err := rows.Scan(&model, &sample, &searched); err != nil {
			return err
		}
		byModel[model] = ratio{sample: int(sample.Int64), searched: int(searched.Int64)}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range models {
		r, ok := byModel[models[i].AgentModel]
		if !ok || r.sample == 0 {
			continue
		}
		models[i].SessionCorrelatedSample = r.sample
		models[i].SearchBeforeRecordRatio = float64(r.searched) / float64(r.sample)
	}
	return nil
}

// metricBucketDefs returns the YAML-loaded EventDefinitions that have a
// non-empty Metric tag, in a stable order (the order EventDefinitions is
// already sorted into by the loader — alphabetical by Key). The SQL
// builders rely on the order being deterministic so the scan loop matches
// the SELECT projection slot-for-slot.
func metricBucketDefs() []domain.EventDef {
	out := make([]domain.EventDef, 0, 6)
	for _, def := range domain.EventDefinitions {
		if def.Metric == "" {
			continue
		}
		out = append(out, def)
	}
	return out
}

// metricKey returns the event_type key for the YAML registry entry whose
// Metric tag matches the given bucket. The ok return is false when the
// registry has no matching entry — defensive against a kit YAML that
// drops a bucket Phase 2 hard-coded.
func metricKey(bucket domain.EventMetricBucket) (string, bool) {
	for _, def := range domain.EventDefinitions {
		if domain.EventMetricBucket(def.Metric) == bucket {
			return def.Key, true
		}
	}
	return "", false
}

// periodFilter translates the human period string into a `created_at >=
// datetime(...)` clause and returns the ISO timestamp of the start. "all"
// returns ("", "") so the caller can omit the filter and skip echoing
// `since` to the agent.
func periodFilter(period string) (clause, since string) {
	switch period {
	case "7d":
		return "created_at >= datetime('now', '-7 days')", "7 days ago"
	case "all":
		return "", ""
	default: // "30d" and unknown values
		return "created_at >= datetime('now', '-30 days')", "30 days ago"
	}
}
