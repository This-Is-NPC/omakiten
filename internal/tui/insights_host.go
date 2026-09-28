package tui

import (
	"omakiten/internal/domain"
	"omakiten/internal/tui/screens/insights"
)

// boundInsightsScreen binds live host deps onto the Stats › Insights screen.
func (m Model) boundInsightsScreen() insights.Screen {
	deps := insights.Deps{
		Available: m.insightsPort() != nil}
	return m.insightsScreen.Bind(deps)
}

func (m Model) insightsBucketNames() []domain.Bucket {
	return append([]domain.Bucket(nil), m.workflow.Buckets...)
}

func insightsPayload(reading domain.Insights, names []domain.Bucket) insights.Payload {
	return insights.Payload{Insights: reading, BucketNames: names}
}
