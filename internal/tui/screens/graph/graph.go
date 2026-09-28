// Package graph owns the Tasks > Graph screen: the DAG projection, the screengrid
// body it is mounted on, rendering, and task outcomes. The cursor is the
// arranger's — the projection declares which of its lines are nodes through
// screenlayout's selectable mask and keeps no cursor of its own.
package graph

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	depgraph "omakiten/internal/graph"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/screenhost"
)

// Deps is what the host hands the screen for a paint. Lines is the projection
// ALREADY BUILT: the screen paints a DAG listing, it does not walk a dependency
// graph to obtain one. Dependencies stays for the count the kicker prints and
// the empty branch it selects; Tasks resolves the selected node back to a task.
type Deps struct {
	Tasks        []domain.Task
	Dependencies []domain.TaskDependency
	Lines        []depgraph.Line
}

// Screen implements the Tasks > Graph screen contract.
type Screen struct {
	deps  Deps
	lines []depgraph.Line
	grid  screengrid.State
}

func New() Screen {
	return Screen{grid: screengrid.NewState()}
}

// Scroll is the body offset, exposed for fixture assertions.
func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionBody) }

func (s Screen) ID() screenhost.ID { return screenhost.TasksGraph }

func (s Screen) Bind(deps Deps, frame screenhost.Frame) Screen {
	s.deps = deps
	s.lines = deps.Lines
	return s.resyncBody(frame.Kit())
}

// Cursor is the projected LINE the selection sits on — the arranger's item
// index for the body section, and the only cursor this screen has. The
// selectable mask keeps it on a line that carries a node.
func (s Screen) Cursor() int { return s.grid.Layout().Cursor(sectionBody) }

func (s Screen) SelectedTask() (domain.Task, bool) {
	cursor := s.Cursor()
	if cursor < 0 || cursor >= len(s.lines) || s.lines[cursor].TaskID == 0 {
		return domain.Task{}, false
	}
	taskID := s.lines[cursor].TaskID
	for _, task := range s.deps.Tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.Task{}, false
}

func (s Screen) SelectedTaskID() (int64, bool) {
	task, ok := s.SelectedTask()
	return task.ID, ok
}

// SelectTaskID parks the selection on the first projected line carrying the
// task. The write goes through screengrid.State.WithCursor rather than
// WithLayout(Layout().WithCursor(...)): the latter discards the frame memo the
// last composition recorded, and a cursor is not one of the things that memo
// witnesses.
func (s Screen) SelectTaskID(taskID int64, frame screenhost.Frame) Screen {
	for i, line := range s.lines {
		if line.TaskID == taskID {
			s.grid = s.grid.WithCursor(sectionBody, i)
			break
		}
	}
	return s.resyncBody(frame.Kit())
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	switch key.String() {
	case "enter":
		if task, found := s.SelectedTask(); found {
			return screenhost.OpenTask(s, task.ID, nil)
		}
		return screenhost.Stay(s, nil)
	case "r":
		return screenhost.Reload(s, nil)
	}
	// Motion is the grid's. Every scroll key this screen advertises comes from
	// screenlayout.StandardBindings, and the mask on Block.Selectable is what
	// makes j/k/g/G/page step node to node instead of line to line — so there is
	// no key table here to fall out of step with the footer.
	if next, handled := s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.bodyRoot(kit)); handled {
		s.grid = next
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.resyncBody(frame.Kit())
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		frame.FooterOpen(true),
		frame.FooterMoveVim(false),
		frame.FooterScrollPage(false),
		frame.FooterTopBottom(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID: "tasks_graph", Title: frame.Text("tui.help.tasks_graph.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "← →", Description: frame.Text("tui.help.tasks_graph.switch_view")},
			{Key: "↑ ↓ · j k", Description: frame.Text("tui.help.tasks_graph.move_cursor")},
			{Key: "enter", Description: frame.Text("tui.help.tasks_graph.open_task")},
			{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.tasks_graph.scroll_halfpage")},
			{Key: "g · G", Description: frame.Text("tui.help.tasks_graph.jump_top_bottom")},
		},
	}}
}

// selectableIndices is the projection's selectable mask: the ascending indices
// of the lines that carry a node, which is every line except the blank spacer
// between two roots. It is DECLARED to the arranger on Block.Selectable rather
// than kept beside a second cursor — the mapping both ways that used to live
// here is what the mask replaces.
func selectableIndices(lines []depgraph.Line) []int {
	indices := make([]int, 0, len(lines))
	for i, line := range lines {
		if line.TaskID != 0 {
			indices = append(indices, i)
		}
	}
	return indices
}

var _ screenhost.Screen = Screen{}
