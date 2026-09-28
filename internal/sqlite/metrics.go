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

	countQuery, queryArgs := metricSummaryQuery(metricDefs, periodClause, projectID)

	rows, err := s.db.QueryContext(ctx, countQuery, queryArgs...)
	if err != nil {
		return nil, since, fmt.Errorf("metrics summary counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byModel, err := scanAgentMetrics(rows, metricDefs)
	if err != nil {
		return nil, since, err
	}

	if err := s.fillSearchBeforeRecord(ctx, byModel, periodClause, projectID); err != nil {
		return nil, since, err
	}
	return byModel, since, nil
}

func metricSummaryQuery(metricDefs []domain.EventDef, periodClause string, projectID int64) (string, []any) {
	where, whereArgs := metricSummaryWhere(metricDefs, periodClause, projectID)
	caseClauses, caseArgs := metricSummaryCases(metricDefs)
	query := `
SELECT
  agent_model,
` + strings.Join(caseClauses, ",\n") + `
FROM events
WHERE ` + where + `
GROUP BY agent_model
ORDER BY 2 DESC, 1
`
	args := append([]any{}, caseArgs...)
	return query, append(args, whereArgs...)
}

func metricSummaryWhere(metricDefs []domain.EventDef, periodClause string, projectID int64) (string, []any) {
	placeholders := make([]string, len(metricDefs))
	args := make([]any, len(metricDefs))
	for i, def := range metricDefs {
		placeholders[i] = "?"
		args[i] = def.Key
	}
	clauses := []string{sqlutil.AgentAttributedFilter, "event_type IN (" + strings.Join(placeholders, ",") + ")"}
	if periodClause != "" {
		clauses = append(clauses, periodClause)
	}
	if projectID > 0 {
		clauses = append(clauses, "project_id = ?")
		args = append(args, projectID)
	}
	return strings.Join(clauses, " AND "), args
}

func metricSummaryCases(metricDefs []domain.EventDef) ([]string, []any) {
	clauses := make([]string, len(metricDefs))
	args := make([]any, len(metricDefs))
	for i, def := range metricDefs {
		clauses[i] = "  " + sqlutil.ConditionalCount("event_type = ?")
		args[i] = def.Key
	}
	return clauses, args
}

func scanAgentMetrics(rows *sql.Rows, metricDefs []domain.EventDef) ([]domain.AgentMetrics, error) {
	var models []domain.AgentMetrics
	for rows.Next() {
		var agentModel string
		counts := make([]sql.NullInt64, len(metricDefs))
		scanDest := []any{&agentModel}
		for i := range counts {
			scanDest = append(scanDest, &counts[i])
		}
		if err := rows.Scan(scanDest...); err != nil {
			return nil, err
		}
		m := domain.AgentMetrics{AgentModel: agentModel, Buckets: make(map[domain.EventMetricBucket]int, len(metricDefs))}
		for i, def := range metricDefs {
			m.Buckets[domain.EventMetricBucket(def.Metric)] = int(counts[i].Int64)
		}
		if added := m.Buckets[domain.MetricBucketSolutionAdded]; added > 0 {
			m.LikeRate = float64(m.Buckets[domain.MetricBucketSolutionLiked]) / float64(added)
		}
		models = append(models, m)
	}
	return models, rows.Err()
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

	query, queryArgs := searchBeforeRecordQuery(recordedKey, searchedKey, periodClause, projectID)
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

func searchBeforeRecordQuery(recordedKey, searchedKey, periodClause string, projectID int64) (string, []any) {
	args := []any{recordedKey}
	clauses := []string{
		"r.event_type = ?",
		sqlutil.AgentAttributedFilterFor("r"),
		"r.agent_session_id IS NOT NULL AND r.agent_session_id != ''",
	}
	if periodClause != "" {
		clauses = append(clauses, "r."+periodClause)
	}
	if projectID > 0 {
		clauses = append(clauses, "r.project_id = ?")
		args = append(args, projectID)
	}
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
WHERE ` + strings.Join(clauses, " AND ") + `
GROUP BY r.agent_model
`
	return query, append([]any{searchedKey}, args...)
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
