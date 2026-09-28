// Package board owns the Tasks > Board screen's move mode and the card each
// lane paints. The shared task read model arrives from internal/taskprojection;
// lane/card cursors, scroll windows and the horizontal carousel are the grid's.
package board

import (
	"hash/fnv"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/keynav"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
)

// Deps is the immutable snapshot the host supplies on each frame.
//
// It used to also carry RenderCard / CardHeight / ColumnStyle / EmptyStyle
// callbacks back into the root package, because the board could describe a
// lane but not paint one. That is why a lane drawn from a fixture came out
// unbordered and cardless. The card and the column are components now, so the
// board paints its own and the host hands over DATA only.
type Deps struct {
	Projection   taskprojection.Projection
	Tasks        []domain.Task
	Workflow     domain.Workflow
	Dependencies []domain.TaskDependency
	Comments     []domain.Comment
	View         config.BoardViewSettings
	Priorities   []config.PriorityDefinition
}

// Screen implements the Tasks > Board contract.
//
// There is no lane cursor, no card cursor, no per-lane scroll offset and no
// rendered-body cache on it. All four were the board's and all four are the
// grid's now: [Screen.Column] and [Screen.Card] READ them back out of
// screengrid.State rather than holding a second copy that has to be kept in
// step with it.
type Screen struct {
	deps          Deps
	projection    taskprojection.Projection
	tasksByBucket map[string][]domain.Task
	grid          screengrid.State
	moveMode      bool
	dataKey       uint64
	// memos is one composition memo per lane, shared between screen values on
	// purpose: a body renders under a value receiver, during Update and again
	// during View, and neither may write back. What is shared is derived work,
	// never a decision — an entry is served only when its key matches what the
	// caller has just built. See [Screen.laneMemo].
	memos map[string]*screenlayout.BlockMemo[laneMemoKey]
}

func New() Screen {
	return Screen{
		grid:  screengrid.NewState(),
		memos: map[string]*screenlayout.BlockMemo[laneMemoKey]{},
	}
}

func (s Screen) ID() screenhost.ID { return screenhost.TasksBoard }

// Column is the focused lane's index, DERIVED from which lane holds the grid's
// focus. An unfocused board — one that has not been entered or resynced yet —
// reads as its first lane, which is where the grid seeds itself.
func (s Screen) Column() int {
	if at := s.laneIndex(s.grid.Focus()); at >= 0 {
		return at
	}
	return 0
}

// Card is the focused lane's cursor, read out of the grid. Every lane keeps its
// own, so walking away from a lane and back returns to the card it was left on
// rather than to whatever index the previous lane happened to be at.
func (s Screen) Card() int {
	bucket, ok := s.focusedBucket()
	if !ok {
		return 0
	}
	return max(0, s.grid.Layout().Cursor(laneID(bucket)))
}

// laneIndex is the position of a lane id among the buckets, or -1.
func (s Screen) laneIndex(id screenlayout.ID) int {
	if id == "" {
		return -1
	}
	for i, bucket := range s.projection.Buckets() {
		if laneID(bucket.Key) == id {
			return i
		}
	}
	return -1
}

func (s Screen) MoveMode() bool        { return s.moveMode }
func (s Screen) BlocksHostInput() bool { return s.moveMode }
func (s Screen) OwnsFooter() bool      { return true }

func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "enter", "n", "e", "m", "esc", "left", "h", "right", "l", "up", "k", "down", "j", "pgup", "ctrl+u", "pgdown", "ctrl+d", "home", "g", "end", "G":
		return true
	default:
		return false
	}
}

func (s Screen) CancelMove() Screen {
	s.moveMode = false
	return s
}

// Bind takes the host's snapshot. The projection is rebuilt only when the
// snapshot actually changed, and the grid is resynced only then — the host
// re-binds before EVERY message, so a resync here unconditionally would make
// every keystroke pay for a full re-measure of every lane. A snapshot that did
// not move leaves the stored cursors alone, which is what they already are:
// clamped, by the resync that last saw a change.
func (s Screen) Bind(deps Deps, frame screenhost.Frame) Screen {
	projection := deps.Projection
	if projection.Fingerprint() == 0 {
		projection = taskprojection.Build(taskprojection.Input{
			Tasks: deps.Tasks, Workflow: deps.Workflow, Dependencies: deps.Dependencies,
			Comments: deps.Comments, Priorities: deps.Priorities})
	}
	key := snapshotKey(projection, deps.View.Filter.Priority)
	s.deps = deps
	s.projection = projection
	s = s.seedFocus()
	if key != s.dataKey || s.tasksByBucket == nil {
		s.dataKey = key
		s.tasksByBucket = projection.RootTasksByBucket(taskprojection.Query{Priority: deps.View.Filter.Priority})
		s = s.resync(frame.Kit())
	}
	return s
}

// seedFocus parks the focus on the first lane while nothing holds it, so
// Column, Card and the carousel window all have a lane to be about before the
// first keystroke arrives.
func (s Screen) seedFocus() Screen {
	if s.laneIndex(s.grid.Focus()) >= 0 || len(s.projection.Buckets()) == 0 {
		return s
	}
	s.grid = s.grid.WithFocus(laneID(s.projection.Buckets()[0].Key))
	return s
}

// resync re-measures every lane and writes back the clamped cursors and
// offsets. It is the call a refresh and a resize owe the grid; it is
// deliberately NOT on the keystroke path, where HandleKey settles the one pair
// it moved out of the frame it already resolved.
func (s Screen) resync(kit screenkit.Kit) Screen {
	if len(s.projection.Buckets()) == 0 {
		return s
	}
	s.grid = s.grid.Resync(kit, s.bodyBox(kit), s.root(kit))
	return s
}

func (s Screen) SelectedTask() (domain.Task, bool) {
	tasks := s.currentTasks()
	card := s.Card()
	if card < 0 || card >= len(tasks) {
		return domain.Task{}, false
	}
	return tasks[card], true
}

func (s Screen) SelectedTaskID() (int64, bool) {
	task, ok := s.SelectedTask()
	return task.ID, ok
}

// SelectTaskID moves the selection onto a task wherever it lives.
//
// The cursor goes in through WithCursor and not WithLayout: the two write the
// same number, and only the first keeps the arrangement metadata the last
// composition recorded. Nothing about a cursor invalidates that metadata, so
// spelling the seed the other way would make the next keystroke re-render the
// whole body to rediscover a frame that had not changed.
func (s Screen) SelectTaskID(taskID int64, frame screenhost.Frame) Screen {
	for _, bucket := range s.projection.Buckets() {
		for card, task := range s.tasksByBucket[bucket.Key] {
			if task.ID != taskID {
				continue
			}
			s.grid = s.grid.WithFocus(laneID(bucket.Key)).WithCursor(laneID(bucket.Key), card)
			return s.resync(frame.Kit())
		}
	}
	return s
}

// Update answers the keys the BOARD owns and routes everything else to the
// grid.
//
// Opening, creating, editing and arming a move are screen actions with no
// geometry in them; every motion — `h` `l` across the lanes, `j` `k` `pgup`
// `pgdn` `g` `G` inside one — is the grid's, replayed through the arranger that
// placed the focused lane this frame. The board no longer has a moveColumn, a
// moveCard or a scroll window to step, and cannot disagree with the layout about
// where the cursor now is.
func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	if outcome, handled := s.handleTaskKey(frame, key); handled {
		return outcome
	}
	if outcome, handled := s.handleMoveKey(frame, key); handled {
		return outcome
	}
	if key.String() == "r" {
		return screenhost.Reload(s, nil)
	}
	return s.routeGrid(frame, key)
}

func (s Screen) handleTaskKey(frame screenhost.Frame, key tea.KeyMsg) (screenhost.Outcome, bool) {
	switch key.String() {
	case "enter":
		if task, found := s.SelectedTask(); found {
			s.moveMode = false
			return screenhost.OpenTask(s, task.ID, nil), true
		}
	case "n":
		s.moveMode = false
		return screenhost.CreateTask(s, nil), true
	case "e":
		if task, found := s.SelectedTask(); found {
			s.moveMode = false
			return screenhost.EditTask(s, task.ID, nil), true
		}
	case "m":
		if _, found := s.SelectedTask(); found {
			s.moveMode = !s.moveMode
			status := frame.Text("tui.status.move_cancelled")
			if s.moveMode {
				status = frame.Text("tui.status.move_mode_active")
			}
			return screenhost.SetStatus(s, status, nil), true
		}
	case "esc":
		if s.moveMode {
			s.moveMode = false
			return screenhost.SetStatus(s, "", nil), true
		}
	}
	return screenhost.Outcome{}, false
}

func (s Screen) handleMoveKey(frame screenhost.Frame, key tea.KeyMsg) (screenhost.Outcome, bool) {
	if !s.moveMode {
		return screenhost.Outcome{}, false
	}
	switch key.String() {
	case "left", "h":
		return s.moveOutcome(frame, s.Column()-1), true
	case "right", "l":
		return s.moveOutcome(frame, s.Column()+1), true
	}
	return screenhost.Outcome{}, false
}

func (s Screen) routeGrid(frame screenhost.Frame, key tea.KeyMsg) screenhost.Outcome {
	s.grid = s.route(frame.Kit(), key.String())
	return screenhost.Stay(s, nil)
}

// laneSteps is the horizontal vocabulary the board WRAPS on, spelled here
// because the grid clamps instead.
var laneSteps = map[string]int{"left": -1, "h": -1, "right": 1, "l": 1}

// route hands one keystroke to the grid, and turns the carousel's clamp back
// into the board's wrap.
//
// The grid stops at the edge and reports the key unhandled — the right answer
// for a body whose columns are zones, where walking off the end of a form would
// be a surprise. A board is a ring: `l` on the last lane has always landed on
// the first, and the shortest honest way to keep that is to notice the refusal
// and point the focus at the opposite lane. The window follows the focus, so
// nothing else has to move.
func (s Screen) route(kit screenkit.Kit, key string) screengrid.State {
	if len(s.projection.Buckets()) == 0 {
		return s.grid
	}
	next, handled := s.grid.HandleKey(kit, s.bodyBox(kit), key, s.root(kit))
	step, lane := laneSteps[key]
	if handled || !lane {
		return next
	}
	n := len(s.projection.Buckets())
	return next.WithFocus(laneID(s.projection.Buckets()[(s.Column()+step+n)%n].Key))
}

func (s Screen) moveOutcome(frame screenhost.Frame, target int) screenhost.Outcome {
	if target < 0 || target >= len(s.projection.Buckets()) {
		return screenhost.SetStatus(s, frame.Text("tui.status.no_target_column"), nil)
	}
	task, ok := s.SelectedTask()
	if !ok {
		return screenhost.SetStatus(s, frame.Text("tui.status.no_selected_task"), nil)
	}
	s.grid = s.grid.WithFocus(laneID(s.projection.Buckets()[target].Key))
	s.moveMode = false
	return screenhost.MoveTaskToBucket(s, task.ID, s.projection.Buckets()[target].Key, nil)
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleLeave || event == screenhost.LifecycleBlur {
		s.moveMode = false
	}
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.seedFocus().resync(frame.Kit())
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if s.moveMode {
		return []screenhost.FooterBinding{
			{Key: "left/right", Label: frame.Text("tui.footer.move_task_to_lane"), Primary: true},
			frame.FooterCancel(false),
			{Key: "q", Label: frame.Text("tui.footer.quit")},
		}
	}
	return []screenhost.FooterBinding{
		frame.FooterOpen(true),
		frame.FooterNew(true),
		frame.FooterMoveTask(true),
		{Key: "left/right", Label: frame.Text("tui.footer.lanes")},
		{Key: "up/down", Label: frame.Text("tui.footer.tasks")},
		frame.FooterScrollPage(false),
		frame.FooterEdit(false),
		{Key: keynav.Default.Tops.Primary(), Label: frame.Text("tui.footer.tabs")},
		{Key: keynav.Default.Zones.Primary(), Label: frame.Text("tui.footer.zones")},
		frame.FooterSubNav(false),
		{Key: "ctrl+p", Label: frame.Text("tui.footer.project")},
		frame.FooterHistoryBack(false),
		frame.FooterHelp(false),
	}
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID: "tasks_board", Title: frame.Text("tui.help.tasks_board.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "← ↑ ↓ → · h j k l", Description: frame.Text("tui.help.tasks_board.navigate")},
			{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.tasks_board.scroll_column")},
			{Key: "g · G", Description: frame.Text("tui.help.tasks_board.first_last_card")},
			{Key: "enter", Description: frame.Text("tui.help.tasks_board.open_task")},
			{Key: "n", Description: frame.Text("tui.help.tasks_board.new_task")},
			{Key: "e", Description: frame.Text("tui.help.tasks_board.edit_task")},
			{Key: "c", Description: frame.Text("tui.help.tasks_board.add_comment")},
			{Key: "m", Description: frame.Text("tui.help.tasks_board.move_task")},
			{Key: "A", Description: frame.Text("tui.help.tasks_board.toggle_archived")},
		},
	}}
}

// columnHeader is the lane's kicker row — `▸ BUCKET · N` when focused, `// BUCKET · N` otherwise. Shared by the
// renderer and the chrome tally so a two-line header could never be introduced
// without the budget paying for it.
func (s Screen) columnHeader(kit screenkit.Kit, bucket domain.Bucket, focused bool) string {
	name := screenkit.Sanitize(bucket.Name)
	if name == "" {
		name = screenkit.Sanitize(bucket.Key)
	}
	return kit.Styles.SectionKickerCount(name, len(s.tasksByBucket[bucket.Key]), focused)
}

// CursorVisible reports whether the selected card is inside its lane's own
// window, and whether that lane was PAINTED at all.
//
// It asks the grid where the lane was placed and then asks the ARRANGER what it
// windowed inside that placement, rather than restating either. A property test
// that re-derived the window would agree with any bug in it.
//
// The second return is why there are two. A lane declares a floor of
// [laneMinRows], and a body shorter than that affords no lane — the board paints
// its carousel hint and nothing else. There is then no window for the cursor to
// be inside or outside of, and answering "visible" would be a lie the old
// implementation told: it measured a viewport of zero, got an empty range back
// and reported the cursor visible in a lane it had just clipped away. Saying so
// separately keeps the invariant sharp AND keeps a caller able to tell a board
// that hid the cursor from a terminal that could not draw a board.
func (s Screen) CursorVisible(frame screenhost.Frame) (visible, painted bool) {
	if len(s.currentTasks()) == 0 {
		return true, true
	}
	bucket, ok := s.focusedBucket()
	if !ok {
		return false, false
	}
	kit := frame.Kit()
	placed, ok := screengrid.Render(kit, s.grid, s.bodyBox(kit), s.root(kit)).Placement(laneID(bucket))
	if !ok || placed.Dropped {
		return false, false
	}
	l := s.computeLayout(screenlayout.HostBox(kit).Width, len(s.projection.Buckets()))
	section := s.laneSection(kit, s.projection.Buckets()[s.Column()], true, l, themeOf(kit))
	window, ok := screenlayout.ArrangeIn(kit, placed.Box, s.grid.Layout(), section).Placement(laneID(bucket))
	if !ok || window.Dropped {
		return false, false
	}
	if window.Cursor < 0 || window.Last < window.First {
		return false, false
	}
	return window.Cursor >= window.First && window.Cursor <= window.Last, true
}

func (s Screen) View(frame screenhost.Frame) string {
	return s.render(frame.Kit())
}

// render is the whole body: the grid's view, plus the empty-state chrome the
// box was already shortened for. Screengrid owns the leading blank and the clip;
// Indent matches the historical two-space gutter around the lane carousel.
func (s Screen) render(kit screenkit.Kit) string {
	if len(s.projection.Buckets()) == 0 {
		return kit.Panel(kit.T("tui.empty.board_no_buckets"))
	}
	body := screengrid.Render(kit, s.grid, s.bodyBox(kit), s.root(kit)).View
	if chrome := s.emptyChrome(kit); len(chrome) > 0 {
		if body != "" {
			body += "\n"
		}
		body += strings.Join(chrome, "\n")
	}
	return screenkit.Indent("\n"+body, 2)
}

func (s Screen) cardSpec(kit screenkit.Kit, task domain.Task, selected bool, l layout) card.Spec {
	return card.Spec{
		ID: task.ID, Title: task.Title,
		Badges:   s.cardBadges(kit, task),
		Selected: selected, Cursor: selected,
		Archived: task.State == domain.TaskStateArchived,
		BoxWidth: l.cardWidth, InnerWidth: l.cardContent,
	}
}

// cardBadges is the lane card's badge line: the priority pill, then the blocker,
// comment and sub-task counts. The counts were already the board's — only the
// painting crossed back into the host.
func (s Screen) cardBadges(kit screenkit.Kit, task domain.Task) []string {
	badges := make([]string, 0, 4)
	if def, ok := s.projection.Priority(task.Priority); ok {
		if pill := tokenstrip.Pill(kit.Styles, def.Color, def.Value); pill != "" {
			badges = append(badges, pill)
		}
	}
	counts := s.projection.Badges(task.ID)
	return append(badges, tokenstrip.Counts{
		Blockers: counts.Blockers,
		Comments: counts.Comments,
		Subtasks: counts.Subtasks,
	}.Render(kit.Styles, kit.T)...)
}

func (s Screen) emptyHint(kit screenkit.Kit) string {
	lines := []string{
		kit.Styles.HintAccent.Render(kit.T("tui.empty.board_no_tasks")), "",
		kit.Styles.Hint.Render(kit.T("tui.board.empty_press_n")) + kit.Styles.HintAccent.Render("n") + kit.Styles.Hint.Render(kit.T("tui.board.empty_to_create")),
		kit.Styles.Hint.Render(kit.T("tui.board.empty_cli_example")), "",
		kit.Styles.HintAccent.Render("m") + kit.Styles.Hint.Render(kit.T("tui.board.empty_inline_move")) + kit.Styles.HintAccent.Render("enter") + kit.Styles.Hint.Render(kit.T("tui.board.empty_inline_open")) + kit.Styles.HintAccent.Render("c") + kit.Styles.Hint.Render(kit.T("tui.board.empty_inline_comment")),
	}
	return kit.Styles.HintBox.Width(clamp(kit.AvailableWidth()-8, 32, 60)).Render(strings.Join(lines, "\n"))
}

func (s Screen) focusedBucket() (string, bool) {
	at := s.Column()
	if at < 0 || at >= len(s.projection.Buckets()) {
		return "", false
	}
	return s.projection.Buckets()[at].Key, true
}

func (s Screen) currentTasks() []domain.Task {
	bucket, ok := s.focusedBucket()
	if !ok {
		return nil
	}
	return s.tasksByBucket[bucket]
}

func (s Screen) taskCount() int {
	total := 0
	for _, tasks := range s.tasksByBucket {
		total += len(tasks)
	}
	return total
}

func snapshotKey(projection taskprojection.Projection, priorities []string) uint64 {
	h := fnv.New64a()
	write := func(value string) { _, _ = h.Write([]byte(value)); _, _ = h.Write([]byte{0}) }
	write(strconv.FormatUint(projection.Fingerprint(), 10))
	for _, priority := range priorities {
		write(priority)
	}
	return h.Sum64()
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

var _ screenhost.Screen = Screen{}
