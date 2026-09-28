package tui

import (
	"omakiten/internal/tui/screens/table"
)

func (m Model) boundTableScreen() table.Screen {
	return m.tableScreen.Bind(table.Deps{
		Projection: m.taskProjection(m.workflow),
		Tasks:      m.tasks, Dependencies: m.dependencies, Comments: m.comments,
		View: m.views.Table, Priorities: m.priorities}, m.screenFrame())
}
