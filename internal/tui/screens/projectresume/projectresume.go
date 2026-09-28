// Package projectresume owns the dedicated Project › Resume screen: distribution,
// likely next work, blocked work, and dependencies the host loads via the facade.
package projectresume

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

var bodySpec = screenlayout.Spec{
	ID: "project-resume-body", MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems,
}

// BucketCount is the distribution row the resume screen paints.
type BucketCount struct {
	BucketKey string
	Name      string
	Count     int
}

// TaskSummary is the likely-next / blocked row the resume screen paints.
type TaskSummary struct {
	ID        int64
	Title     string
	BucketKey string
	Priority  string
}

// DependencySummary is the blocked-by edge the resume screen paints.
type DependencySummary struct {
	TaskID          int64
	DependsOnTaskID int64
}

// Payload is the ResumeProject projection the host loads through the facade.
type Payload struct {
	TaskBuckets    []BucketCount
	LikelyNextWork []TaskSummary
	BlockedWork    []TaskSummary
	Dependencies   []DependencySummary
	NextStepPrompt string
	Err            error
}

// Screen is the Project › Resume implementation of screenhost.Screen.
type Screen struct {
	payload   Payload
	loaded    bool
	grid      screengrid.State
	bodyLines []string
	bodyWidth int
}

// New returns an unbound resume screen.
func New() Screen { return Screen{grid: screengrid.NewState()} }

func (s Screen) ID() screenhost.ID { return screenhost.ProjectResume }

func (s Screen) Loaded() bool { return s.loaded }

func (s Screen) Payload() Payload { return s.payload }

func (s Screen) Scroll() int { return s.grid.Layout().Offset(bodySpec.ID) }

// Apply folds a loaded ResumeProject payload into the screen and invalidates
// the stored body. Composition waits for Lifecycle; the host follows Apply with
// its resize lifecycle so the first paint is never an uncomposed body.
func (s Screen) Apply(payload Payload) Screen {
	s.payload = payload
	s.loaded = true
	s.bodyLines = nil
	s.bodyWidth = 0
	return s
}

// Loading clears the loaded flag so the view shows a computing placeholder.
func (s Screen) Loading() Screen {
	s.loaded = false
	s.payload.Err = nil
	s.bodyLines = nil
	s.bodyWidth = 0
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	switch key.String() {
	case "r":
		s = s.Loading()
		return screenhost.Reload(s, nil)
	case "esc":
		return screenhost.Back(s, nil)
	}
	s.grid, _ = s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.root(kit))
	return screenhost.Stay(s, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	kit := frame.Kit()
	box := s.panelBox(kit)
	s = s.composeOn(event, kit, box)
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	spelling := key.String()
	for _, binding := range screenlayout.StandardBindings() {
		for _, owned := range binding.Keys {
			if owned == spelling {
				return true
			}
		}
	}
	switch spelling {
	case "r", "esc":
		return true
	}
	return false
}

func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) ResetOnNavigation() bool { return true }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		frame.FooterRefresh(true),
		frame.FooterScroll(false),
		frame.FooterBack(false),
		frame.FooterHelp(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID:    "project_resume",
		Title: frame.Text("tui.help.project_resume.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "r", Description: frame.Text("tui.footer.refresh")},
			{Key: "j k · pgup · pgdn · g G", Description: frame.Text("tui.footer.scroll")},
			{Key: "esc", Description: frame.Text("tui.footer.back")},
		},
	}}
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	// Liveness guard — see the note on description.View. It is a budget
	// constraint, not a precedence decision. payload.Err is included because
	// compose skips the body on the same condition (#126545).
	if !s.loaded || s.payload.Err != nil {
		if body, ok := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(kit)...); ok {
			return kit.Panel(body)
		}
	}
	return "\n" + screenkit.Indent(s.gridResult(frame).View, 2)
}

// states are the candidates screenstate ranks. This screen has no empty state:
// it either computed a resume or it did not, so Vazio is absent from the set
// rather than passed as a false flag — the component paints only what is live,
// so adding it would be inventing a state the screen does not have.
func (s Screen) states(kit screenkit.Kit) []screenstate.State {
	id := screenstate.For(kit.T("tui.kicker.project_resume"))
	return []screenstate.State{
		id.Failed(s.payload.Err, kit.T("tui.stat.error_badge")),
		id.Loading(!s.loaded, kit.T("tui.project_resume.computing")),
	}
}

func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().ViewportRows()
	if kit.Height <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	return screenlayout.Box{
		Width: kit.PanelContentWidth(),
		Rows:  rows,
	}
}

func (s Screen) composeOn(event screenhost.LifecycleEvent, kit screenkit.Kit, box screenlayout.Box) Screen {
	if event != screenhost.LifecycleEnter && event != screenhost.LifecycleResize {
		return s
	}
	s.bodyWidth = box.Width
	s.bodyLines = nil
	if s.loaded && s.payload.Err == nil {
		// box.Width is the exact width the Cell receives. AvailableWidth()-4
		// used to reach the same integer (both Width-8), but carrying the
		// arranged width closes that P5 side door rather than recomputing it.
		inner := max(1, box.Width-panel.Borders)
		s.bodyLines = screenkit.CapRows(strings.Split(s.renderBody(kit, inner), "\n"), inner)
	}
	s.grid = s.grid.Resync(kit, box, s.root(kit))
	return s
}

func (s Screen) bodySection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: bodySpec,
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.bodyBlock(kit, canvas)
		},
	}
}

func (s Screen) root(kit screenkit.Kit) screengrid.Node {
	section := s.bodySection(kit)
	return screengrid.Cell(section.Def, section.Body)
}

func (s Screen) bodyBlock(kit screenkit.Kit, canvas screenlayout.Canvas) screenlayout.Block {
	width := max(1, canvas.Width())
	inner := max(1, width-panel.Borders)
	lines := screenkit.CapRows(strings.Split(s.renderBody(kit, inner), "\n"), inner)
	if len(lines) == 0 {
		return screenlayout.Block{Cursor: screenlayout.NoSelection()}
	}
	kicker, items := lines[0], lines[1:]
	var columnHeader []string
	if len(items) > 0 && items[0] == "" {
		columnHeader, items = []string{""}, items[1:]
	}
	block := framed.List(kit.Styles.Border, width, kicker, columnHeader, items)
	block.Cursor = screenlayout.NoSelection()
	return block
}

func (s Screen) gridResult(frame screenhost.Frame) screengrid.Result {
	kit := frame.Kit()
	return screengrid.Render(kit, s.grid, s.panelBox(kit), s.root(kit))
}

func (s Screen) renderBody(kit screenkit.Kit, width int) string {
	p := s.payload
	sections := []string{
		s.section(kit, 1, kit.T("tui.project_resume.distribution.kicker"), width, s.distributionBody(kit, p.TaskBuckets)),
		s.section(kit, 2, kit.T("tui.project_resume.likely_next.kicker"), width, s.taskBody(kit, p.LikelyNextWork, "tui.project_resume.likely_next.empty")),
		s.section(kit, 3, kit.T("tui.project_resume.blocked.kicker"), width, s.taskBody(kit, p.BlockedWork, "tui.project_resume.blocked.empty")),
		s.section(kit, 4, kit.T("tui.project_resume.dependencies.kicker"), width, s.depsBody(kit, p.Dependencies)),
	}
	header := kit.Styles.FocusKicker(kit.T("tui.kicker.project_resume")) + kit.Styles.Hint.Render("  // "+kit.T("tui.project_resume.subtitle"))
	out := header + "\n\n" + strings.Join(sections, "\n\n")
	if prompt := strings.TrimSpace(screenkit.Sanitize(p.NextStepPrompt)); prompt != "" {
		out += "\n\n" + kit.Styles.Hint.Render(prompt)
	}
	return out
}

func (s Screen) section(kit screenkit.Kit, n int, kicker string, width int, body []string) string {
	head := kit.Styles.Info.Render(fmt.Sprintf("#%d ", n)) + kit.Styles.Kicker(kicker)
	rows := []string{head, kit.HRule(width)}
	rows = append(rows, body...)
	return strings.Join(rows, "\n")
}

func (s Screen) distributionBody(kit screenkit.Kit, buckets []BucketCount) []string {
	if len(buckets) == 0 {
		return []string{kit.Styles.Hint.Render(kit.T("tui.project_resume.distribution.empty"))}
	}
	counts := make([]string, len(buckets))
	names := make([]string, len(buckets))
	for i, bucket := range buckets {
		counts[i] = fmt.Sprintf("%3d", bucket.Count)
		names[i] = screenkit.Sanitize(bucket.Name)
		if names[i] == "" {
			names[i] = screenkit.Sanitize(bucket.BucketKey)
		}
	}
	painted := screenkit.PaintColumn(kit.Styles.HintAccent, counts)
	rows := make([]string, len(buckets))
	for i := range buckets {
		rows[i] = painted[i] + "  " + names[i]
	}
	return rows
}

func (s Screen) taskBody(kit screenkit.Kit, tasks []TaskSummary, emptyKey string) []string {
	if len(tasks) == 0 {
		return []string{kit.Styles.Hint.Render(kit.T(emptyKey))}
	}
	heads := make([]string, len(tasks))
	metas := make([]string, len(tasks))
	for i, task := range tasks {
		title := screenkit.Sanitize(task.Title)
		if title == "" {
			title = fmt.Sprintf("#%d", task.ID)
		}
		heads[i] = fmt.Sprintf("#%d  %s", task.ID, title)
		metas[i] = taskMeta(task)
	}
	painted := screenkit.PaintColumn(kit.Styles.Hint, metas)
	rows := make([]string, len(tasks))
	for i := range tasks {
		rows[i] = heads[i]
		if metas[i] != "" {
			rows[i] += "  " + painted[i]
		}
	}
	return rows
}

// taskMeta is the `bucket · priority` tail of a task row, either half optional.
func taskMeta(task TaskSummary) string {
	meta := screenkit.Sanitize(task.BucketKey)
	priority := screenkit.Sanitize(task.Priority)
	if priority == "" {
		return meta
	}
	if meta != "" {
		meta += " · "
	}
	return meta + priority
}

func (s Screen) depsBody(kit screenkit.Kit, deps []DependencySummary) []string {
	if len(deps) == 0 {
		return []string{kit.Styles.Hint.Render(kit.T("tui.project_resume.dependencies.empty"))}
	}
	rows := make([]string, 0, len(deps))
	for _, dep := range deps {
		rows = append(rows, fmt.Sprintf("#%d → #%d", dep.TaskID, dep.DependsOnTaskID))
	}
	return rows
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.NavigationResetter = Screen{}
