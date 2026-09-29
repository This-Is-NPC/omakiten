// Package home owns the cross-project Home screen presentation and interaction state.
package home

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
)

const (
	columnInnerMin = 40
	columnInnerMax = 84
)

type Payload struct {
	Projects []domain.Project
	Tags     map[int64][]domain.Tag
	Pending  map[int64]int
}

type Result struct {
	Generation uint64
	Payload    Payload
	Err        error
}

type Screen struct {
	projects       []domain.Project
	tags           map[int64][]domain.Tag
	pending        map[int64]int
	picker         list.Picker
	grid           screengrid.State
	armedProjectID int64
	armedCounters  domain.ProjectDeleteCounters
	requestGen     uint64
	loading        bool
	err            error
}

func New() Screen {
	return Screen{picker: list.NewPicker(list.Single), grid: screengrid.NewState()}
}

func (s Screen) ID() screenhost.ID { return screenhost.Home }

func (s Screen) Apply(result Result) Screen {
	if result.Generation != s.requestGen {
		return s
	}
	if result.Err != nil {
		s.loading, s.err = false, result.Err
		return s
	}
	s.projects = append([]domain.Project(nil), result.Payload.Projects...)
	s.tags = cloneTags(result.Payload.Tags)
	s.pending = clonePending(result.Payload.Pending)
	s.loading, s.err = false, result.Err
	s.picker = s.picker.WithCursor(s.picker.Cursor, len(s.projects), 1)
	if !s.hasProject(s.armedProjectID) {
		s = s.CancelDelete()
	}
	return s
}

func (s Screen) ConfirmDelete() screenhost.Outcome {
	project, ok := s.selected()
	if !ok || s.armedProjectID != project.ID {
		return screenhost.Stay(s.CancelDelete(), nil)
	}
	counters := s.armedCounters
	s = s.CancelDelete().Loading(s.requestGen + 1)
	out := screenhost.DeleteProject(s, project.ID, counters, nil)
	out.Action.Generation = s.requestGen
	return out
}

func (s Screen) Loading(generation uint64) Screen {
	s.requestGen, s.loading, s.err = generation, true, nil
	return s
}

func (s Screen) ArmDelete(projectID int64, counters domain.ProjectDeleteCounters) Screen {
	if s.hasProject(projectID) {
		s.armedProjectID, s.armedCounters = projectID, counters
	}
	return s
}

func (s Screen) CancelDelete() Screen {
	s.armedProjectID, s.armedCounters = 0, domain.ProjectDeleteCounters{}
	return s
}

func (s Screen) Projects() []domain.Project { return append([]domain.Project(nil), s.projects...) }
func (s Screen) IsLoading() bool            { return s.loading }
func (s Screen) Cursor() int                { return s.picker.Cursor }
func (s Screen) Scroll() int                { return s.grid.Layout().Offset(sectionCards) }
func (s Screen) ArmedProjectID() int64      { return s.armedProjectID }
func (s Screen) Generation() uint64         { return s.requestGen }

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	if key.String() == "ctrl+h" {
		s = s.Loading(s.requestGen + 1)
		out := screenhost.Reload(s, nil)
		out.Action.Generation = s.requestGen
		return out
	}
	if key.String() == "n" {
		return screenhost.CreateProject(s, nil)
	}
	project, selected := s.selected()
	if key.String() == "d" && selected {
		if s.armedProjectID == project.ID {
			counters := s.armedCounters
			s = s.CancelDelete().Loading(s.requestGen + 1)
			out := screenhost.DeleteProject(s, project.ID, counters, nil)
			out.Action.Generation = s.requestGen
			return out
		}
		return screenhost.PrepareProjectDelete(s.CancelDelete(), project.ID, nil)
	}
	if key.String() == "e" && selected {
		return screenhost.EditProject(s.CancelDelete(), project.ID, nil)
	}
	if s.armedProjectID != 0 {
		s = s.CancelDelete()
	}
	if next, handled := s.handleNavKey(frame, msg, key); handled {
		return next
	}
	var cmd tea.Cmd
	s.picker, cmd = s.picker.Update(msg, len(s.projects), 0)
	s = s.syncCardsWindow(frame)
	if s.picker.LastEvent() == list.PickerSelect {
		if project, ok := s.selected(); ok {
			return screenhost.SelectProject(s, project.ID, cmd)
		}
	}
	return screenhost.Stay(s, cmd)
}

// handleNavKey routes scroll/cursor motion through the mounted grid cell first.
// When the declared cursor snaps the highlight still, the picker drives nav and
// the grid resync follows (same dual-ownership as settingspicker).
func (s Screen) handleNavKey(frame screenhost.Frame, msg tea.Msg, key tea.KeyMsg) (screenhost.Outcome, bool) {
	kit := frame.Kit()
	pre := s.picker.Cursor
	next, handled := s.grid.HandleKey(kit, s.columnBox(frame), key.String(), s.root(frame))
	if !handled {
		return screenhost.Outcome{}, false
	}
	s.grid = next
	s = s.syncPickerFromGrid()
	if s.picker.Cursor != pre {
		return screenhost.Stay(s, nil), true
	}
	var cmd tea.Cmd
	s.picker, cmd = s.picker.Update(msg, len(s.projects), 0)
	return screenhost.Stay(s.syncCardsWindow(frame), cmd), true
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		return screenhost.Stay(s.syncCardsWindow(frame), nil)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	// Liveness guard — see description.View (#2467 budget note).
	// err is included because cardsBlock skips the body on the same condition
	// (#126545): omitting it blanks the screen once Failed leaves the hand-painted
	if s.err != nil || s.loading || len(s.projects) == 0 {
		if body, ok := screenstate.Resolve(kit, s.columnBox(frame).Width, s.states(frame)...); ok {
			return s.wrapColumn(frame, body)
		}
	}
	arranged := s.gridResult(frame).View
	return s.wrapColumn(frame, arranged)
}

// states are the candidates screenstate ranks. Order is not precedence;
// screenstate ranks.
func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(fmt.Sprintf(kit.T("tui.home.projects_kicker_fmt"), len(s.projects)))
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.projects")),
		id.Vazio(len(s.projects) == 0, frame.Text("tui.empty.home_no_projects"), ""),
	}
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if len(s.projects) == 0 {
		return []screenhost.FooterBinding{frame.FooterNew(true), {Key: "q", Label: frame.Text("tui.footer.quit")}, frame.FooterHelp(false)}
	}
	bindings := []screenhost.FooterBinding{frame.FooterOpen(true), frame.FooterNew(false), frame.FooterEdit(false), frame.FooterMove(false)}
	if s.armedProjectID != 0 {
		bindings = append(bindings, screenhost.FooterBinding{Key: "d", Label: frame.Text("tui.footer.confirm_delete_project"), Primary: true})
	} else {
		bindings = append(bindings, screenhost.FooterBinding{Key: "d", Label: frame.Text("tui.footer.delete_project")})
	}
	return append(bindings, screenhost.FooterBinding{Key: "ctrl+h", Label: frame.Text("tui.footer.refresh")}, screenhost.FooterBinding{Key: "q", Label: frame.Text("tui.footer.quit")}, frame.FooterHelp(false))
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "home", Title: frame.Text("tui.help.home.title"), Bindings: []screenhost.HelpBinding{
		{Key: "↑ ↓ · j k", Description: frame.Text("tui.help.home.move_project")},
		{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.home.scroll_halfpage")},
		{Key: "g · G", Description: frame.Text("tui.help.home.first_last_project")},
		{Key: "enter", Description: frame.Text("tui.help.home.open_project")},
		{Key: "ctrl+h", Description: frame.Text("tui.help.home.reload")},
		{Key: "q · ctrl+c", Description: frame.Text("tui.help.home.quit")},
	}}}
}

func (s Screen) OwnsKey(tea.KeyMsg) bool { return true }
func (s Screen) OwnsFooter() bool        { return true }

func (s Screen) selected() (domain.Project, bool) {
	if len(s.projects) == 0 || s.picker.Cursor < 0 || s.picker.Cursor >= len(s.projects) {
		return domain.Project{}, false
	}
	return s.projects[s.picker.Cursor], true
}

func (s Screen) hasProject(id int64) bool {
	if id == 0 {
		return false
	}
	for _, project := range s.projects {
		if project.ID == id {
			return true
		}
	}
	return false
}

// header is the chrome the project column draws above its scrolled cards: the
// PROJECTS kicker and the rule under it. View joins it into the body and
// viewportRows measures it, so the two can never disagree.
func (s Screen) kicker(kit screenkit.Kit) string {
	text := fmt.Sprintf(kit.T("tui.home.projects_kicker_fmt"), len(s.projects))
	text = strings.Replace(text, "// ", "▸ ", 1)
	return kit.Styles.HintAccent.Render(text)
}

func (s Screen) columnInner(available int) int {
	inner := available - 2
	if inner > columnInnerMax {
		inner = columnInnerMax
	}
	if inner < columnInnerMin {
		inner = columnInnerMin
	}
	if maxInner := available - 2; inner > maxInner {
		if maxInner < 1 {
			maxInner = 1
		}
		inner = maxInner
	}
	return inner
}

// renderCard is the project card: no id prefix and no cursor chevron — the home
// grid marks the selection with the border alone.
func (s Screen) renderCard(kit screenkit.Kit, project domain.Project, selected bool, width int) string {
	title := project.Name
	if title == "" {
		title = project.Slug
	}
	painter := card.Painter{Styles: kit.Styles}
	spec := painter.Fit(card.Spec{Title: title, Selected: selected}, width)
	spec.Meta = []string{s.metaLine(project, spec.InnerWidth)}
	spec.Badges = s.projectBadges(kit, project, spec.InnerWidth)
	return painter.Render(spec)
}

// metaLine is `slug · path`, shortened path-first so the slug — the thing the
// user types to switch projects — survives a narrow card.
func (s Screen) metaLine(project domain.Project, contentWidth int) string {
	meta := project.Slug + " · " + project.RootPath
	if screenkit.VisibleWidth(meta) <= contentWidth {
		return meta
	}
	budget := contentWidth - screenkit.VisibleWidth(project.Slug+" · ")
	if budget < 4 {
		return project.Slug
	}
	return project.Slug + " · " + screenkit.TruncatePath(project.RootPath, budget)
}

// projectBadges is the open-task count followed by the project's tags.
func (s Screen) projectBadges(kit screenkit.Kit, project domain.Project, width int) []string {
	badges := make([]string, 0, len(s.tags[project.ID])+1)
	badges = append(badges, tokenstrip.CountBand(kit.Styles, s.pending[project.ID], kit.T("tui.badge.open")))
	for _, tag := range s.tags[project.ID] {
		label := tag.Label
		if label == "" {
			label = tag.Name
		}
		// Uppercased rather than hash-prefixed: a project label is not a task
		// tag, and the two read differently on purpose.
		badges = append(badges, tokenstrip.Label(kit.Styles, screenkit.Truncate(strings.ToUpper(label), width-2)))
	}
	return badges
}

func (s Screen) emptyHint(kit screenkit.Kit, columnWidth int) string {
	lines := []string{kit.Styles.HintAccent.Render(kit.T("tui.empty.home_no_projects_full")), "", kit.Styles.Hint.Render(kit.T("tui.home.register_with")), kit.Styles.Hint.Render(kit.T("tui.home.okt_init_example")), "", kit.Styles.Hint.Render(kit.T("tui.home.then_reopen_prefix")) + kit.Styles.HintAccent.Render(kit.T("tui.home.okt_tui_cmd")) + kit.Styles.Hint.Render(kit.T("tui.home.then_reopen_suffix"))}
	width := columnWidth - 6
	if width < 32 {
		width = 32
	}
	if width > 60 {
		width = 60
	}
	return kit.Styles.HintBox.Width(width).Render(strings.Join(lines, "\n"))
}

// wrapWords wraps a card title at one width. The two-width form lives in
// screenkit; home never hangs text under a prefix, so it passes the same number
// twice.
func wrapWords(value string, width int) []string {
	return screenkit.WrapWords(value, width, width)
}

func cloneTags(source map[int64][]domain.Tag) map[int64][]domain.Tag {
	if source == nil {
		return nil
	}
	clone := make(map[int64][]domain.Tag, len(source))
	for id, tags := range source {
		clone[id] = append([]domain.Tag(nil), tags...)
	}
	return clone
}

func clonePending(source map[int64]int) map[int64]int {
	if source == nil {
		return nil
	}
	clone := make(map[int64]int, len(source))
	for id, count := range source {
		clone[id] = count
	}
	return clone
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
