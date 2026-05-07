package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type MetricsService struct {
	repo MetricsRepository
}

func NewMetricsService(repo MetricsRepository) *MetricsService {
	return &MetricsService{repo: repo}
}

// Summary aggregates per-AI-model behaviour over the chosen period. Period
// must be "7d", "30d" (default), or "all" — anything else falls back to
// "30d" so a typo never hides data unintentionally. projectID > 0 narrows
// the view to one project; 0 returns the global benchmark.
func (s *MetricsService) Summary(ctx context.Context, project domain.ProjectContext, period string, projectID int64) (summary domain.MetricsSummary, err error) {
	finish := activity.Track(ctx, "app.MetricsService.Summary", project, map[string]any{"period": period, "project_id": projectID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" {
		period = "30d"
	}
	if period != "7d" && period != "30d" && period != "all" {
		period = "30d"
	}

	rows, since, err := s.repo.AgentMetricsSummary(ctx, period, projectID)
	if err != nil {
		return
	}
	summary = domain.MetricsSummary{
		Period:  period,
		Since:   since,
		ByModel: rows,
	}
	var totalSearchBefore int
	for _, m := range rows {
		summary.Total.ErrorsRecorded += m.ErrorsRecorded
		summary.Total.ErrorsSearched += m.ErrorsSearched
		summary.Total.SolutionsAdded += m.SolutionsAdded
		summary.Total.SolutionsLiked += m.SolutionsLiked
		summary.Total.SolutionsFailed += m.SolutionsFailed
		summary.Total.SolutionsTopViewed += m.SolutionsTopViewed
		summary.Total.SessionCorrelatedSample += m.SessionCorrelatedSample
		// Reconstruct the per-model search-before-record count from the ratio
		// so the totals row can recompute the ratio over the combined sample
		// instead of averaging ratios (which would be wrong when samples
		// differ in size).
		totalSearchBefore += int(m.SearchBeforeRecordRatio*float64(m.SessionCorrelatedSample) + 0.5)
	}
	if summary.Total.SolutionsAdded > 0 {
		summary.Total.LikeRate = float64(summary.Total.SolutionsLiked) / float64(summary.Total.SolutionsAdded)
	}
	if summary.Total.SessionCorrelatedSample > 0 {
		summary.Total.SearchBeforeRecordRatio = float64(totalSearchBefore) / float64(summary.Total.SessionCorrelatedSample)
	}
	return
}
