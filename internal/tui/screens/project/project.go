// Package project owns the project overview and its full-width form reader.
package project

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

const descriptionCap = 6

const projectFormSection = screenlayout.ID("project-form")

// The three zones this screen declares to the arranger. The ids key each zone's
// persisted cursor and scroll offset, so they are literal constants rather than
// anything derived from data.
const (
	sectionMeta      = screenlayout.ID("meta")
	sectionDashboard = screenlayout.ID("dashboard")
	sectionActivity  = screenlayout.ID("activity")
	// groupMeta stacks the meta panel and the dashboard inside ONE column, which
	// is the arrangement this screen composed by hand as
	// `JoinHorizontal(JoinVertical(meta, "", dashboard), "  ", activity)`.
	groupMeta    = screenlayout.ID("meta-column")
	sectionState = screenlayout.ID("project-state")
)

// zoneMinRows is a row floor, not a width, so it stays here. The width
// measures live in screenkit and arrive through [screenlayout.BesideFeed]
// and [screenlayout.Feed].
const (
	zoneMinRows = 3
)

type Focus uint8

const (
	FocusForm Focus = iota
	FocusDashboard
	FocusActivity
)

type BucketCount struct {
	Name  string
	Count int
}

type Dashboard struct {
	Buckets                         []BucketCount
	TotalTasks, RootTasks, SubTasks int
	PlanCount, PlanDone, PlanTotal  int
}

type Payload struct {
	Project     domain.ProjectContext
	Description string
	Tags        []domain.Tag
	Activity    []domain.Event
	Dashboard   Dashboard
	Tasks       []domain.Task
}

type Result struct {
	Generation uint64
	Payload    Payload
	Err        error
}

// Screen is the project overview: a meta panel, a dashboard and an activity
// feed laid out side by side on a wide terminal and stacked on a narrow one.
//
// It declares those three zones to [screenlayout] and holds nothing else about
// them. Before this migration it held a `focus`, a card `cursor` and a
// `linelist` document window, and it composed its own body: two width
// expressions, a breakpoint comparison, a per-branch translation of the feed
// cursor into a document row, and a row budget it derived itself. All of it is
// now the arranger's — the screen states minimums and renders content, and the
// arranger answers with a width, a row budget, a window and a cursor.
//
// The three zones scroll INDEPENDENTLY again, which is the obligation #2415
// recorded when it collapsed them onto one document window (comment 122985).
// Focus decides which zone the standard keys move; the arranger routes them.
type Screen struct {
	payload Payload
	// grid owns the nested composition's focus, cursors and zone offsets.
	grid          screengrid.State
	md            *markdown.Renderer
	activityCache *activityCardsCache
	requestGen    uint64
	loading       bool
	err           error
}

type activityCardsCache struct {
	cards map[activityCardKey]string
}

type activityCardKey struct {
	index   int
	width   int
	focused bool
}

func New() Screen {
	return Screen{grid: screengrid.NewState(), md: markdown.New(screenkit.MarkdownTokens{}), activityCache: &activityCardsCache{cards: make(map[activityCardKey]string)}}
}

func (s Screen) ID() screenhost.ID { return screenhost.Project }

// Focus is which zone the standard keys move, translated out of the arranger's
// focused section id. "" is the state before anything has been focused, which
// the arranger routes to the first scrollable section — the meta panel.
func (s Screen) Focus() Focus {
	switch s.grid.Focus() {
	case sectionDashboard:
		return FocusDashboard
	case sectionActivity:
		return FocusActivity
	default:
		return FocusForm
	}
}

// ActivityCursor is the selected feed card, or -1 when the feed does not hold
// focus. The arranger keeps a cursor for the section at all times so a keystroke
// lands on a card rather than nudging an offset; the SELECTION is only shown,
// and only reported here, while the zone is focused — which is the behaviour
// this screen has always had.
func (s Screen) ActivityCursor() int {
	if s.Focus() != FocusActivity {
		return -1
	}
	return s.grid.Layout().Cursor(sectionActivity)
}

// BodyScroll is the offset of the zone that holds focus.
func (s Screen) BodyScroll() int { return s.zoneScroll(s.focusedSection()) }

func (s Screen) zoneScroll(id screenlayout.ID) int { return s.grid.Layout().Offset(id) }

func (s Screen) focusedSection() screenlayout.ID {
	switch s.grid.Focus() {
	case sectionDashboard:
		return sectionDashboard
	case sectionActivity:
		return sectionActivity
	default:
		return sectionMeta
	}
}

func (s Screen) IsLoading() bool          { return s.loading }
func (s Screen) Err() error               { return s.err }
func (s Screen) Generation() uint64       { return s.requestGen }
func (s Screen) Payload() Payload         { return clonePayload(s.payload) }
func (s Screen) Activity() []domain.Event { return append([]domain.Event(nil), s.payload.Activity...) }

func (s Screen) Loading(generation uint64) Screen {
	s.requestGen, s.loading, s.err = generation, true, nil
	return s
}

func (s Screen) Apply(result Result) Screen {
	if result.Generation != s.requestGen {
		return s
	}
	s.loading, s.err = false, result.Err
	if result.Err != nil {
		return s
	}
	s.payload, s.activityCache = clonePayload(result.Payload), &activityCardsCache{cards: make(map[activityCardKey]string)}
	return s
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	if s.grid.Focus() == "" {
		s.grid = s.grid.Resync(kit, s.gridBox(frame), s.root(frame))
	}
	// A screen resolves its own vocabulary before the arranger. A declined
	// key is the only kind that reaches the layout, so a nested grid cannot
	// consume the project's `f` action before it opens the project form.
	if out, handled := s.handleProjectAction(key); handled {
		return out
	}
	if next, handled := s.projectGridKey(kit, frame, key); handled {
		s.grid = next
		return screenhost.Stay(s, nil)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) handleProjectAction(key tea.KeyMsg) (screenhost.Outcome, bool) {
	switch key.String() {
	case "f":
		return screenhost.OpenProjectForm(s, nil), true
	case "R":
		return screenhost.OpenProjectResume(s, nil), true
	case "enter":
		if cursor := s.ActivityCursor(); cursor >= 0 && cursor < len(s.payload.Activity) {
			event := s.payload.Activity[cursor]
			if event.EventType == domain.EventTypeComment {
				return screenhost.OpenProjectComment(s, event.ID, nil), true
			}
			if event.EntityType == domain.EventEntityTask && event.EntityID > 0 {
				return screenhost.OpenTask(s, event.EntityID, nil), true
			}
		}
		return screenhost.Stay(s, nil), true
	case "r":
		s = s.Loading(s.requestGen + 1)
		out := screenhost.Reload(s, nil)
		out.Action.Generation = s.requestGen
		return out, true
	case "esc":
		return screenhost.Back(s, nil), true
	}
	return screenhost.Outcome{}, false
}

func (s Screen) projectGridKey(kit screenkit.Kit, frame screenhost.Frame, key tea.KeyMsg) (screengrid.State, bool) {
	next, handled := s.grid.HandleKey(kit, s.gridBox(frame), key.String(), s.root(frame))
	if !handled {
		return s.grid, false
	}
	if s.gridBox(frame).Rows >= zoneMinRows*3 {
		return next, true
	}
	if next.Focus() != sectionMeta {
		return s.grid, true
	}
	placement, exists := screengrid.Render(kit, next, s.gridBox(frame), s.root(frame)).Placement(next.Focus())
	if !exists || placement.Dropped {
		return s.grid, true
	}
	return next, true
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	switch event {
	case screenhost.LifecycleEnter:
		s.grid = screengrid.NewState()
		s.grid = s.grid.Resync(frame.Kit(), s.gridBox(frame), s.root(frame))
	case screenhost.LifecycleResize:
		s.grid = s.grid.Resync(frame.Kit(), s.gridBox(frame), s.root(frame))
	}
	return screenhost.Stay(s, nil)
}

// OwnsKey claims the arranger's whole key table plus this screen's own keys.
//
// The motion half is READ from [screenlayout.StandardBindings] rather than
// restated, because a screen that hand-lists the spellings is a private copy of
// a table the arranger already holds — and it was short by two the moment the
// migration landed: the arranger routes `ctrl+u` and `ctrl+d`, which the old
// list did not mention, so the host would never have handed them over.
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
	// "pgdn" is the arranger's spelling of pgdown; the rest are this screen's.
	case "f", "R", "enter", "r", "esc", "n", "e", "c", "m", "A":
		return true
	}
	return false
}

func (s Screen) OwnsFooter() bool { return true }

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	bindings := []screenhost.FooterBinding{{Key: keynav.Default.Zones.Primary(), Label: frame.Text("tui.footer.zone"), Primary: true}, {Key: "f", Label: frame.Text("tui.footer.focus_description"), Primary: true}, {Key: "R", Label: frame.Text("tui.footer.project_resume"), Primary: true}}
	// The motion vocabulary is advertised in every zone because it now WORKS
	// in every zone: j/k nudge the document (or walk the feed cursor when the
	// feed holds focus), pgup/pgdn page it, g/G jump to either end. Advertising
	// it only for the activity zone is what taught users the other two zones
	// were frozen.
	bindings = append(bindings,
		frame.FooterScroll(false),
		frame.FooterPage(false),
		frame.FooterTopBottom(false),
	)
	if s.Focus() == FocusActivity {
		bindings = append(bindings, screenhost.FooterBinding{Key: "enter", Label: frame.Text("tui.footer.open_comment_activity")})
	}
	return append(bindings,
		frame.FooterRefresh(false),
		frame.FooterBack(false),
		frame.FooterHelp(false),
	)
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "project", Title: frame.Text("tui.help.project.title"), Bindings: []screenhost.HelpBinding{
		{Key: keynav.Default.Zones.Primary(), Description: frame.Text("tui.footer.zone")},
		{Key: "f", Description: frame.Text("tui.footer.focus_description")},
		{Key: "R", Description: frame.Text("tui.footer.project_resume")},
		{Key: "j k · ↑ ↓", Description: frame.Text("tui.footer.scroll")},
		{Key: "pgup · pgdn", Description: frame.Text("tui.footer.page")},
		{Key: "g · G", Description: frame.Text("tui.footer.top_bottom")},
		{Key: "enter", Description: frame.Text("tui.footer.open_comment_activity")},
		{Key: "r", Description: frame.Text("tui.footer.refresh")},
		{Key: "esc", Description: frame.Text("tui.footer.back")},
	}}}

}

func (s Screen) stateSection(frame screenhost.Frame) screenlayout.Func {
	kit := frame.Kit()
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionState, MinRows: 1},
		Body: func(screenlayout.Canvas) screenlayout.Block {
			body, _ := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...)
			return screenlayout.Block{Header: framed.Document(kit.Panel(body)), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s Screen) metaSection(kit screenkit.Kit, focus Focus) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.BesideFeed(sectionMeta, groupMeta, screenkit.PanelFloor, zoneMinRows, screenlayout.ScrollItems),
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return screenlayout.Block{Items: strings.Split(s.metaPanel(kit, canvas.Width(), focus == FocusForm), "\n"), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s Screen) dashboardSection(kit screenkit.Kit, focus Focus) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.BesideFeed(sectionDashboard, groupMeta, screenkit.PanelFloor, zoneMinRows, screenlayout.ScrollItems),
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			header, rows := s.dashboardPanel(kit, focus == FocusDashboard, canvas.Width())
			return screenlayout.Block{Header: header, Items: rows, Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s Screen) activitySection(kit screenkit.Kit, focus Focus) screenlayout.Func {
	spec := screenlayout.Feed(sectionActivity, zoneMinRows)
	spec.SelectFirst = true
	return screenlayout.Func{
		Def: spec,
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			header := []string{kit.Styles.SectionKickerCount(kit.T("tui.kicker.activity"), len(s.payload.Activity), focus == FocusActivity)}
			selected := -1
			if focus == FocusActivity {
				selected = canvas.Cursor()
			}
			if len(s.payload.Activity) == 0 {
				return screenlayout.Block{Header: header, Items: []string{"", kit.Styles.Hint.Render(kit.T("tui.empty.project_activity"))}, Cursor: screenlayout.NoSelection()}
			}
			return screenlayout.Block{Header: header, Items: s.activityCards(kit, canvas.Width(), selected)}
		},
	}
}

// root is the three-zone composition. The grid owns focus and scrolling while
// each cell owns only its canvas-sized body.
func (s Screen) root(frame screenhost.Frame) screengrid.Node {
	if s.stateActive(frame) {
		section := s.stateSection(frame)
		return screengrid.Cell(section.Def, section.Body)
	}
	kit := frame.Kit()
	focus := s.Focus()
	metaSection := s.metaSection(kit, focus)
	dashboardSection := s.dashboardSection(kit, focus)
	activitySection := s.activitySection(kit, focus)
	meta := screengrid.Cell(metaSection.Def, metaSection.Body)
	dashboard := screengrid.Cell(dashboardSection.Def, dashboardSection.Body)
	activity := screengrid.Cell(activitySection.Def, activitySection.Body)
	metaColumn := screengrid.Rows(screenlayout.Spec{ID: groupMeta, MinWidth: screenkit.PanelFloor}, meta, dashboard)
	return screengrid.Cols(screenlayout.Spec{ID: screenlayout.ID("project-root")}, metaColumn, activity)
}

func (s Screen) gridBox(frame screenhost.Frame) screenlayout.Box {
	return screenlayout.HostBox(frame.Kit())
}

func (s Screen) activityCards(kit screenkit.Kit, panelWidth, selected int) []string {
	width := max(8, panelWidth-2)
	cards := make([]string, 0, len(s.payload.Activity))
	for i, event := range s.payload.Activity {
		focused := i == selected
		if s.activityCache == nil {
			cards = append(cards, s.activityCard(kit, event, focused, width))
			continue
		}
		key := activityCardKey{index: i, width: width, focused: focused}
		cardView, ok := s.activityCache.cards[key]
		if !ok {
			cardView = s.activityCard(kit, event, focused, width)
			s.activityCache.cards[key] = cardView
		}
		cards = append(cards, cardView)
	}
	return cards
}

func (s Screen) activityCard(kit screenkit.Kit, event domain.Event, focused bool, width int) string {
	if event.EventType == domain.EventTypeComment {
		return s.renderCommentCard(kit, event, focused, width)
	}
	return s.renderSystemEventCard(kit, event, focused, width)
}

func (s Screen) renderCommentCard(kit screenkit.Kit, event domain.Event, focused bool, width int) string {
	tags := make([]string, len(event.Tags))
	for i, tag := range event.Tags {
		tags[i] = tag.Label
	}
	return card.Painter{Styles: kit.Styles}.Comment(card.Comment{
		Author:    event.AuthorType,
		Timestamp: event.CreatedAt,
		Body:      event.Body,
		EmptyText: kit.T("tui.comment.empty"),
		Tags:      tags,
		MoreFmt:   kit.T("tui.event.more_lines_fmt"),
		Focused:   focused,
		Width:     width,
	})
}

func (s Screen) renderSystemEventCard(kit screenkit.Kit, event domain.Event, focused bool, width int) string {
	return card.Painter{Styles: kit.Styles}.System(card.System{
		Label:     s.systemEventLabel(kit, event),
		Timestamp: event.CreatedAt,
		Focused:   focused,
		Width:     width,
	})
}

func (s Screen) systemEventLabel(kit screenkit.Kit, event domain.Event) string {
	switch event.EventType {
	case domain.EventTypeTaskCreated:
		if bucket := eventPayloadField(event.Payload, "bucket"); bucket != "" {
			return fmt.Sprintf(kit.T("tui.event.task_created_in_fmt"), bucket)
		}
		return kit.T("tui.event.task_created")
	case domain.EventTypeTaskMoved:
		from, to := eventPayloadField(event.Payload, "from"), eventPayloadField(event.Payload, "to")
		if from != "" && to != "" {
			return fmt.Sprintf(kit.T("tui.event.task_moved_from_to_fmt"), from, to)
		}
		if to != "" {
			return fmt.Sprintf(kit.T("tui.event.task_moved_to_fmt"), to)
		}
		return kit.T("tui.event.task_moved")
	case domain.EventTypeTaskCompleted:
		if bucket := eventPayloadField(event.Payload, "bucket"); bucket != "" {
			return fmt.Sprintf(kit.T("tui.event.task_completed_in_fmt"), bucket)
		}
		return kit.T("tui.event.task_completed")
	default:
		return event.EventType
	}
}

func eventPayloadField(payload, key string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(payload), &fields) != nil {
		return ""
	}
	value, _ := fields[key].(string)
	return value
}

func (s Screen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	box := s.gridBox(frame)
	if s.stateActive(frame) {
		box.Width, box.Rows = frame.Width(), frame.Height()
	}
	arranged := screengrid.Render(kit, s.grid, box, s.root(frame)).View
	if s.stateActive(frame) {
		return arranged
	}
	return "\n" + screenkit.Indent(arranged, 2)
}

func (s Screen) stateActive(frame screenhost.Frame) bool {
	return s.err != nil || s.loading || s.payload.Project.ID == 0
}

// states centralizes failed/loading/empty precedence for the overview.
func (s Screen) states(frame screenhost.Frame) []screenstate.State {
	kit := frame.Kit()
	id := screenstate.For(fmt.Sprintf(kit.T("tui.kicker.project_fmt"), s.payload.Project.Slug))
	return []screenstate.State{
		id.Failed(s.err, kit.T("tui.stat.error_badge")),
		id.Loading(s.loading, kit.T("tui.loading.project")),
		id.Vazio(s.payload.Project.ID == 0 && !s.loading && s.err == nil, kit.T("tui.empty.project_not_found"), ""),
	}
}

// metaPanel renders the project meta box at the width the ARRANGER gave the
// zone. It used to derive that width itself, out of a copy of the feed's
// percentage expression and the join's two spaces.
func (s Screen) metaPanel(kit screenkit.Kit, panelWidth int, focused bool) string {
	valueWidth := panelWidth - gridtable.LabelWidth - 3
	if valueWidth < 8 {
		valueWidth = 8
	}
	slug := screenkit.Sanitize(s.payload.Project.Slug)
	kicker := kit.Styles.SectionKicker(fmt.Sprintf(kit.T("tui.kicker.project_fmt"), slug), focused)
	root := s.payload.Project.RootPath
	if strings.TrimSpace(root) == "" {
		root = "—"
	}
	tags := kit.Styles.Hint.Render("—")
	if line := tagsLine(s.payload.Tags); line != "" {
		tags = line
	}
	detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).Custom(gridtable.Styled(kicker)).
		Row(kit.T("tui.row.name"), s.payload.Project.Name).
		Row(kit.T("tui.row.slug"), s.payload.Project.Slug).
		Row(kit.T("tui.row.root_path"), root).
		Row(kit.T("tui.row.id"), fmt.Sprintf("%d", s.payload.Project.ID)).
		Row(kit.T("tui.row.tags"), tags).
		Row(kit.T("tui.row.comments"), fmt.Sprintf("%d", len(s.payload.Activity))).
		Kicker(kit.T("tui.kicker.description")).Span(gridtable.Styled(s.inlineDescription(kit, valueWidth)))
	return detail.View(kit.Styles.Border)
}

func (s Screen) inlineDescription(kit screenkit.Kit, width int) string {
	body := strings.TrimSpace(s.payload.Description)
	if body == "" {
		return kit.Styles.Hint.Render(kit.T("tui.empty.project_description"))
	}
	s.md.Reload(kit.Markdown)
	rendered := markdown.Body(s.md, body, width, true)
	lines := strings.Split(rendered, "\n")
	if len(lines) <= descriptionCap {
		return rendered
	}
	return strings.Join(lines[:descriptionCap], "\n") + "\n" + kit.Styles.Hint.Render(fmt.Sprintf(kit.T("tui.task.description_more_fmt"), len(lines)-descriptionCap))
}

// dashboardPanel returns the zone's chrome and its scrollable rows separately,
// because the arranger charges the two differently: a Header is measured, paid
// for and pinned, while the rows below it are the item window.
func (s Screen) dashboardPanel(kit screenkit.Kit, focused bool, width int) (header, rows []string) {
	d := s.payload.Dashboard
	fields := make([][2]string, 0, len(d.Buckets)+1)
	for _, bucket := range d.Buckets {
		fields = append(fields, [2]string{bucket.Name, fmt.Sprintf("%d", bucket.Count)})
	}
	fields = append(fields, [2]string{kit.T("tui.dashboard.total"), fmt.Sprintf("%d", d.TotalTasks)})
	tasks := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.dashboard.tasks"), fields...)
	subs := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.dashboard.subtasks"), [2]string{kit.T("tui.dashboard.roots"), fmt.Sprintf("%d", d.RootTasks)}, [2]string{kit.T("tui.dashboard.children"), fmt.Sprintf("%d", d.SubTasks)})
	percent := "—"
	if d.PlanTotal > 0 {
		percent = fmt.Sprintf("%.0f%%", float64(d.PlanDone)/float64(d.PlanTotal)*100)
	}
	plans := gridtable.Rows(kit.Styles.Kicker, kit.T("tui.dashboard.plans"), [2]string{kit.T("tui.dashboard.plan_count"), fmt.Sprintf("%d", d.PlanCount)}, [2]string{kit.T("tui.dashboard.plan_progress"), fmt.Sprintf("%d/%d", d.PlanDone, d.PlanTotal)}, [2]string{kit.T("tui.dashboard.plan_percent"), percent})
	kicker := kit.Styles.SectionKicker(kit.T("tui.kicker.dashboard"), focused)
	// Shared Auto + FitWidths sizing; join with a single newline so adjacent
	// table borders stay flush (Render's stacked gap is for distinct panels).
	widths := gridtable.ColumnWidths(width, gridtable.Options{
		LabelWidth: 10, ValueWidth: 8, Auto: true,
	}, tasks, subs, plans)
	body := strings.Join([]string{
		gridtable.RenderCells(tasks, widths, kit.Styles.Border),
		gridtable.RenderCells(subs, widths, kit.Styles.Border),
		gridtable.RenderCells(plans, widths, kit.Styles.Border),
	}, "\n")
	return []string{kicker, ""}, strings.Split(body, "\n")
}

func tagsLine(tags []domain.Tag) string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		name := screenkit.Sanitize(tag.Label)
		if name == "" {
			name = screenkit.Sanitize(tag.Name)
		}
		names = append(names, name)
	}
	return strings.Join(names, " · ")
}

func clonePayload(payload Payload) Payload {
	payload.Tags = append([]domain.Tag(nil), payload.Tags...)
	payload.Activity = append([]domain.Event(nil), payload.Activity...)
	payload.Dashboard.Buckets = append([]BucketCount(nil), payload.Dashboard.Buckets...)
	payload.Tasks = append([]domain.Task(nil), payload.Tasks...)
	return payload
}

type FormScreen struct {
	payload          Payload
	md               *markdown.Renderer
	markdownRendered bool
	grid             screengrid.State
	body             *formBodyCache
}

type formBodyCache struct {
	memo       screenlayout.BlockMemo[formBodyKey]
	lines      []string
	linesKey   formBodyKey
	linesReady bool
	root       screengrid.Node
	rootKey    formBodyKey
	rootReady  bool
}

type formBodyKey struct {
	width    int
	rendered bool
}

func NewForm() FormScreen {
	return FormScreen{md: markdown.New(screenkit.MarkdownTokens{}), markdownRendered: true, grid: screengrid.NewState(), body: &formBodyCache{}}
}
func (s FormScreen) ID() screenhost.ID { return screenhost.ProjectForm }
func (s FormScreen) Apply(payload Payload) FormScreen {
	s.payload, s.grid, s.body = clonePayload(payload), screengrid.NewState(), &formBodyCache{}
	return s
}
func (s FormScreen) Scroll() int             { return s.grid.Layout().Offset(projectFormSection) }
func (s FormScreen) MarkdownRendered() bool  { return s.markdownRendered }
func (s FormScreen) BlocksHostInput() bool   { return true }
func (s FormScreen) OwnsKey(tea.KeyMsg) bool { return true }
func (s FormScreen) OwnsFooter() bool        { return true }

func (s FormScreen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
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
	if next, handled := s.grid.HandleKey(frame.Kit(), screenlayout.HostBox(frame.Kit()), key.String(), s.formRoot(frame)); handled {
		s.grid = next
	}
	return screenhost.Stay(s, nil)
}
func (s FormScreen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		s.grid = s.grid.Resync(frame.Kit(), screenlayout.HostBox(frame.Kit()), s.formRoot(frame))
	}
	return screenhost.Stay(s, nil)
}
func (s FormScreen) formSection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: projectFormSection, MinRows: 1, Scroll: screenlayout.ScrollItems},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			composed := s.formBlock(kit, canvas.Width())
			if len(composed.Items) == 0 {
				return screenlayout.Block{Cursor: screenlayout.NoSelection()}
			}
			key := formBodyKey{width: canvas.Width(), rendered: s.markdownRendered}
			if s.body == nil || !s.body.linesReady || s.body.linesKey != key {
				lines := strings.Split(strings.TrimSuffix(composed.Items[0], "\n"), "\n")
				if s.body != nil {
					s.body.lines, s.body.linesKey, s.body.linesReady = lines, key, true
				}
				return screenlayout.Block{Items: lines, Cursor: screenlayout.NoSelection()}
			}
			return screenlayout.Block{Items: s.body.lines, Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s FormScreen) formRoot(frame screenhost.Frame) screengrid.Node {
	kit := frame.Kit()
	key := formBodyKey{width: kit.AvailableWidth(), rendered: s.markdownRendered}
	if s.body != nil && s.body.rootReady && s.body.rootKey == key {
		return s.body.root
	}
	section := s.formSection(kit)
	root := screengrid.Cell(section.Def, section.Body)
	if s.body != nil {
		s.body.root, s.body.rootKey, s.body.rootReady = root, key, true
	}
	return root
}

func (s FormScreen) formBlock(kit screenkit.Kit, width int) screenlayout.Block {
	valueWidth := width - gridtable.LabelWidth - 3
	if valueWidth < 24 {
		valueWidth = 24
	}
	root := s.payload.Project.RootPath
	if strings.TrimSpace(root) == "" {
		root = "—"
	}
	tags := kit.Styles.Hint.Render("—")
	if line := tagsLine(s.payload.Tags); line != "" {
		tags = line
	}
	inputs := []string{s.payload.Project.Name, s.payload.Project.Slug, root, tags, s.payload.Description}
	key := formBodyKey{width: width, rendered: s.markdownRendered}
	build := func() screenlayout.Block {
		detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).
			Custom(gridtable.Styled(kit.Styles.FocusKicker(fmt.Sprintf(kit.T("tui.kicker.project_fmt"), screenkit.Sanitize(s.payload.Project.Slug))))).
			Row(kit.T("tui.row.name"), s.payload.Project.Name).
			Row(kit.T("tui.row.slug"), s.payload.Project.Slug).
			Row(kit.T("tui.row.root_path"), root).
			Row(kit.T("tui.row.id"), fmt.Sprintf("%d", s.payload.Project.ID)).
			Row(kit.T("tui.row.tags"), tags).
			Kicker(kit.T("tui.kicker.description"))
		body := strings.TrimSpace(s.payload.Description)
		if body == "" {
			detail = detail.Span(gridtable.Styled(kit.Styles.Hint.Render(kit.T("tui.empty.project_description"))))
		} else {
			s.md.Reload(kit.Markdown)
			detail = detail.Span(gridtable.Styled(markdown.Body(s.md, body, valueWidth, s.markdownRendered)))
		}
		return screenlayout.Block{Items: []string{detail.View(kit.Styles.Border)}}
	}
	if s.body == nil {
		return build()
	}
	return s.body.memo.Block(key, inputs, build)
}

func (s FormScreen) View(frame screenhost.Frame) string {
	kit := frame.Kit()
	rendered := screengrid.Render(kit, s.grid, screenlayout.HostBox(kit), s.formRoot(frame)).View
	return "\n" + screenkit.Indent(rendered, 2)
}
func (s FormScreen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{frame.FooterCloseFocus(true), frame.FooterScroll(false), frame.FooterPage(false), frame.FooterTopBottom(false), frame.FooterToggleMarkdown(false), frame.FooterHelp(false)}
}
func (s FormScreen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "project_form", Title: frame.Text("tui.kicker.description"), Bindings: []screenhost.HelpBinding{{Key: "f · esc", Description: frame.Text("tui.footer.close_focus")}, {Key: "j k", Description: frame.Text("tui.footer.scroll")}, {Key: "M", Description: frame.Text("tui.footer.toggle_markdown")}}}}
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.Screen = FormScreen{}
var _ screenhost.KeyOwner = FormScreen{}
var _ screenhost.FooterOwner = FormScreen{}
var _ screenhost.InteractionBlocker = FormScreen{}
