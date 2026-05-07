package agent

import "omakiten/internal/domain"

// MetricsSummaryInput drives the metrics.summary MCP endpoint. Period
// defaults to "30d" when empty; ProjectID 0 returns the cross-project view.
type MetricsSummaryInput struct {
	ProjectSelector
	Period    string `json:"period,omitempty"`
	ProjectID int64  `json:"project_id,omitempty"`
}

type MetricsSummaryResponse struct {
	Project ProjectSummary        `json:"project,omitempty"`
	Summary domain.MetricsSummary `json:"summary"`
}
