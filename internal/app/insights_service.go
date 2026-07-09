package app

import (
	"context"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// DefaultStuckDays re-exports domain.DefaultStuckDays as the service-layer
// name callers already use. The canonical value lives in domain so this
// layer and the sqlite repository reference one constant and can never
// disagree; the repository still applies it defensively when handed
// stuckDays <= 0.
const DefaultStuckDays = domain.DefaultStuckDays

// InsightsService computes the six today-insights on demand (no cache) for a
// project: stuck tasks, cycle-time/bottleneck per bucket, WIP per bucket,
// guard hotspots, the error loop, and a basic per-model contrast. It is a
// thin orchestration layer over InsightsRepository — input validation,
// activity tracking, and the staleness-threshold default — mirroring
// MetricsService so the two read-side services share a shape.
//
// This service is read-only: every insight is a query, nothing here mutates
// state. The TUI surface (task 1351) and the MCP tool (task 1352) consume
// this service; neither is wired here.
type InsightsService struct {
	repo InsightsRepository
}

func NewInsightsService(repo InsightsRepository) *InsightsService {
	return &InsightsService{repo: repo}
}

// Today returns all six insights for the given project. projectID > 0 scopes
// the task-shaped insights (stuck, cycle time, WIP, guards, per-model) to
// one project; 0 returns the global view. stuckDays parameterises insight 1;
// pass 0 to take the default (domain.DefaultStuckDays). stuckBuckets names
// the in-flight bucket ids the stuck scan targets — callers resolve it from
// their active workflow via Workflow.InFlightBucketIDs; empty falls back to
// the repository's canonical dev/review ids.
//
// The returned domain.Insights carries an explicit HasData flag per
// sub-insight so a renderer can distinguish "no history" from a genuine zero
// — never a silent zero.
func (s *InsightsService) Today(ctx context.Context, project domain.ProjectContext, projectID int64, stuckDays int, stuckBuckets []int64) (insights domain.Insights, err error) {
	finish := activity.Track(ctx, "app.InsightsService.Today", project, map[string]any{"project_id": projectID, "stuck_days": stuckDays})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if stuckDays <= 0 {
		stuckDays = DefaultStuckDays
	}

	insights, err = s.repo.Insights(ctx, projectID, stuckDays, stuckBuckets)
	return
}
