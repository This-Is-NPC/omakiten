package tui

import (
	"omakiten/internal/tui/screens/stats"
)

// boundStatsScreen binds live host deps onto the Stats › General screen.
//
// The metrics port is assigned only when the concrete service is non-nil: a nil
// *app.MetricsService stored in an interface field is a NON-nil interface, and
// the screen decides "unavailable" from that field alone.
func (m Model) boundStatsScreen() stats.Screen {
	deps := stats.Deps{
		Available: m.metricsPort() != nil,
		Totals: stats.Totals{
			Tasks:    len(m.tasks),
			Comments: len(m.comments),
			Tags:     len(m.tags),
			Tokens:   m.metrics}}
	return m.statsScreen.Bind(deps)
}
