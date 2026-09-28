package tui

import (
	depgraph "omakiten/internal/graph"
	"omakiten/internal/tui/screens/graph"
)

// boundGraphScreen binds the Tasks > Graph screen to the current data. Building
// the DAG projection is the HOST's call, not the screen's: the view settings
// that order its roots are the host's state, and internal/graph is where the
// walk lives.
func (m Model) boundGraphScreen() graph.Screen {
	view := m.views.Graph
	return m.graphScreen.Bind(graph.Deps{
		Tasks:        m.tasks,
		Dependencies: m.dependencies,
		Lines: depgraph.Lines(m.dependencies, m.tasks, depgraph.RootOrder{
			Field: view.Sort.Field, Order: view.Sort.Order,
		}),
	}, m.screenFrame())
}
