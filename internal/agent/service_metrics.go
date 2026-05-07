package agent

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func (s *Service) MetricsSummary(ctx context.Context, input MetricsSummaryInput) (MetricsSummaryResponse, error) {
	// Resolve a project context so activity tracking can attribute the call,
	// but tolerate the cross-project / orphaned cases — metrics.summary
	// supports both an explicit project filter and a global view.
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		project = domain.ProjectContext{}
	}
	summary, err := app.NewMetricsService(s.repo).Summary(ctx, project, input.Period, input.ProjectID)
	if err != nil {
		return MetricsSummaryResponse{}, err
	}
	return MetricsSummaryResponse{Project: projectSummary(project), Summary: summary}, nil
}
