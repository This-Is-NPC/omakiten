package agent

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

// MetricsSummary returns the per-window task / event metrics for a
// project, or the global summary across every project when the caller
// supplies no selector and no resolver match.
//
// The resolveProject error is intentionally swallowed to a zero
// ProjectContext: metrics.summary is one of two surfaces (the other is
// project.overview) that legitimately runs without a project — agents
// invoking `okt metrics summary --period 7d` from outside a registered
// project root want the global rollup, not a project_not_found error.
// The fallthrough therefore IS the "global summary" code path; the
// downstream app.MetricsService.Summary sees ProjectContext{} (ID=0)
// and skips the project filter on every query.
func (s *Service) MetricsSummary(ctx context.Context, input MetricsSummaryInput) (MetricsSummaryResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		// Global-summary fallthrough — see godoc above.
		project = domain.ProjectContext{}
	}
	summary, err := app.NewMetricsService(s.repo).Summary(ctx, project, input.Period, input.ProjectID)
	if err != nil {
		return MetricsSummaryResponse{}, err
	}
	return MetricsSummaryResponse{Project: projectSummary(project), Summary: summary}, nil
}
