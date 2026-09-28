// Package plans owns the Tasks > Plans list and read-only plan-goal reader.
package plans

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

type Screen struct {
	plans   []domain.PlanRollup
	grid    screengrid.State
	rows    *plansRowsCache
	loading bool
	err     error
}

type plansRowsCache struct {
	width int
	rows  []string
	ready bool
}

// New seeds the body's selection on the first plan through
// [screengrid.State.WithCursor], which is where the deleted `list.NewWindow(0)`
// used to put it. The seed matters before the first resync: the host can reach
// this screen and press `enter` on the same frame it loads the rollups, and an
// unresolved cursor would make that press select nothing. `SelectFirst` on
// sectionBody says the same thing to the arranger; this says it to the screen.
func New() Screen {
	return Screen{grid: screengrid.NewState().WithCursor(sectionBody, 0), rows: &plansRowsCache{}}
}

func (s Screen) ID() screenhost.ID { return screenhost.TasksPlans }

func (s Screen) Apply(rollups []domain.PlanRollup, err error) Screen {
	s.plans = append([]domain.PlanRollup(nil), rollups...)
	s.loading, s.err = false, err
	s.rows = &plansRowsCache{}
	return s
}

func (s Screen) Loading() Screen              { s.loading, s.err = true, nil; return s }
func (s Screen) Cursor() int                  { return s.grid.Layout().Cursor(sectionBody) }
func (s Screen) Scroll() int                  { return s.grid.Layout().Offset(sectionBody) }
func (s Screen) Rollups() []domain.PlanRollup { return append([]domain.PlanRollup(nil), s.plans...) }

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	// Resolve the plan list's own actions first; every motion spelling is the
	// grid's, and on a single-Cell root it declines everything else.
	switch key.String() {
	case "enter":
		if plan, found := s.selected(); found {
			return screenhost.OpenPlanNetwork(s.syncBodyWindow(kit), plan.Plan.Slug, nil)
		}
	case "f":
		if plan, found := s.selected(); found {
			return screenhost.OpenPlanGoal(s.syncBodyWindow(kit), plan.Plan.Slug, nil)
		}
	case "r":
		return screenhost.Reload(s.Loading(), nil)
	}
	s.grid, _ = s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.root(kit))
	return screenhost.Stay(s, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		s = s.syncBodyWindow(frame.Kit())
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) selected() (domain.PlanRollup, bool) {
	cursor := s.Cursor()
	if len(s.plans) == 0 || cursor < 0 || cursor >= len(s.plans) {
		return domain.PlanRollup{}, false
	}
	return s.plans[cursor], true
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if s.loading || s.err != nil || len(s.plans) == 0 {
		if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
			return kit.Panel(body)
		}
	}
	return "\n" + screenkit.Indent(s.gridView(kit), 2)
}

func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(kit.T("tui.plans.kicker"))
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.plans")),
		id.Vazio(len(s.plans) == 0, kit.T("tui.plans.list.empty"), ""),
	}
}

// OwnsKey claims the list's domain action before any nested layout/grid gets a
// chance to interpret the same spelling.
func (s Screen) OwnsKey(key tea.KeyMsg) bool { return key.String() == "f" }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{frame.FooterOpen(true), {Key: "f", Label: frame.Text("tui.footer.view_goal"), Primary: true}, frame.FooterMoveVim(false), frame.FooterTopBottom(false), frame.FooterPage(false), frame.FooterRefresh(false)}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "tasks_plans", Title: frame.Text("tui.plans.kicker"), Bindings: []screenhost.HelpBinding{{Key: "enter", Description: frame.Text("tui.footer.open")}, {Key: "f", Description: frame.Text("tui.footer.view_goal")}, {Key: "j k", Description: frame.Text("tui.footer.move")}, {Key: "r", Description: frame.Text("tui.footer.refresh")}}}}
}

func truncate(value string, width int) string {
	if width <= 0 || len([]rune(value)) <= width {
		return value
	}
	runes := []rune(value)
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}

func percent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 100 / total
}

type GoalScreen struct {
	show             domain.PlanShow
	grid             screengrid.State
	md               *markdown.Renderer
	markdownRendered bool
	loading          bool
	err              error
}

func NewGoal() GoalScreen {
	return GoalScreen{grid: screengrid.NewState(), md: markdown.New(markdown.Tokens{}), markdownRendered: true}
}
func (s GoalScreen) ID() screenhost.ID { return screenhost.PlanGoal }
func (s GoalScreen) Apply(show domain.PlanShow, err error) GoalScreen {
	s.show, s.err, s.loading = show, err, false
	s.grid = screengrid.NewState()
	return s
}
func (s GoalScreen) Loading() GoalScreen     { s.loading, s.err = true, nil; return s }
func (s GoalScreen) Scroll() int             { return s.grid.Layout().Offset(sectionGoal) }
func (s GoalScreen) MarkdownRendered() bool  { return s.markdownRendered }
func (s GoalScreen) OwnsKey(tea.KeyMsg) bool { return true }
func (s GoalScreen) BlocksHostInput() bool   { return true }

func (s GoalScreen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	switch key.String() {
	case "esc", "f":
		return screenhost.Back(s, nil)
	case "M":
		s.markdownRendered = !s.markdownRendered
		status := frame.Text("tui.status.markdown_raw")
		if s.markdownRendered {
			status = frame.Text("tui.status.markdown_rendered")
		}
		return screenhost.SetStatus(s, status, nil)
	}
	// Every motion spelling is the grid's; on a single-Cell root it declines
	// everything the switch above already claimed.
	kit := frame.Kit()
	s.grid, _ = s.grid.HandleKey(kit, screenlayout.HostBox(kit), key.String(), s.root(frame))
	return screenhost.Stay(s, nil)
}

func (s GoalScreen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.syncBodyWindow(frame)
	}
	return screenhost.Stay(s, nil)
}

func (s GoalScreen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	if s.loading || s.err != nil {
		if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...); ok {
			return kit.Panel(body)
		}
	}
	view := s.gridView(frame)
	return screenkit.Indent("\n"+view, 2)
}

func (s GoalScreen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(kit.T("tui.kicker.goal"))
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.plan_goal")),
	}
}

func (s GoalScreen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{frame.FooterCloseFocus(true), frame.FooterScroll(false), frame.FooterPage(false), frame.FooterTopBottom(false), frame.FooterToggleMarkdown(false)}
}
func (s GoalScreen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "plan_goal", Title: frame.Text("tui.kicker.goal"), Bindings: []screenhost.HelpBinding{{Key: "f · esc", Description: frame.Text("tui.footer.close_focus")}, {Key: "j k", Description: frame.Text("tui.footer.scroll")}, {Key: "M", Description: frame.Text("tui.footer.toggle_markdown")}}}}
}

var _ screenhost.KeyOwner = Screen{}

var _ screenhost.Screen = Screen{}
var _ screenhost.Screen = GoalScreen{}
var _ screenhost.KeyOwner = GoalScreen{}
var _ screenhost.InteractionBlocker = GoalScreen{}
