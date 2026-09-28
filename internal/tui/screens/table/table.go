// Package table owns the Tasks > Table screen, including its selectable-row
// cursor, screenlayout scroll window, rendering, and task outcomes. Filtering
// and sorting arrive in the shared taskprojection read model.
package table

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// Deps is the immutable task snapshot supplied by the host.
type Deps struct {
	Projection   taskprojection.Projection
	Tasks        []domain.Task
	Dependencies []domain.TaskDependency
	Comments     []domain.Comment
	View         config.TableViewSettings
	Priorities   []config.PriorityDefinition
}

// Screen implements the Tasks > Table screen contract.
type Screen struct {
	deps       Deps
	projection taskprojection.Projection
	rows       []domain.Task
	selected   int
	grid       screengrid.State
}

func New() Screen { return Screen{grid: screengrid.NewState()} }

// Scroll is the body offset, exposed for fixture assertions.
func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionRows) }

func (s Screen) ID() screenhost.ID { return screenhost.TasksTable }

// Bind replaces the host snapshot, rebuilds the local projection, and clamps
// cursor and scroll state to the new row count.
func (s Screen) Bind(deps Deps, frame screenhost.Frame) Screen {
	projection := deps.Projection
	if projection.Fingerprint() == 0 {
		projection = taskprojection.Build(taskprojection.Input{
			Tasks: deps.Tasks, Workflow: domain.Workflow{}, Dependencies: deps.Dependencies,
			Comments: deps.Comments, Priorities: deps.Priorities})
	}
	s.deps = deps
	s.projection = projection
	s.rows = projection.Tasks(taskprojection.Query{
		Priority: deps.View.Filter.Priority, Bucket: deps.View.Filter.Bucket,
		SortField: deps.View.Sort.Field, SortOrder: deps.View.Sort.Order})
	if s.selected >= len(s.rows) {
		s.selected = len(s.rows) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
	return s.syncRowsWindow(frame.Kit())
}

func (s Screen) Rows() []domain.Task { return s.rows }
func (s Screen) Selected() int       { return s.selected }

func (s Screen) SelectedTask() (domain.Task, bool) {
	if s.selected < 0 || s.selected >= len(s.rows) {
		return domain.Task{}, false
	}
	return s.rows[s.selected], true
}

func (s Screen) SelectedTaskID() (int64, bool) {
	task, ok := s.SelectedTask()
	return task.ID, ok
}

// SelectTaskID positions the cursor on taskID when it is visible, otherwise on
// the first row. It preserves the board-to-table focus handoff.
func (s Screen) SelectTaskID(taskID int64, frame screenhost.Frame) Screen {
	s.selected = 0
	for i, task := range s.rows {
		if task.ID == taskID {
			s.selected = i
			break
		}
	}
	return s.syncRowsWindow(frame.Kit())
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	if s.isMotionKey(key) {
		s.moveGrid(kit, key, false)
		return screenhost.Stay(s, nil)
	}
	if outcome, handled := s.handleTableAction(kit, key); handled {
		return outcome
	}
	s.moveGrid(kit, key, true)
	return screenhost.Stay(s, nil)
}

func (s Screen) isMotionKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "up", "k", "down", "j", "pgup", "ctrl+u", "pgdown", "ctrl+d", "home", "g", "end", "G":
		return true
	}
	return false
}

func (s *Screen) moveGrid(kit screenkit.Kit, key tea.KeyMsg, sync bool) {
	root := s.rowsRoot(kit)
	if next, handled := s.grid.HandleKey(kit, s.panelBox(kit), key.String(), root); handled {
		s.grid = next
		if c := s.grid.Layout().Cursor(sectionRows); c >= 0 {
			s.selected = c
		}
		if sync {
			*s = s.syncRowsWindow(kit)
		}
	}
}

func (s Screen) handleTableAction(kit screenkit.Kit, key tea.KeyMsg) (screenhost.Outcome, bool) {
	switch key.String() {
	case "enter":
		if task, found := s.SelectedTask(); found {
			return screenhost.OpenTask(s.syncRowsWindow(kit), task.ID, nil), true
		}
		return screenhost.Stay(s.syncRowsWindow(kit), nil), true
	case "m":
		if task, found := s.SelectedTask(); found {
			return screenhost.MoveTask(s.syncRowsWindow(kit), task.ID, nil), true
		}
		return screenhost.Stay(s.syncRowsWindow(kit), nil), true
	case "r":
		return screenhost.Reload(s, nil), true
	}
	return screenhost.Outcome{}, false
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.syncRowsWindow(frame.Kit())
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		frame.FooterOpen(true),
		frame.FooterNew(true),
		frame.FooterMoveTask(true),
		{Key: "up/down", Label: frame.Text("tui.footer.select")},
		frame.FooterScrollPage(false),
		frame.FooterTopBottom(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID: "tasks_table", Title: frame.Text("tui.help.tasks_table.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "↑ ↓ · j k", Description: frame.Text("tui.help.tasks_table.select_task")},
			{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.tasks_table.scroll_halfpage")},
			{Key: "g · G", Description: frame.Text("tui.help.tasks_table.first_last_task")},
			{Key: "enter", Description: frame.Text("tui.help.tasks_table.open_task")},
			{Key: "n", Description: frame.Text("tui.help.tasks_table.new_task")},
			{Key: "e", Description: frame.Text("tui.help.tasks_table.edit_task")},
			{Key: "m", Description: frame.Text("tui.help.tasks_table.move_bucket")},
			{Key: "A", Description: frame.Text("tui.help.tasks_table.toggle_archived")},
		},
	}}
}

var _ screenhost.Screen = Screen{}
