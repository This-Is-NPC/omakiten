package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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
// YAML-loaded registry.Definitions() (any entry whose Metric is
// non-empty contributes a bucket). Adding a new bucket therefore lives
// entirely in the kit YAML — no SQL literal to keep in sync.
func (s *Store) AgentMetricsSummary(ctx context.Context, period string, projectID int64) ([]domain.AgentMetrics, string, error) {
	periodClause, since := periodFilter(period)

	query := "SELECT agent_model, COALESCE(project_id, 0), event_type, COUNT(*) FROM events WHERE " + sqlutil.AgentAttributedFilter
	var args []any
	if periodClause != "" {
		query += " AND " + periodClause
	}
	if projectID > 0 {
		query += " AND project_id = ?"
		args = append(args, projectID)
	}
	query += " GROUP BY agent_model, project_id, event_type"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, since, fmt.Errorf("metrics summary counts: %w", err)
	}
	byModel, err := s.scanAgentMetrics(rows)
	_ = rows.Close()
	if err != nil {
		return nil, since, err
	}
	if err := s.fillSearchBeforeRecord(ctx, byModel, periodClause, projectID); err != nil {
		return nil, since, err
	}
	return byModel, since, nil
}

func (s *Store) scanAgentMetrics(rows *sql.Rows) ([]domain.AgentMetrics, error) {
	models := map[string]*domain.AgentMetrics{}
	for rows.Next() {
		var agentModel, eventType string
		var projectID int64
		var count int
		if err := rows.Scan(&agentModel, &projectID, &eventType, &count); err != nil {
			return nil, err
		}
		registry := s.eventRegistry(projectID)
		def, ok := registry.Definition(eventType)
		if !ok || def.Metric == "" {
			continue
		}
		accumulateMetric(models, agentModel, registry, def, count)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.AgentMetrics, 0, len(models))
	for _, model := range models {
		if added := model.Buckets[domain.MetricBucketSolutionAdded]; added > 0 {
			model.LikeRate = float64(model.Buckets[domain.MetricBucketSolutionLiked]) / float64(added)
		}
		out = append(out, *model)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Buckets[domain.MetricBucketErrorRecorded], out[j].Buckets[domain.MetricBucketErrorRecorded]
		if a != b {
			return a > b
		}
		return out[i].AgentModel < out[j].AgentModel
	})
	return out, nil
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
	query, queryArgs := s.searchBeforeRecordQuery(periodClause, projectID)
	if query == "" {
		return nil
	}
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
        AND s.project_id IS r.project_id
        AND s.id < r.id
        AND s.created_at >= datetime(r.created_at, '-30 minutes')
    )`)

	query := `
SELECT
  r.agent_model,
  COUNT(*) AS sample,
  ` + searchedBeforeRecord + ` AS searched
FROM events r
WHERE ` + strings.Join(clauses, " AND ") + `
GROUP BY r.agent_model
`
	return query, append([]any{searchedKey}, args...)
}

// metricKey returns the event_type key for the YAML registry entry whose
// Metric tag matches the given bucket. The ok return is false when the
// registry has no matching entry — defensive against a kit YAML that
// drops a bucket Phase 2 hard-coded.
func metricKey(registry *domain.EventRegistry, bucket domain.EventMetricBucket) (string, bool) {
	for _, def := range registry.Definitions() {
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

func (s *Store) searchBeforeRecordQuery(period string, projectID int64) (string, []any) {
	scopes := s.registryScopes()
	if projectID > 0 {
		scopes = []eventRegistryScope{{registry: s.eventRegistry(projectID), projectID: projectID}}
	}
	var queries []string
	var args []any
	for _, scope := range scopes {
		recorded, ok := metricKey(scope.registry, domain.MetricBucketErrorRecorded)
		if !ok {
			continue
		}
		searched, ok := metricKey(scope.registry, domain.MetricBucketErrorsResearched)
		if !ok {
			continue
		}
		query, baseArgs := searchBeforeRecordQuery(recorded, searched, period, 0)
		condition, scopeArgs := scope.condition("r.")
		query = strings.Replace(query, "WHERE r.event_type", "WHERE ("+condition+") AND r.event_type", 1)
		queries = append(queries, query)
		args = append(args, baseArgs[0])
		args = append(args, scopeArgs...)
		args = append(args, baseArgs[1:]...)
	}
	if len(queries) == 0 {
		return "", nil
	}
	return "SELECT agent_model, SUM(sample), SUM(searched) FROM (" + strings.Join(queries, " UNION ALL ") + ") GROUP BY agent_model", args
}

func accumulateMetric(models map[string]*domain.AgentMetrics, agent string, registry *domain.EventRegistry, definition domain.EventDef, count int) {
	model := models[agent]
	if model == nil {
		model = &domain.AgentMetrics{AgentModel: agent, Buckets: map[domain.EventMetricBucket]int{}}
		models[agent] = model
	}
	for _, metadata := range registry.Definitions() {
		if metadata.Metric == "" {
			continue
		}
		bucket := domain.EventMetricBucket(metadata.Metric)
		if _, exists := model.Buckets[bucket]; !exists {
			model.Buckets[bucket] = 0
		}
	}
	model.Buckets[domain.EventMetricBucket(definition.Metric)] += count
}
