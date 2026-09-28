package tui

import (
	"context"

	"omakiten/internal/domain"
	"omakiten/internal/operation"
)

type metricsAdapter struct{ svc *operation.Service }

func (a metricsAdapter) Summary(ctx context.Context, project domain.ProjectContext, period string, projectID int64) (domain.MetricsSummary, error) {
	resp, err := a.svc.MetricsSummary(ctx, operation.MetricsSummaryInput{
		ProjectSelector: operation.ProjectSelector{ProjectID: project.ID},
		Period:          period,
		ProjectID:       projectID})
	if err != nil {
		return domain.MetricsSummary{}, err
	}
	return resp.Summary, nil
}

type insightsAdapter struct{ svc *operation.Service }

func (a insightsAdapter) Today(ctx context.Context, project domain.ProjectContext, projectID int64, stuckDays int, stuckBuckets []int64) (domain.Insights, error) {
	return a.svc.InsightsToday(ctx, project, projectID, stuckDays, stuckBuckets)
}

type searchAdapter struct{ svc *operation.Service }

func (a searchAdapter) Search(ctx context.Context, project domain.ProjectContext, query string, entityTypes []string) ([]domain.SearchHit, error) {
	resp, err := a.svc.Search(ctx, operation.SearchInput{
		ProjectSelector: operation.ProjectSelector{ProjectID: project.ID},
		Query:           query,
		EntityTypes:     entityTypes})
	if err != nil {
		return nil, err
	}
	hits := make([]domain.SearchHit, 0, len(resp.Hits))
	for _, h := range resp.Hits {
		hits = append(hits, domain.SearchHit{
			EntityType: domain.SearchEntityType(h.EntityType),
			ID:         h.ID,
			Score:      h.Score,
			Snippet:    h.Snippet,
			ProjectID:  h.ProjectID})
	}
	return hits, nil
}

func (m Model) metricsPort() MetricsPort {
	if m.repos.Metrics != nil {
		return m.repos.Metrics
	}
	if svc := m.repos.operationService(); svc != nil {
		return metricsAdapter{svc: svc}
	}
	return nil
}

func (m Model) insightsPort() InsightsPort {
	if m.repos.Insights != nil {
		return m.repos.Insights
	}
	if svc := m.repos.operationService(); svc != nil {
		return insightsAdapter{svc: svc}
	}
	return nil
}

func (m Model) searchPort() SearchPort {
	if m.repos.Search != nil {
		return m.repos.Search
	}
	if svc := m.repos.operationService(); svc != nil {
		return searchAdapter{svc: svc}
	}
	return nil
}
