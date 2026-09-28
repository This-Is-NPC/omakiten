package tui

import (
	"context"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screens/logs"
)

// boundLogsScreen binds live host deps onto the Stats › Logs screen, resolving
// the display window and the storage retention from the active bundle snapshot.
func (m Model) boundLogsScreen() logs.Screen {
	views := m.activeViewSettings()
	settings := logs.ViewSettings{
		Retention: logs.Retention{WindowDays: views.Logs.WindowDays}}
	if snap := m.repos.activeSnapshot(); snap != nil {
		resolved := snap.Events().ResolveRetention("cli.tool_call")
		settings.Retention = logs.Retention{
			Known:      true,
			MaxAgeDays: resolved.MaxAgeDays,
			MaxRows:    resolved.MaxRows,
			WindowDays: int(snap.LogsWindowDays().Hours() / 24)}
	}
	deps := logs.Deps{Available: m.repos.Events != nil, Settings: settings}
	return m.logsScreen.Bind(deps)
}

// loadLogsPayload is host orchestration for the Logs screen. It resolves the
// configured window, runs both event queries, applies registry visibility, and
// prepares the aggregate before the screen receives anything.
func (m Model) loadLogsPayload(filter domain.LogsFilterMode) (logs.Payload, error) {
	return prepareLogsPayload(m.ctx, m.project, m.repos.Events, m.activeViewSettings(), m.repos.activeSnapshot(), filter)
}

func prepareLogsPayload(ctx context.Context, project domain.ProjectContext, events EventStore, views config.ViewSettings, snapshot *config.Snapshot, filter domain.LogsFilterMode) (logs.Payload, error) {
	since := time.Time{}
	if snapshot != nil {
		if window := snapshot.LogsWindowDays(); window > 0 {
			since = time.Now().Add(-window)
		}
	} else if views.Logs.WindowDays > 0 {
		since = time.Now().Add(-time.Duration(views.Logs.WindowDays) * 24 * time.Hour)
	}
	query := domain.EventFilter{
		ProjectID:  project.ID,
		Categories: domain.LogsFilterCategories(filter),
		Since:      since,
		Limit:      views.Logs.Limit,
		Order:      views.Logs.Sort.Order,
	}
	rows, err := events.ListEvents(ctx, query)
	if err != nil {
		return logs.Payload{}, err
	}
	counts, err := events.EventCategoryCounts(ctx, project.ID, since)
	if err != nil {
		return logs.Payload{}, err
	}
	if filter == domain.LogsFilterAll {
		rows = domain.FilterLogVisibleRows(rows)
	}
	return logs.Payload{Rows: rows, Stats: domain.ComputeEventStats(rows, counts)}, nil
}
