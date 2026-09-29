// Package taskdetail owns the Task Detail screen and its local interaction
// models. Project services and cross-screen navigation remain host-owned and
// are requested through typed screenhost outcomes.
package taskdetail

import (
	"sort"
	"strings"

	bubblekey "github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/keynav"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// The three zones this screen declares to the arranger. The details block and
// the subtask board stack inside ONE column beside the feed, which is the
// arrangement joinSections used to compose by hand as
// `JoinHorizontal(JoinVertical(form, subtasks), "  ", activity)`.
const (
	sectionDetails  = screenlayout.ID("details")
	sectionSubtasks = screenlayout.ID("subtasks")
	sectionActivity = screenlayout.ID("activity")
	groupLeft       = screenlayout.ID("left-column")
	sectionBlockers = screenlayout.ID("blockers")
	sectionState    = screenlayout.ID("task-detail-state")
	sectionMove     = screenlayout.ID("task-detail-move")
)

// The row floors stay here. Width measures live in screenkit and arrive
// through [screenlayout.BesideFeed] and [screenlayout.Feed].
//
// # Drop order is a product decision
//
// sections() declares activity LAST on purpose. MinRows is a hard floor: when
// the stacked floors do not fit the HostBox, the arranger drops from the bottom
// up. Details and subtasks are the subject of this screen — the task and its
// children — so they keep their floors; the activity feed is supplemental and
// is the zone that yields. Do not reorder the declarations to "fix" a drop
// without rewriting this decision.
//
// Measured at the 80×24 golden floor with DefaultChromeRows=7: BodyRows=15,
// HostBox=14; details(6)+subtasks(8)+activity(4)=18, so the stacked body
// cannot afford every floor and the focused zone fills the box. The
// subtask floor includes its four-row outer frame plus the lane's own top and
// bottom edges, keeping the shortest rendered board closed.
//
// detailsWeight fills toward the description-preview ceiling before subtasks
// take leftover rows — Studio's fields-over-detail split. Equal weights were
// the 50/50 split that clipped the preview beside an empty board. The ceiling
// is a formula (not a paint) so a keystroke still renders the details block
// once.
const (
	detailsMinRows    = 6
	detailsWeight     = 8
	detailsMetaFields = 5
	subtasksMinRows   = 8
	feedMinRows       = 4
)

type Focus uint8

const (
	FocusDetails Focus = iota
	FocusSubtasks
	FocusActivity
)

type Mode uint8

const (
	ModeNormal Mode = iota
	ModeBlockers
	ModeComment
	ModeMove
)

type Payload struct {
	Generation   uint64
	Task         domain.Task
	Projection   taskprojection.Projection
	Tasks        []domain.Task
	Workflow     domain.Workflow
	Dependencies []domain.TaskDependency
	Activity     []domain.Event
	Stack        []int64
}

type Result struct {
	Generation     uint64
	TaskID         int64
	Task           domain.Task
	TaskValid      bool
	Tasks          []domain.Task
	TasksValid     bool
	Dependencies   []domain.TaskDependency
	BlockersValid  bool
	Activity       []domain.Event
	ActivityValid  bool
	PreserveAnchor bool
}

type State struct {
	Width, Height  int
	Focus          Focus
	Mode           Mode
	ActivityCursor int
	ActivityScroll int
	SubtaskCursor  int
	SubtaskScroll  int
	SubtaskColumn  int
	SubtaskOffset  int
	Stack          []int64
	Pending        bool
	DeleteArmed    bool
}

type Deps struct {
	// Priorities is the configured priority table. It replaced a PriorityLabel
	// resolver AND a TaskBadges painter, both of which called back into the root
	// package: the screen could describe a sub-task card but not paint one, so a
	// lane rendered from a fixture came out badgeless. With the table in hand the
	// screen resolves the label and the pill itself, and the host passes data.
	Priorities []config.PriorityDefinition
	TaskTags   func(int64) []domain.Tag
	MovePrompt func(domain.Task) string
}

type Screen struct {
	deps       Deps
	payload    Payload
	projection taskprojection.Projection
	focus      Focus
	mode       Mode
	width      int
	height     int
	frame      screenhost.Frame
	// grid is the canonical body navigator: the single source of truth for
	// body cursor, offset and focus. The arranger state underneath is reached
	// through grid.Layout(), never mirrored into a second field.
	grid        screengrid.State
	subtasks    list.Cards
	subtaskCol  int
	subtaskOff  int
	checks      map[int64]bool
	comment     textarea.Model
	move        textinput.Model
	moveTaskID  int64
	details     list.Viewport
	md          *markdown.Renderer
	body        *taskDetailBlockCache
	pending     bool
	deleteArmed bool

	// feed is the flattened activity feed the last syncActivity produced, kept
	// so the one mutation that changes nothing but which card wears the accent
	// border — the j/k cursor move — can repaint two cards instead of sixty.
	// Transient view state, deliberately absent from State: State carries the
	// selection the host restores, while this is rebuilt from the next sync.
	feed activityFeed
}

// activityFeed is the rendered activity cards, memoized.
//
// It used to carry the flattening — the feed lines and the card-to-line ranges
// — as ONE value alongside the cards, because the screen owned the window and
// had to translate a card index into a line index. The arranger owns both now:
// a Block's Items ARE the cards, one item is one cursor position however many
// rows it occupies, and there is no second coordinate space to keep in step.
// What is left worth memoizing is the expensive half, the card renders.
type activityFeed struct {
	ready bool
	key   activityFeedKey
	// focused is the card index the cards were rendered focused at, so a cursor
	// move can repaint exactly the two whose accent flipped.
	focused int
	cards   []string
}
type taskDetailBlockCache struct {
	details screenlayout.BlockMemo[taskDetailBlockKey]
	cards   screenlayout.BlockMemo[taskCardSetKey]
}

type taskCardSetKey struct {
	generation uint64
	bucket     string
	boxWidth   int
	innerWidth int
	theme      string
}

// taskDetailBlockKey identifies a memoised block. EVERY input the block's build
// closure reads has to be in here, and `rows` is never one of them — a block
// whose height depends on the row budget is not memoisable at all (P2 in
// docs/internal/tui-screen-assembly.md). Only the details block is keyed by
// this type; see subtasksBlock in render.go for why the board is not.
type taskDetailBlockKey struct {
	width                 int
	generation            uint64
	taskID                int64
	focus                 Focus
	mode                  Mode
	offset                int
	cursor, subtaskCursor int
	title, description    string
	bucket, priority      string
	deps                  int
	full                  bool
}

// activityFeedKey is the identity of every input a feed's CARDS are rendered
// from except which one is focused. Focus is carried separately, in
// activityFeed.focused, because moving it changes two cards' border colour and
// nothing else — that separation is the whole point of the memo.
//
// Compared with == and never dereferenced. A false negative costs one full
// re-render, which is exactly the behaviour this replaced; a false positive
// would paint a stale feed, so every input the cards read is witnessed:
//
//   - width and height, because commentCardWidth is derived from the kit's
//     available width and the geometry rides on the frame;
//   - mode, because ModeComment adds the composer under the feed;
//   - generation, taskID, the event count and the head pointer, because the
//     screen replaces payload.Activity WHOLESALE (Open, Apply and Replace all
//     copy into a fresh slice) rather than mutating events in place, and each
//     of those paths runs a full syncActivity anyway;
//   - theme, an ANSI-and-catalog fingerprint of every style and every string a
//     card paints with, so a theme swap or a language change is a miss.
type activityFeedKey struct {
	width      int
	height     int
	mode       Mode
	generation uint64
	taskID     int64
	events     int
	head       *domain.Event
	theme      string
}

func New() Screen {
	comment := textarea.New()
	comment.Prompt = ""
	comment.ShowLineNumbers = false
	comment.KeyMap.InsertNewline = bubblekey.NewBinding(bubblekey.WithKeys("alt+enter", "shift+enter", "ctrl+j"))
	move := textinput.New()
	move.Prompt = ""
	return Screen{grid: screengrid.NewState(), subtasks: list.NewCards(), comment: comment, move: move, details: list.NewViewport(), md: markdown.New(screenkit.MarkdownTokens{}), body: &taskDetailBlockCache{}}
}

func (s Screen) ID() screenhost.ID     { return screenhost.TaskDetail }
func (s Screen) Bind(deps Deps) Screen { s.deps = deps; return s }
func (s Screen) SelectedTaskID() (int64, bool) {
	return s.payload.Task.ID, s.payload.Task.ID > 0
}

func (s Screen) Payload() Payload {
	p := s.payload
	p.Tasks = append([]domain.Task(nil), p.Tasks...)
	p.Workflow.Buckets = append([]domain.Bucket(nil), p.Workflow.Buckets...)
	p.Dependencies = append([]domain.TaskDependency(nil), p.Dependencies...)
	p.Activity = append([]domain.Event(nil), p.Activity...)
	p.Stack = append([]int64(nil), p.Stack...)
	return p
}

func (s Screen) State() State {
	return State{
		Width: s.width, Height: s.height, Focus: s.focus, Mode: s.mode,
		ActivityCursor: s.activityCursor(), ActivityScroll: s.grid.Layout().Offset(sectionActivity),
		SubtaskCursor: s.subtasks.Cursor(), SubtaskScroll: s.subtasks.Scroll(),
		SubtaskColumn: s.subtaskCol, SubtaskOffset: s.subtaskOff,
		Stack: append([]int64(nil), s.payload.Stack...), Pending: s.pending,
		DeleteArmed: s.deleteArmed,
	}
}

func (s Screen) Activity() []domain.Event { return append([]domain.Event(nil), s.payload.Activity...) }
func (s Screen) Subtasks() list.Cards     { return s.subtasks }
func (s Screen) BlockerPicker() list.Picker {
	picker := list.NewPicker(list.Multi)
	picker = picker.WithCursor(s.grid.Layout().Cursor(sectionBlockers), len(s.blockerCandidates()), 0)
	picker = picker.WithScroll(s.grid.Layout().Offset(sectionBlockers))
	return picker
}

// activityCursor is the selected feed card, held by the arranger. It replaced
// the screen's own activityAt field.
func (s Screen) activityCursor() int { return s.grid.Layout().Cursor(sectionActivity) }

// withActivityCursorAt moves the activity zone's cursor on the grid arranger
// state, which is what View and key routing consume.
func (s Screen) withActivityCursorAt(index int) Screen {
	s.grid = s.grid.WithCursor(sectionActivity, clampCursor(index, len(s.payload.Activity)))
	return s
}

// withZoneFocus points BOTH the screen's zone and the arranger's key routing at
// the same place, so the zone that is accented is the zone the standard keys
// move. The subtask board still windows its own lanes through cardlist, but it
// is a real focus target: tab/f/esc park on it the way they park on details and
// activity.
func (s Screen) zoneID(focus Focus) screenlayout.ID {
	switch focus {
	case FocusActivity:
		return sectionActivity
	case FocusSubtasks:
		return sectionSubtasks
	default:
		return sectionDetails
	}
}

func (s Screen) withZoneFocus(focus Focus) Screen {
	s.focus = focus
	s.grid = s.grid.WithFocus(s.zoneID(focus))
	return s
}

// cycleZone walks details → activity → sub-tasks. The visual tree is still
// [details over subtasks] | activity; the keyboard order is the reading
// order of the two primary columns, then the board.
func (s Screen) cycleZone(step int) Screen {
	order := []Focus{FocusDetails, FocusActivity, FocusSubtasks}
	at := 0
	for i, zone := range order {
		if zone == s.focus {
			at = i
			break
		}
	}
	next := order[(at+len(order)+step)%len(order)]
	s = s.withZoneFocus(next)
	s.grid = s.grid.WithFullscreen(false)
	if next == FocusActivity && s.activityCursor() < 0 && len(s.payload.Activity) > 0 {
		s = s.withActivityCursorAt(0)
	}
	if next == FocusSubtasks && s.subtasks.Cursor() < 0 {
		s = s.syncSubtasks().firstSubtask()
	}
	return s.syncActivity()
}
func (s Screen) BlockerChecks() map[int64]bool {
	out := make(map[int64]bool, len(s.checks))
	for id, checked := range s.checks {
		out[id] = checked
	}
	return out
}
func (s Screen) CommentInput() textarea.Model        { return s.comment }
func (s Screen) MoveInput() textinput.Model          { return s.move }
func (s Screen) MoveTaskID() int64                   { return s.moveTaskID }
func (s Screen) Details() list.Viewport              { return s.details }
func (s Screen) FocusedSubtask() (domain.Task, bool) { return s.activeSubtask() }

func (s Screen) WithDetails(model list.Viewport) Screen { s.details = model; return s }

func (s Screen) WithSubtasks(model list.Cards) Screen { s.subtasks = model; return s }
func (s Screen) WithFocus(focus Focus) Screen {
	s = s.withZoneFocus(focus)
	s = s.syncChildren()
	if focus == FocusSubtasks && s.subtasks.Cursor() < 0 {
		s = s.firstSubtask()
	} else if focus == FocusActivity && s.activityCursor() < 0 && len(s.payload.Activity) > 0 {
		s = s.withActivityCursorAt(0).syncActivity()
	}
	return s
}
func (s Screen) FinishOperation() Screen {
	s.mode, s.pending, s.deleteArmed = ModeNormal, true, false
	s.moveTaskID = 0
	s.comment.Reset()
	s.comment.Blur()
	s.move.Reset()
	s.body = &taskDetailBlockCache{}
	s.move.Blur()
	return s.syncChildren()
}

func (s Screen) Replace(payload Payload) Screen {
	payload.Generation = s.payload.Generation
	payload.Stack = append([]int64(nil), s.payload.Stack...)
	if payload.Projection.Fingerprint() == 0 {
		payload.Projection = s.projection.Rebuild(payload.Tasks, payload.Dependencies)
	}
	s.payload = payload
	s.projection = payload.Projection
	return s.syncChildren()
}

func (s Screen) Open(payload Payload, frame screenhost.Frame) Screen {
	payload.Tasks = append([]domain.Task(nil), payload.Tasks...)
	payload.Dependencies = append([]domain.TaskDependency(nil), payload.Dependencies...)
	payload.Activity = append([]domain.Event(nil), payload.Activity...)
	payload.Stack = append([]int64(nil), payload.Stack...)
	if payload.Projection.Fingerprint() == 0 {
		payload.Projection = taskprojection.Build(taskprojection.Input{
			Tasks: payload.Tasks, Workflow: payload.Workflow, Dependencies: payload.Dependencies,
			Priorities: s.deps.Priorities})
	}
	s.payload = payload
	s.projection = payload.Projection
	s.grid, s.feed = screengrid.NewState(), activityFeed{}
	s.body = &taskDetailBlockCache{}
	s.subtasks = list.NewCards()
	s.subtaskCol, s.subtaskOff = 0, 0
	s.pending = true
	s = s.withZoneFocus(FocusDetails)
	s = s.resize(frame)
	return s.syncChildren()
}

func (s Screen) Apply(result Result) Screen {
	if !s.pending || result.Generation != s.payload.Generation || result.TaskID != s.payload.Task.ID {
		return s
	}
	anchor := s.FocusedActivityID()
	if result.TaskValid {
		s = s.applyTaskResult(result.Task)
	}
	if result.TasksValid {
		s.payload.Tasks = append([]domain.Task(nil), result.Tasks...)
	}
	if result.BlockersValid {
		s.payload.Dependencies = append([]domain.TaskDependency(nil), result.Dependencies...)
	}
	if result.ActivityValid {
		s.payload.Activity = append([]domain.Event(nil), result.Activity...)
		s = s.restoreActivityAnchor(anchor)
	}
	s.payload.Projection = s.projection.Rebuild(s.payload.Tasks, s.payload.Dependencies)
	s.projection = s.payload.Projection
	return s.syncChildren()
}

func (s Screen) applyTaskResult(task domain.Task) Screen {
	s.payload.Task = task
	for i, current := range s.payload.Tasks {
		if current.ID == task.ID {
			s.payload.Tasks[i] = task
			break
		}
	}
	return s
}

func (s Screen) restoreActivityAnchor(anchor int64) Screen {
	if anchor == 0 {
		return s
	}
	restored := -1
	for i, event := range s.payload.Activity {
		if event.ID == anchor {
			restored = i
			break
		}
	}
	return s.withActivityCursorAt(restored)
}

func (s Screen) CancelPending() Screen { s.pending = false; return s }

func (s Screen) WithActivityCursor(cursor int) Screen {
	return s.withActivityCursorAt(cursor).syncActivity()
}

func (s Screen) FocusedActivityID() int64 {
	at := s.activityCursor()
	if at < 0 || at >= len(s.payload.Activity) {
		return 0
	}
	return s.payload.Activity[at].ID
}

func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	s = s.resize(frame)
	if key.String() == "ctrl+c" {
		return screenhost.Quit(s, nil)
	}
	if s.mode != ModeNormal {
		return s.updateMode(key)
	}
	if key.String() != "d" {
		s.deleteArmed = false
	}
	return s.updateNormal(frame, key)
}

func (s Screen) updateNormal(frame screenhost.Frame, key tea.KeyMsg) screenhost.Outcome {
	// Screen-owned actions first, then the grid. tab/f/esc are the grid's
	// zone ring, section fullscreen and leave — the same keys Studio uses —
	// so they must not be claimed here before the focused section can take them.
	if outcome, handled := s.updateZoneKey(frame, key); handled {
		return outcome
	}
	return s.updateTaskKey(frame, key)
}

// updateZoneKey decides the grid's own keys: the zone ring (tab), a
// section's fullscreen toggle (f) and leaving the screen (esc). It reports
// whether the key was its to claim, so updateNormal can fall through to
// screen-owned actions otherwise.
func (s Screen) updateZoneKey(frame screenhost.Frame, key tea.KeyMsg) (screenhost.Outcome, bool) {
	switch key.String() {
	case "esc":
		if next, handled := s.handleGridKey(frame, key.String()); handled {
			return screenhost.Stay(next, nil), true
		}
		closed := screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionCloseTaskDetail, TaskID: s.payload.Task.ID, Generation: s.payload.Generation}}
		return closed, true
	case "tab":
		return screenhost.Stay(s.cycleZone(1), nil), true
	case "f":
		if next, handled := s.handleGridKey(frame, key.String()); handled {
			return screenhost.Stay(next, nil), true
		}
		return screenhost.Stay(s, nil), true
	default:
		return screenhost.Outcome{}, false
	}
}

// updateTaskKey decides the task-level keys: comments, move, activity
// paging, task mutations, subtask actions, opening the focused item and
// plain item navigation. Called once the grid has passed on the key.
func (s Screen) updateTaskKey(frame screenhost.Frame, key tea.KeyMsg) screenhost.Outcome {
	switch key.String() {
	case "b":
		return screenhost.Stay(s.openBlockers(), nil)
	case "c":
		return s.startCommentMode()
	case "m":
		return s.startMoveMode()
	case "J":
		return screenhost.Stay(s.withActivityCursorAt(s.activityCursor()+1).syncActivity(), nil)
	case "K":
		return s.stepActivity(-1)
	case "r":
		return s.refreshTask()
	case "e":
		return screenhost.EditTask(s, s.payload.Task.ID, nil)
	case "d":
		return s.armDelete()
	case "x":
		return s.archiveAction()
	case "s":
		return s.focusSubtasks(frame)
	case " ":
		return s.completeFocusedSubtask()
	case "M", "a", "n":
		return s.subtaskAction(key.String())
	case "enter":
		return s.openFocusedItem()
	default:
		return screenhost.Stay(s.navigate(key.String()), nil)
	}
}

// refreshTask arms the pending indicator and requests a reload of the task
// this screen is showing.
func (s Screen) refreshTask() screenhost.Outcome {
	s.pending = true
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionRefreshTaskDetail, TaskID: s.payload.Task.ID, Generation: s.payload.Generation}}
}

// armDelete requests the delete-confirmation flow when the details zone is
// focused; delete only ever applies to the task itself, never a subtask or
// an activity entry.
func (s Screen) armDelete() screenhost.Outcome {
	if s.focus != FocusDetails {
		return screenhost.Stay(s, nil)
	}
	s.deleteArmed = true
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionDeleteTaskFromDetail, TaskID: s.payload.Task.ID, Generation: s.payload.Generation}}
}

func (s Screen) subtaskAction(key string) screenhost.Outcome {
	kind := screenhost.ActionToggleTaskMarkdown
	if key != "M" {
		kind = screenhost.ActionCreateSubtask
	}
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: kind, TaskID: s.payload.Task.ID, Generation: s.payload.Generation}}
}

func (s Screen) startCommentMode() screenhost.Outcome {
	s.mode = ModeComment
	s.comment.Reset()
	s.comment.Focus()
	return screenhost.Stay(s, nil)
}

func (s Screen) stepActivity(step int) screenhost.Outcome {
	at := s.activityCursor()
	if at < 0 {
		at = len(s.payload.Activity)
	}
	return screenhost.Stay(s.withActivityCursorAt(at+step).syncActivity(), nil)
}

func (s Screen) startMoveMode() screenhost.Outcome {
	s.mode = ModeMove
	s.moveTaskID = s.payload.Task.ID
	if s.focus == FocusSubtasks {
		if child, ok := s.activeSubtask(); ok {
			s.moveTaskID = child.ID
		}
	}
	s.move.Reset()
	s.move.Focus()
	return screenhost.Stay(s, nil)
}

func (s Screen) archiveAction() screenhost.Outcome {
	if s.focus != FocusDetails {
		return screenhost.Stay(s, nil)
	}
	if s.payload.Task.State == domain.TaskStateArchived {
		return screenhost.UnarchiveTaskFromDetail(s, s.payload.Task.ID, s.payload.Generation)
	}
	return screenhost.ArchiveTaskFromDetail(s, s.payload.Task.ID, s.payload.Generation)
}

func (s Screen) focusSubtasks(frame screenhost.Frame) screenhost.Outcome {
	if len(s.children()) == 0 {
		return screenhost.SetStatus(s, frame.Text("tui.status.no_subtasks_to_focus"), nil)
	}
	s = s.withZoneFocus(FocusSubtasks)
	s = s.syncSubtasks().firstSubtask()
	return screenhost.Stay(s, nil)
}

func (s Screen) completeFocusedSubtask() screenhost.Outcome {
	if s.focus != FocusSubtasks {
		return screenhost.Stay(s, nil)
	}
	child, ok := s.activeSubtask()
	if !ok {
		return screenhost.Stay(s, nil)
	}
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionCompleteSubtask, TaskID: child.ID, Generation: s.payload.Generation}}
}

func (s Screen) openFocusedItem() screenhost.Outcome {
	if s.focus == FocusSubtasks {
		if child, ok := s.activeSubtask(); ok {
			return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionOpenNestedTask, TaskID: child.ID, Generation: s.payload.Generation}}
		}
	}
	if s.focus == FocusActivity && s.FocusedActivityID() != 0 {
		return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionOpenTaskComment, TaskID: s.payload.Task.ID, CommentID: s.FocusedActivityID(), Generation: s.payload.Generation}}
	}
	if s.focus == FocusDetails {
		return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionOpenTaskDescription, TaskID: s.payload.Task.ID, Generation: s.payload.Generation}}
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) updateMode(key tea.KeyMsg) screenhost.Outcome {
	if key.String() == "esc" {
		s.mode = ModeNormal
		s.grid = s.grid.WithFocus(sectionDetails)
		s.comment.Blur()
		s.move.Blur()
		return screenhost.Stay(s, nil)
	}
	switch s.mode {
	case ModeBlockers:
		return s.updateBlockers(key)
	case ModeComment:
		return s.updateComment(key)
	case ModeMove:
		return s.updateMove(key)
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) updateBlockers(key tea.KeyMsg) screenhost.Outcome {
	switch key.String() {
	case " ", "space":
		candidates := s.blockerCandidates()
		if cursor := s.grid.Layout().Cursor(sectionBlockers); cursor >= 0 && cursor < len(candidates) {
			s.checks[candidates[cursor].ID] = !s.checks[candidates[cursor].ID]
		}
	case "ctrl+s":
		return s.blockerOutcome(nil)
	default:
		if next, handled := s.grid.HandleKey(s.frame.Kit(), screenlayout.HostBox(s.frame.Kit()), key.String(), s.blockerRoot(s.frame)); handled {
			s.grid = next
		}
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) updateComment(key tea.KeyMsg) screenhost.Outcome {
	if key.String() == "enter" {
		body := strings.TrimSpace(s.comment.Value())
		if body != "" {
			return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionAddTaskComment, TaskID: s.payload.Task.ID, Value: body, Generation: s.payload.Generation}}
		}
		return screenhost.Stay(s, nil)
	}
	var cmd tea.Cmd
	s.comment, cmd = s.comment.Update(key)
	return screenhost.Stay(s, cmd)
}

func (s Screen) updateMove(key tea.KeyMsg) screenhost.Outcome {
	if key.String() == "enter" {
		bucket := strings.TrimSpace(s.move.Value())
		if bucket != "" {
			return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionMoveTaskFromDetail, TaskID: s.moveTaskID, BucketKey: bucket, Generation: s.payload.Generation}}
		}
		return screenhost.Stay(s, nil)
	}
	var cmd tea.Cmd
	s.move, cmd = s.move.Update(key)
	return screenhost.Stay(s, cmd)
}

func (s Screen) blockerOutcome(cmd tea.Cmd) screenhost.Outcome {
	ids := make([]int64, 0, len(s.checks))
	for id, checked := range s.checks {
		if checked {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return screenhost.Outcome{Screen: s, Action: screenhost.Action{Kind: screenhost.ActionSaveTaskBlockers, TaskID: s.payload.Task.ID, TaskIDs: ids, Generation: s.payload.Generation}, Command: cmd}
}

// navigate routes a motion key to the zone that holds focus.
//
// The details block and the feed are SECTIONS, so their motion is the
// arranger's: it owns the window, the offset and the cursor, and it clamps
// against the geometry it is about to paint. The subtask board is not — it
// windows its own lanes through cardlist and reads h/l for the carousel, which
// is a key vocabulary the standard table has no spelling for — so its keys stay
// here.
func (s Screen) sharingDetailsPreview() bool {
	if s.focus != FocusDetails || s.grid.Fullscreen() {
		return false
	}
	box := screenlayout.HostBox(s.bodyKit(s.frame))
	if screenlayout.FitsSideBySide(box, s.detailsSpec(), s.subtasksSpec(), s.activitySpec()) {
		return true
	}
	return box.Rows >= detailsMinRows+subtasksMinRows+feedMinRows
}

func (s Screen) navigate(key string) Screen {
	// Sharing details is a 5-line preview. j/k must not window it; the full
	// body is `f`, or stacked auto-fill when the zone floors do not fit.
	if s.sharingDetailsPreview() {
		switch key {
		case "j", "down", "k", "up", "pgdown", "pgup", "g", "home", "G", "end":
			return s
		}
	}
	// The board owns its lane and card cursor. Handle its vocabulary before the
	// generic grid so a standard section cursor cannot swallow a card move.
	if s.focus == FocusSubtasks {
		switch key {
		case "j", "down":
			s.subtasks = s.subtasks.MoveCursor(1)
			return s
		case "k", "up":
			s.subtasks = s.subtasks.MoveCursor(-1)
			return s
		case "h", "left":
			s.subtaskCol = clamp(s.subtaskCol-1, 0, max(0, len(s.subtaskLanes().Lanes)-1))
			return s.syncSubtasks().firstSubtask()
		case "l", "right":
			s.subtaskCol = clamp(s.subtaskCol+1, 0, max(0, len(s.subtaskLanes().Lanes)-1))
			return s.syncSubtasks().firstSubtask()
		case "g", "home":
			s.subtasks = s.subtasks.JumpFirst()
			return s
		case "G", "end":
			s.subtasks = s.subtasks.JumpLast()
			return s
		}
	}
	// Details and activity use the grid's section cursors and offsets.
	next, handled := s.handleGridKey(s.frame, key)
	if handled {
		return next
	}
	return s
}

// handleGridKey routes one keystroke through the same tree and box View paints.
func (s Screen) handleGridKey(frame screenhost.Frame, key string) (Screen, bool) {
	kit := s.bodyKit(frame)
	next, handled := s.grid.HandleKey(kit, screenlayout.HostBox(kit), key, s.bodyRoot(frame))
	if !handled {
		return s, false
	}
	return s.applyGrid(next), true
}

// applyGrid writes the grid state back and keeps the screen's zone enum on the
// same leaf the keyboard is parked on, so accent painting and screen-owned
// keys (enter, space, d) follow the section the user just tabbed into.
//
// A zone change has to Resync. HandleKey records the frame BEFORE it steps
// focus, and LifecycleEnter drops the feed memo; without a resync the next
// j/k misses both caches and recomposes every card. Cursor motion inside a
// zone only repaints the two cards whose accent flipped.
func (s Screen) applyGrid(next screengrid.State) Screen {
	prev := s.focus
	s.grid = next
	switch next.Focus() {
	case sectionSubtasks:
		s.focus = FocusSubtasks
	case sectionActivity:
		s.focus = FocusActivity
		if s.activityCursor() < 0 && len(s.payload.Activity) > 0 {
			s = s.withActivityCursorAt(0)
		}
	default:
		s.focus = FocusDetails
	}
	if s.focus != prev {
		s = s.syncActivity()
		if s.focus == FocusSubtasks && s.subtasks.Cursor() < 0 {
			s = s.syncSubtasks().firstSubtask()
		}
		return s
	}
	if s.focus == FocusActivity {
		s.feed = s.refocused(s.feed)
	}
	return s
}

// currentActivityFeed returns the feed the screen is currently showing, reusing
// the memo when it describes the present inputs and re-rendering from scratch
// when it does not. A miss costs exactly what an unmemoized render costs.
//
// It is the one thing #2447 built that this migration had to CARRY rather than
// discard. The arranger renders every section twice per keystroke — once to
// resolve the frame the key moves within, once to paint the frame after — so a
// feed of sixty markdown-ish cards re-rendered on demand would have doubled a
// cost #2447 had just cut by two thirds. With the memo a keystroke pays one
// full feed render at most, and a cursor move pays two cards.
func (s Screen) currentActivityFeed() activityFeed {
	key := s.activityFeedKey()
	if s.feed.ready && s.feed.key == key && len(s.feed.cards) == len(s.payload.Activity) {
		return s.feed
	}
	return s.renderActivityFeed(key)
}

// renderActivityFeed composes every card. This is the full render both
// syncActivity and a memo miss run.
func (s Screen) renderActivityFeed(key activityFeedKey) activityFeed {
	return activityFeed{ready: true, key: key, focused: s.activityCursor(), cards: s.activityCards(s.frame)}
}

// refocused repaints only the cards whose focused-ness flipped between the
// feed's render and the current cursor.
//
// It is safe here and nowhere else for the reason #2419 recorded on Studio: a
// memo that serves stale output on a mutation is worse than no memo, so only a
// mutation that provably leaves the cached inputs alone may consult it. Moving
// the cursor changes which of two cards carries the accent border and nothing
// else — every other input is in activityFeedKey.
func (s Screen) refocused(feed activityFeed) activityFeed {
	if !feed.ready || feed.focused == s.activityCursor() {
		return feed
	}
	cards := append([]string(nil), feed.cards...)
	for _, i := range [2]int{feed.focused, s.activityCursor()} {
		if i >= 0 && i < len(cards) {
			cards[i] = s.cardAt(i, i == s.activityCursor())
		}
	}
	feed.focused, feed.cards = s.activityCursor(), cards
	return feed
}

func (s Screen) activityFeedKey() activityFeedKey {
	key := activityFeedKey{
		width: s.activityCardWidth(s.frame), height: s.frame.Height(), mode: s.mode,
		generation: s.payload.Generation, taskID: s.payload.Task.ID,
		events: len(s.payload.Activity), theme: s.activityCardTheme(s.frame),
	}
	if len(s.payload.Activity) > 0 {
		key.head = &s.payload.Activity[0]
	}
	return key
}

func (s Screen) openBlockers() Screen {
	s.mode = ModeBlockers
	s.grid = s.grid.WithFocus(sectionBlockers).WithCursor(sectionBlockers, 0)
	s.checks = map[int64]bool{}
	for _, blocker := range s.projection.Blockers(s.payload.Task.ID) {
		s.checks[blocker.ID] = true
	}
	return s
}

func (s Screen) syncChildren() Screen { return s.syncActivity().syncSubtasks() }

func (s Screen) syncActivity() Screen {
	s.feed = s.renderActivityFeed(s.activityFeedKey())
	if s.height > 0 {
		kit := s.bodyKit(s.frame)
		s.grid = s.grid.Resync(kit, screenlayout.HostBox(kit), s.bodyRoot(s.frame))
		s.grid = s.grid.WithFocus(s.zoneID(s.focus))
	}
	return s
}

func (s Screen) syncSubtasks() Screen {
	children := s.childrenInColumn()
	items := make([]list.Item, len(children))
	for i, task := range children {
		items[i] = list.Item{Content: screenkit.Sanitize(task.Title), Height: 1}
	}
	s.subtasks = s.subtasks.WithItems(items).WithViewport(s.viewportRows())
	return s
}

func (s Screen) firstSubtask() Screen {
	if len(s.childrenInColumn()) == 0 {
		s.subtasks = s.subtasks.WithCursor(-1)
	} else {
		s.subtasks = s.subtasks.JumpFirst()
	}
	return s
}

func (s Screen) children() []domain.Task {
	return s.projection.Children(s.payload.Task.ID)
}

func (s Screen) subtaskLanes() taskprojection.ChildLaneSet {
	return s.projection.ChildLanes(s.payload.Task.ID)
}

func (s Screen) childrenInColumn() []domain.Task {
	lanes := s.subtaskLanes().Lanes
	column := clamp(s.subtaskCol, 0, len(lanes)-1)
	if column < 0 || column >= len(lanes) {
		return nil
	}
	return lanes[column].Tasks
}

func (s Screen) activeSubtask() (domain.Task, bool) {
	children := s.childrenInColumn()
	cursor := s.subtasks.Cursor()
	if cursor < 0 || cursor >= len(children) {
		return domain.Task{}, false
	}
	return children[cursor], true
}

func (s Screen) blockerCandidates() []domain.Task {
	return s.projection.Candidates(s.payload.Task.ID)
}

// viewportRows is the row budget for the panels that hang off the task detail —
// the subtask list's window and the blocker picker's cursor window.
// with it only while the host chrome was exactly seven rows and no status badge
// was showing; a status badge made it two rows too generous. Reading the kit
// removes the copy.
func (s Screen) viewportRows() int { return max(1, s.frame.Kit().Chrome().ViewportRows()) }
func (s Screen) resize(frame screenhost.Frame) Screen {
	s.frame = frame
	if s.width == frame.Width() && s.height == frame.Height() {
		return s
	}
	s.body = &taskDetailBlockCache{}
	s.width, s.height = frame.Width(), frame.Height()
	width := max(1, s.width-12)
	s.comment.SetWidth(width)
	s.comment.SetHeight(5)
	s.move.Width = width
	return s.syncChildren()
}

func (s Screen) View(frame screenhost.Frame) string {
	return s.render(frame)
}

func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	switch s.mode {
	case ModeBlockers:
		return []screenhost.FooterBinding{{Key: "space", Label: frame.Text("tui.footer.toggle_blocker"), Primary: true}, frame.FooterSave(true), frame.FooterMove(false), frame.FooterScrollPage(false), frame.FooterCancel(false)}
	case ModeComment:
		return []screenhost.FooterBinding{{Key: "enter", Label: frame.Text("tui.footer.save_comment"), Primary: true}, {Key: "alt+enter/shift+enter", Label: frame.Text("tui.footer.newline")}, frame.FooterCancel(false)}
	case ModeMove:
		return []screenhost.FooterBinding{{Key: "enter", Label: frame.Text("tui.footer.save"), Primary: true}, frame.FooterCancel(false), {Key: "ctrl+c", Label: frame.Text("tui.footer.quit")}}
	}
	back := frame.Text("tui.footer.back")
	if len(s.payload.Stack) > 0 {
		back = frame.Text("tui.footer.back_parent")
	}
	bindings := []screenhost.FooterBinding{
		frame.FooterEdit(true),
		{Key: "n", Label: frame.Text("tui.footer.sub_task"), Primary: true},
		{Key: "f", Label: frame.Text("tui.footer.focus"), Primary: true},
		{Key: "c", Label: frame.Text("tui.footer.comment"), Primary: true},
		frame.FooterMoveTask(true),
		{Key: keynav.Default.Zones.Primary(), Label: frame.Text("tui.footer.zone")},
		frame.FooterScroll(false),
		{Key: "b", Label: frame.Text("tui.footer.blockers")},
	}
	if s.focus == FocusDetails {
		label := frame.Text("tui.footer.arm_delete")
		if s.deleteArmed {
			label = frame.Text("tui.footer.confirm_delete")
		}
		bindings = append(bindings, screenhost.FooterBinding{Key: "d", Label: label, Primary: s.deleteArmed})
		archiveLabel := frame.Text("tui.footer.archive")
		if s.payload.Task.State == domain.TaskStateArchived {
			archiveLabel = frame.Text("tui.footer.unarchive")
		}
		bindings = append(bindings, screenhost.FooterBinding{Key: "x", Label: archiveLabel})
	}
	switch s.focus {
	case FocusActivity:
		bindings = append(bindings, screenhost.FooterBinding{Key: "enter", Label: frame.Text("tui.footer.open_comment_activity")})
	case FocusSubtasks:
		bindings = append(bindings, screenhost.FooterBinding{Key: "enter", Label: frame.Text("tui.footer.open_subtask")})
	case FocusDetails:
		bindings = append(bindings, screenhost.FooterBinding{Key: "enter", Label: frame.Text("tui.footer.focus_description")})
	}
	return append(bindings,
		frame.FooterRefresh(false),
		screenhost.FooterBinding{Key: "esc", Label: back},
		frame.FooterHelp(false),
	)
}

func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	if s.mode == ModeBlockers {
		return []screenhost.HelpGroup{{ID: "blocker_picker", Title: frame.Text("tui.help.blocker_picker.title"), Bindings: []screenhost.HelpBinding{
			{Key: "space", Description: frame.Text("tui.footer.toggle_blocker")},
			{Key: "ctrl+s", Description: frame.Text("tui.footer.save")},
			{Key: "up/down", Description: frame.Text("tui.footer.move")},
			{Key: "pgup/pgdn", Description: frame.Text("tui.footer.scroll")},
			{Key: "esc", Description: frame.Text("tui.footer.cancel")},
		}}}
	}
	return []screenhost.HelpGroup{{ID: "task_detail", Title: frame.Text("tui.help.task.title"), Bindings: []screenhost.HelpBinding{
		{Key: keynav.Default.Zones.Primary(), Description: frame.Text("tui.help.task.switch_focus")},
		{Key: "j/k", Description: frame.Text("tui.help.task.move")},
		{Key: "J/K", Description: frame.Text("tui.help.task.activity")},
		{Key: "a/n", Description: frame.Text("tui.footer.sub_task")},
		{Key: "b", Description: frame.Text("tui.footer.blockers")},
		{Key: "c", Description: frame.Text("tui.footer.comment")},
		{Key: "m", Description: frame.Text("tui.footer.move")},
		{Key: "d", Description: frame.Text("tui.footer.arm_delete")},
		{Key: "x", Description: frame.Text("tui.footer.archive")},
		{Key: "r", Description: frame.Text("tui.footer.refresh")},
		{Key: "esc", Description: frame.Text("tui.footer.back")},
	}}}
}

func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	// Every lifecycle event is a navigation or a geometry change, i.e. the only
	// windows in which the theme or the language can move while this screen is
	// not the one taking keys. Both are inputs to a card's bytes that no
	// == on the key would catch cheaply, so the memo is dropped and the next
	// keystroke pays one full feed render for it.
	s.feed = activityFeed{}
	if event == screenhost.LifecycleResize || event == screenhost.LifecycleEnter {
		s = s.resize(frame)
	}
	if event == screenhost.LifecycleLeave {
		s = s.CancelPending()
	}
	return screenhost.Stay(s, nil)
}

func (s Screen) OwnsKey(tea.KeyMsg) bool { return true }
func (s Screen) OwnsFooter() bool        { return true }
func (s Screen) BlocksHostInput() bool   { return s.mode != ModeNormal }

func clampCursor(value, count int) int {
	if count == 0 || value < 0 {
		return -1
	}
	return clamp(value, 0, count-1)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.FooterOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
var _ screenhost.TaskSelector = Screen{}
