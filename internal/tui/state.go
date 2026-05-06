package tui

import (
	"context"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/picker"
	"omakiten/internal/tui/components/viewport"
)

const (
	columnWidth                = 28
	taskDetailsPanelWidth      = 40
	taskCommentsPanelMinWidth  = 44
	taskCommentsPanelMaxWidth  = 96
	commentInputHeight         = 5
	taskFormInputWidth         = 72
	taskDescriptionInputHeight = 8
	metaRowLabelWidth          = 14
	selectionMarker            = "▌"
	normalMarker               = " "
	cardBoxWidth               = 26
	cardContentWidth           = 24 // cardBoxWidth - horizontal padding(2); text fits here
	// commentCard padding/border accounting: card Width = panel - 2 (gutter);
	// content (where text + tags wrap) = card.Width - 4 (padding 2 + border 2).
	commentCardChromeWidth = 4
	commentCardLineLimit   = 6
)

// Repositories is the dependency-injection bundle the TUI receives from its
// composition root. Each field is one of the application-layer ports defined
// in `internal/app/ports.go` — keeping these as interfaces means the TUI is
// testable with stubs and the production wire-up (sqlite-backed) can be
// swapped without touching this file.
type Repositories struct {
	Tasks        app.TaskRepository
	Comments     app.CommentRepository
	Dependencies app.DependencyRepository
	Entries      app.ContextEntryRepository
	Config       app.ConfigRepository
	Tags         app.TagRepository
	Editor       *app.BundleEditor
	ActivityLogs activity.ActivityLogRepository
	Events       app.EventRepository
}

// Model is the root Bubble Tea model for the TUI. It aggregates state that
// would otherwise be scattered across half a dozen sub-models — most pickers
// and viewports are now their own components (see internal/tui/components/),
// but the top-level orchestration state (which screen is open, which task is
// selected, the loaded data slices) lives here.
//
// The fields are grouped by concern; the comments inline document
// invariants and lifecycle. Sub-component fields are tagged so it's clear
// which struct owns the cursor/scroll for that surface.
type Model struct {
	ctx     context.Context
	project domain.ProjectContext
	repos   Repositories
	counter token.Counter
	theme   config.Theme
	styles  styles

	width    int
	height   int
	view     int
	mode     inputMode
	input    string
	status   string
	moveMode bool
	helpOpen bool
	helpAll  bool

	taskScreen      taskScreenMode
	taskID          int64
	taskTitle       string
	taskDescription string
	taskPriority    string
	taskField       taskFormField

	blockerPickerOpen   bool
	blockerPickerTaskID int64
	// blockerPicker owns cursor + scroll for the multi-select blocker
	// picker. Open/close lifecycle remains on the parent (an enum flag
	// drives whether the picker is the active view); state inside the
	// picker — what's highlighted, what's scrolled — lives in the
	// component itself.
	blockerPicker       picker.Model
	blockerPickerChecks map[int64]bool

	tasks               []domain.Task
	workflow            domain.Workflow
	dependencies        []domain.TaskDependency
	comments            []domain.Comment
	laws                []domain.Law
	skills              []domain.Skill
	personas            []domain.Persona
	templates           []config.TaskTemplate
	themePickerOptions  []themeOption
	configPickerOptions []configOption
	entries             []domain.ContextEntry
	tags                []domain.Tag
	taskTagsMap         map[int64][]domain.Tag
	metrics             domain.TokenMetrics
	selected            int
	colIdx              int
	cardIdx             int

	entityKind       entityKind
	entityCursors    map[entityKind]int
	entityScroll     map[entityKind]int
	entityKindScroll int
	entityScreen     entityScreenMode
	entityForm       entityForm
	deletePending    bool
	deleteKind       entityKind
	deleteSlug       string

	logs         []domain.ActivityLog
	logsSelected int

	// activity holds the unified activity feed (comments + system events)
	// for the currently-open task detail view. Populated on openTaskView
	// and cleared when the screen closes — refresh does not refetch this
	// because the rest of refresh() loads all-task data.
	activity        []domain.Event
	activityForTask int64
	activityScroll  int
	// activityCursor is the index into the visible activity feed; -1 means
	// "no card selected" and disables the focused-border styling. Card
	// navigation moves it; the scroll offset auto-follows.
	activityCursor int
	// taskFocus tracks which column inside the task detail screen owns
	// navigation keys. Default: form/details column. Tab toggles to the
	// activity column so j/k navigate cards instead of scrolling description.
	taskFocus taskScreenFocus

	// commentScreenOpen is the modal "comment detail" view layered on top of
	// taskScreenView. Long comments overflow the activity column, so Enter on
	// a focused comment opens this dedicated screen where the body can scroll
	// freely. Returning with esc preserves taskScreen + activityCursor so the
	// user lands back on the same card.
	commentScreenOpen bool
	commentScreenID   int64
	// commentScreen owns the scroll offset and grid build state for the
	// dedicated full-width comment view; opened via Enter on a focused
	// comment in the activity feed. The Model resets on each open via
	// detailscreen.New() so prior scroll state never leaks across
	// comments.
	commentScreen detailscreen.Model

	// views caches the resolved per-view sort/filter pulled from the active
	// bundle on each refresh. Render and query helpers read it instead of
	// the raw Settings so omitted fields show up as their canonical defaults.
	views config.ViewSettings

	// taskView owns the scroll offset for the form column on the task
	// detail screen. The activity column manages its own scroll via
	// activityScroll because it has separate semantics (line-based vs
	// card-cursor-based).
	taskView viewport.Model

	// boardColScroll is the leftmost-visible bucket index when the board is
	// too wide to fit all columns side-by-side. Updated via syncBoardColScroll
	// to keep colIdx inside the visible window.
	boardColScroll int

	// boardScroll holds a per-bucket scroll offset (in cards) so long columns
	// can be scrolled vertically without losing context when navigating between
	// lanes. Keys are bucket keys (domain.Bucket.Key).
	boardScroll map[string]int

	entityViewScroll int

	logsScroll int

	tableScroll int

	graphScroll int
	graphCursor int

	// help owns scroll state for the keybindings overlay; instantiated
	// once and reused (the overlay is closed/reopened, not destroyed,
	// so scroll state persists across toggles which feels right — users
	// often re-open help after a tangential keystroke).
	help viewport.Model

	// entityPicker owns cursor + scroll for the entity-screen pickers
	// (theme/config/template-default/persona). Reset to a fresh picker
	// of the appropriate Mode when each one opens — single-select for
	// theme/config/template-default, multi for persona.
	entityPicker picker.Model
}

// inputMode is the modal-input enum: normal navigation, comment input, or
// move input (typing a target bucket key). The mode flag drives whether the
// next keystroke is consumed by the inline text input or routed to the
// view-specific handler.
type inputMode int

const (
	modeNormal inputMode = iota
	modeComment
	modeMove
)

// taskScreenMode tracks the sub-surface of the task detail view stack:
// closed = no overlay; view = read-only details; create/edit = form mode.
// The picker/comment-screen overlays are tracked separately on flag fields.
type taskScreenMode int

const (
	taskScreenClosed taskScreenMode = iota
	taskScreenView
	taskScreenCreate
	taskScreenEdit
)

// taskScreenFocus is which column owns navigation keys inside taskScreenView.
// Tab toggles between them so the user can run j/k/enter on either side
// without a separate keybinding for each surface.
type taskScreenFocus int

const (
	taskFocusForm taskScreenFocus = iota
	taskFocusActivity
)

// taskFormField identifies which field of the create/edit form is focused.
// Tab cycles forward; the priority field has its own ←/→ cycle for the
// fixed enum (low/normal/high).
type taskFormField int

const (
	taskFieldTitle taskFormField = iota
	taskFieldDescription
	taskFieldPriority
)

var viewNames = []string{"BOARD", "TABLE", "GRAPH", "CONFIG", "LOGS"}

// refreshTickMsg drives the realtime refresh loop — emitted every second
// while the user is on a "live" view (board, table, etc.) and not editing.
// shouldRealtimeRefresh decides whether to honor each tick.
type refreshTickMsg struct{}
