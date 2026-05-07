package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"omakiten/internal/domain"
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
func (s *Store) AgentMetricsSummary(ctx context.Context, period string, projectID int64) ([]domain.AgentMetrics, string, error) {
	periodClause, since := periodFilter(period)

	args := []any{}
	whereClauses := []string{
		"agent_model != ''",
		"event_type IN ('error.recorded','error.searched','solution.added','solution.liked','solution.failed','solution.viewed_top')",
	}
	if periodClause != "" {
		whereClauses = append(whereClauses, periodClause)
	}
	if projectID > 0 {
		whereClauses = append(whereClauses, "project_id = ?")
		args = append(args, projectID)
	}

	where := whereClauses[0]
	for _, c := range whereClauses[1:] {
		where += " AND " + c
	}

	countQuery := `
SELECT
  agent_model,
  SUM(CASE WHEN event_type = 'error.recorded' THEN 1 ELSE 0 END),
  SUM(CASE WHEN event_type = 'error.searched' THEN 1 ELSE 0 END),
  SUM(CASE WHEN event_type = 'solution.added' THEN 1 ELSE 0 END),
  SUM(CASE WHEN event_type = 'solution.liked' THEN 1 ELSE 0 END),
  SUM(CASE WHEN event_type = 'solution.failed' THEN 1 ELSE 0 END),
  SUM(CASE WHEN event_type = 'solution.viewed_top' THEN 1 ELSE 0 END)
FROM events
WHERE ` + where + `
GROUP BY agent_model
ORDER BY 2 DESC, 1
`

	rows, err := s.db.QueryContext(ctx, countQuery, args...)
	if err != nil {
		return nil, since, fmt.Errorf("metrics summary counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byModel := []domain.AgentMetrics{}
	for rows.Next() {
		var m domain.AgentMetrics
		if err := rows.Scan(&m.AgentModel, &m.ErrorsRecorded, &m.ErrorsSearched, &m.SolutionsAdded, &m.SolutionsLiked, &m.SolutionsFailed, &m.SolutionsTopViewed); err != nil {
			return nil, since, err
		}
		if m.SolutionsAdded > 0 {
			m.LikeRate = float64(m.SolutionsLiked) / float64(m.SolutionsAdded)
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
func (s *Store) fillSearchBeforeRecord(ctx context.Context, models []domain.AgentMetrics, periodClause string, projectID int64) error {
	if len(models) == 0 {
		return nil
	}
	args := []any{}
	whereClauses := []string{
		"r.event_type = 'error.recorded'",
		"r.agent_model != ''",
		"r.agent_session_id IS NOT NULL AND r.agent_session_id != ''",
	}
	if periodClause != "" {
		whereClauses = append(whereClauses, "r."+periodClause)
	}
	if projectID > 0 {
		whereClauses = append(whereClauses, "r.project_id = ?")
		args = append(args, projectID)
	}

	where := whereClauses[0]
	for _, c := range whereClauses[1:] {
		where += " AND " + c
	}

	query := `
SELECT
  r.agent_model,
  COUNT(*),
  SUM(
    CASE WHEN EXISTS (
      SELECT 1 FROM events s
      WHERE s.event_type = 'error.searched'
        AND s.agent_session_id = r.agent_session_id
        AND s.id < r.id
        AND s.created_at >= datetime(r.created_at, '-30 minutes')
    ) THEN 1 ELSE 0 END
  )
FROM events r
WHERE ` + where + `
GROUP BY r.agent_model
`
	rows, err := s.db.QueryContext(ctx, query, args...)
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
