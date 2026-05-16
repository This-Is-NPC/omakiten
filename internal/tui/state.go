package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"

	"omakiten/internal/activity"
	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/notification"
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
	Projects     app.ProjectRepository
	Workflow     *app.WorkflowService
	Comments     app.CommentRepository
	Dependencies app.DependencyRepository
	Entries      app.ContextEntryRepository
	Tags         app.TagRepository
	Editor       *app.BundleEditor
	BundleStore  app.BundleStore
	EntityFiles  app.EntityFileWriter
	Slugger      app.Slugifier
	ActivityLogs activity.ActivityLogRepository
	Events       app.EventRepository
	Metrics      *app.MetricsService
	Orphans      app.OrphanRepository

	// DispatchCommand invokes the root cobra command in-process and
	// returns the JSON envelope it wrote to stdout. Notification actions
	// rely on it to run CLI commands (e.g. "workflow orphans --confirm")
	// against the same runtime store the live TUI uses. nil disables the
	// action-dispatch path; pressing an action key with non-empty Command
	// becomes a no-op + status hint.
	DispatchCommand func(ctx context.Context, args []string) ([]byte, error)

	// Runtime metadata surfaced on Settings › General. The TUI itself
	// does not consume these for routing or persistence; they exist so
	// the read-only info card can reflect the active install. Empty
	// strings are tolerated and rendered as "—".
	ConfigPath   string
	DBPath       string
	Version      string
	RepoLocalDir string

	// Cache is the per-project BundleCache the runtime seeded at boot.
	// reloadBundle calls Cache.Reload to rotate the in-memory provider
	// snapshot — Phase 3e dropped the ConfigService.Import fallback so
	// hot-reload never reaches the SQL config-write path. Required for
	// any code path that triggers reloadBundle or reads
	// activeSnapshot/activePreviousSnapshot; Phase 2-bis dropped the
	// Repositories.Snapshot test-only escape hatch, so tests now wire
	// a real cache via testfixtures/runtimecache.Install instead of
	// plugging a snapshot pointer here directly.
	Cache *agentruntime.BundleCache
	// ProjectID is the cache key the runtime installed the active
	// ProjectRuntime under. reloadBundle passes it to Cache.Reload so
	// the rotated snapshot lands on the same key the rest of the model
	// is reading from.
	ProjectID int64
}

// activeSnapshot returns the per-project *config.Snapshot from the
// BundleCache entry the runtime installed at boot. TUI inline service
// constructions capture this pointer at the moment of dispatch; the
// cache rotates a fresh pointer on each rebuild, so subsequent calls
// see the new snapshot through the same accessor. Returns nil when the
// cache is not wired (rare test paths that bypass the runtime
// composition root).
func (r *Repositories) activeSnapshot() *config.Snapshot {
	if r.Cache == nil {
		return nil
	}
	pr := r.Cache.Get(r.ProjectID)
	if pr == nil {
		return nil
	}
	return pr.Snapshot
}

// activePreviousSnapshot returns the bundle view captured immediately
// before the latest cache rotation. Only the orphan-rebind flow reads
// it; nil when the cache has only seen one bundle for this project.
func (r *Repositories) activePreviousSnapshot() *config.Snapshot {
	if r.Cache == nil {
		return nil
	}
	pr := r.Cache.Get(r.ProjectID)
	if pr == nil {
		return nil
	}
	return pr.PreviousSnapshot
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
	ctx              context.Context
	project          domain.ProjectContext
	repos            Repositories
	counter          token.Counter
	theme            config.Theme
	styles           styles
	tokenBadgeYellow int
	tokenBadgeRed    int

	width  int
	height int
	top    topID
	sub    subID
	mode   inputMode
	// moveInput is the bubbles textinput powering modeMove (the modal
	// triggered by `m` followed by typing a target bucket key). Reset
	// on every beginInput call so prior values don't leak across moves.
	moveInput textinput.Model
	status    string
	moveMode  bool
	helpOpen bool
	helpAll  bool
	// viewHistory is the in-memory back-stack populated whenever the user
	// makes an intentional zone/sub navigation (tab / digit / `,`/`/`,
	// `0`, `ctrl+h`). Bound to a small cap so long sessions cannot grow
	// it unbounded; `ctrl+o` (vim-style "older") pops the most recent
	// entry. Refreshes and overlay close events do not touch this — the
	// stack is a record of *navigation*, not of every state change.
	viewHistory []navState

	taskScreen taskScreenMode
	taskID     int64
	// taskTitleInput / taskDescriptionInput own caret state and inline text
	// editing for the create/edit form. Replacing the prior `taskTitle` /
	// `taskDescription` strings with bubbles components fixed the "no
	// cursor / can only erase from end" UX bug — these models handle
	// arrow-key navigation, home/end, word-wise delete, paste, etc.
	taskTitleInput       textinput.Model
	taskDescriptionInput textarea.Model
	taskPriority         domain.Priority
	taskField            taskFormField
	// commentInput is reused by modeComment (add) and modeCommentEdit
	// (rewrite). Reset on every beginInput call so the placeholder and
	// pre-fill values reflect the active mode without leaking text across
	// flows.
	commentInput textarea.Model

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
	// priorities is the resolved id↔value↔color table the renderer
	// consults to draw priority badges and to drive the cycle in the
	// task form. Populated from the active bundle on each refresh so
	// edits to config.priorities take effect at the next view tick
	// without restarting the TUI.
	priorities []config.PriorityDefinition
	// severities mirrors priorities for law severities: id↔value↔color
	// table consulted by the entity-screen badge renderer. Same wire-up
	// path (NewModel parameter; refreshed at composition root).
	severities []config.SeverityDefinition
	// registry is the instance-scoped EnumRegistry built from priorities
	// and severities at NewModel time. Threaded into the app services the
	// TUI constructs on the fly (TaskService, TUIQueryService) so they
	// resolve labels/ids against this bundle instead of the deprecated
	// process-global tables.
	registry *domain.EnumRegistry
	themePickerOptions  []themeOption
	configPickerOptions []configOption
	entries             []domain.ContextEntry
	tags                []domain.Tag
	taskTagsMap         map[int64][]domain.Tag
	metrics             domain.TokenMetrics
	selected            int
	colIdx              int
	cardIdx             int

	entityKind    entityKind
	entityCursors map[entityKind]int
	entityScroll  map[entityKind]int
	entityScreen  entityScreenMode
	entityForm       entityForm
	deletePending    bool
	deleteKind       entityKind
	deleteSlug       string

	// Arm-then-confirm pending IDs for task and comment deletion. Non-zero
	// means a `d` press on the same item will fire the delete; any other
	// key clears the arm. The two are mutually exclusive — arming one
	// resets the other so the on-screen prompt always names a single
	// target. commentEditID stores which comment a modeCommentEdit input
	// is rewriting.
	taskDeletePendingID    int64
	commentDeletePendingID int64
	commentEditID          int64

	// home owns the multi-project picker shown when okt tui is launched
	// without a resolvable project. The picker component handles cursor/
	// scroll/keyboard navigation; the slices feed the row builder. Loaded
	// lazily on entry to viewHome so projects/tags don't pay the query cost
	// when the user opens the TUI inside a project.
	homeProjects       []domain.Project
	homeProjectTags    map[int64][]domain.Tag
	homeProjectPending map[int64]int
	homePicker         picker.Model
	// lastProjectRoot is the root_path of the last project the user opened
	// during the session. CLI-side cd-on-exit reads this after program.Run()
	// returns so the parent shell wrapper can `cd` into the project.
	lastProjectRoot string

	logs         []domain.ActivityLog
	logsSelected int
	// logsStats is the unbounded aggregate over the activity log scope
	// (project + view source filter). Populated alongside `logs` on
	// every refresh of the Stats › Logs view; the summary tables read
	// from this so the headline counts reflect the full project
	// history rather than just the limit-N rows currently materialised.
	logsStats domain.ActivityLogStats

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
	// commentScreenEditing flips the same overlay into a full-screen
	// edit form (kicker + textarea + footer) — the read-only detail
	// view is suppressed while editing. Cancel returns to the read
	// view; save runs CommentService.Edit and refreshes the comment.
	commentScreenEditing bool

	// views caches the resolved per-view sort/filter pulled from the active
	// bundle on each refresh. Render and query helpers read it instead of
	// the raw Settings so omitted fields show up as their canonical defaults.
	views config.ViewSettings

	// taskView owns the detail-grid builder + scroll offset for the form column
	// on the task detail screen. The activity column manages its own scroll via
	// activityScroll because it has separate semantics.
	taskView detailscreen.Model

	// boardColScroll is the leftmost-visible bucket index when the board is
	// too wide to fit all columns side-by-side. Updated via syncBoardColScroll
	// to keep colIdx inside the visible window.
	boardColScroll int

	// boardScroll holds a per-bucket scroll offset (in cards) so long columns
	// can be scrolled vertically without losing context when navigating between
	// lanes. Keys are bucket keys (domain.Bucket.Key).
	boardScroll map[string]int

	// entityView owns the detail-grid builder + scroll offset for the focused
	// entity detail screen.
	entityView detailscreen.Model

	logsScroll int

	tableScroll int

	graphScroll int
	graphCursor int

	// statsSummary caches the last-fetched metrics summary. statsPeriod
	// holds the active filter ("7d", "30d", "all"); refreshed on view entry
	// and on period change via ←/→.
	statsSummary domain.MetricsSummary
	statsPeriod  string

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

	// includeArchived flips the active-only task filter on every list view
	// (board/table/graph/logs). Default false (archived hidden); the `A`
	// keybind toggles it. Archived rows render with a dimmed style so the
	// user can still spot them when the toggle is on.
	includeArchived bool

	// markdown owns rendering of body fields (task description, comment
	// body, entity body) into ANSI-styled output. Rebuilt by reloadTheme
	// when the active theme changes so cached renders never serve stale
	// colors. markdownRendered is the session-only toggle bound to `M`.
	markdown         *markdownRenderer
	markdownRendered bool

	// notifications is the catalog of loaded notification cards keyed by
	// slug; the hooks engine names a slug per event and the parent
	// renders that notification as configured. notification is the live model while
	// one is on screen; nil otherwise.
	notifications map[string]config.Notification
	notification   *notification.Model

	// pendingSwapRevertPath stores the previous config yaml path when the
	// active swap produced orphaned tasks. The hooks engine paints an
	// orphan-migration notification overlay; if the user presses esc to
	// dismiss it without choosing migrate or skip, the TUI reverts the
	// swap by re-importing the previous bundle so the user is never left
	// with a config they did not commit to. Cleared whenever the user
	// makes an explicit choice (any ActionMsg) or after the revert runs.
	pendingSwapRevertPath string
	// suppressNextSwapEmit skips the bundle.swapped emit on the next
	// reloadBundle call. Used by revertConfigSwap so the revert hop does
	// not trigger another orphan-migration notification — the revert
	// itself is the user's cancel intent.
	suppressNextSwapEmit bool
}

// inputMode is the modal-input enum: normal navigation, comment-add input
// (embedded in the activity column), or move input (typing a target bucket
// key in the screen-level modal bar). Comment editing lives in its own
// full-screen overlay (`commentScreenEditing`), not in this enum.
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

// topID identifies a top-level navigation zone. Tops group surfaces by
// purpose: Tasks (data lenses over the work queue), Stats (observability),
// Settings (admin). topHome is the sentinel for the multi-project Home
// screen — it sits outside the regular cycle and is reachable only via
// empty-project startup or the dedicated ctrl+h binding.
type topID int

const topHome topID = -1

const (
	topTasks topID = iota
	topStats
	topSettings
)

// subID identifies a sub-menu inside a top. Values are unique across all
// tops so a (top, sub) pair is unambiguous and individual subs can be
// referenced without first resolving their parent top.
type subID int

const (
	subBoard subID = iota
	subTable
	subGraph
	subStatsGeneral
	subStatsLogs
	subSettingsGeneral
	subSettingsLaws
	subSettingsPersonas
	subSettingsSkills
	subSettingsTemplates
	subSettingsTags
)

// topOrder is the canonical cycle order for tab/shift+tab and the order
// the top kicker renders left to right.
var topOrder = []topID{topTasks, topStats, topSettings}

// subsByTop lists the subs each top exposes, in render and cycle order.
// The sub strip is suppressed when the active top has only one sub.
var subsByTop = map[topID][]subID{
	topTasks:    {subBoard, subTable, subGraph},
	topStats:    {subStatsGeneral, subStatsLogs},
	topSettings: {subSettingsGeneral, subSettingsLaws, subSettingsPersonas, subSettingsSkills, subSettingsTemplates, subSettingsTags},
}

var topLabels = map[topID]string{
	topTasks:    "TASKS",
	topStats:    "STATS",
	topSettings: "SETTINGS",
}

var subLabels = map[subID]string{
	subBoard:             "board",
	subTable:             "table",
	subGraph:             "graph",
	subStatsGeneral:      "general",
	subStatsLogs:         "logs",
	subSettingsGeneral:   "general",
	subSettingsLaws:      "laws",
	subSettingsPersonas:  "personas",
	subSettingsSkills:    "skills",
	subSettingsTemplates: "templates",
	subSettingsTags:      "tags",
}

// settingsEntitySubs maps the per-entity Settings subs to the underlying
// `entityKind` they drive. The "general" sub is intentionally excluded —
// it is read-only and does not bind to an entity list. Used to keep
// `m.entityKind` in sync as the user cycles through Settings subs.
var settingsEntitySubs = map[subID]entityKind{
	subSettingsLaws:      entityKindLaw,
	subSettingsPersonas:  entityKindPersona,
	subSettingsSkills:    entityKindSkill,
	subSettingsTemplates: entityKindTemplate,
	subSettingsTags:      entityKindTag,
}

// entityKindForSub returns the entity kind owned by a Settings sub, plus
// a bool that is false for `subSettingsGeneral` (and any non-Settings
// sub) — callers can distinguish "no entity" from "Laws" without a
// second comparison.
func entityKindForSub(s subID) (entityKind, bool) {
	k, ok := settingsEntitySubs[s]
	return k, ok
}

// navState is the addressable navigation key — used to detect view
// changes across an Update tick (so refreshAfterViewChange can re-fetch
// only when the user actually navigated).
type navState struct {
	top  topID
	sub  subID
}

// firstSub returns the canonical landing sub for a top — what the user
// sees after `tab`/`shift+tab`/digit-jump. Stats lands on general, Tasks
// on board, Settings on config.
func firstSub(t topID) subID {
	if subs := subsByTop[t]; len(subs) > 0 {
		return subs[0]
	}
	return subID(-1)
}

// topIndex returns the position of t in topOrder, or -1 when not found.
func topIndex(t topID) int {
	for i, candidate := range topOrder {
		if candidate == t {
			return i
		}
	}
	return -1
}

// subIndex returns the position of s within its parent top's sub list,
// or -1 when the sub does not belong to t.
func subIndex(t topID, s subID) int {
	for i, candidate := range subsByTop[t] {
		if candidate == s {
			return i
		}
	}
	return -1
}

// onHome reports whether the model is currently on the multi-project
// Home view. Centralised so callers do not have to remember the sentinel.
func (m Model) onHome() bool {
	return m.top == topHome
}

// viewHistoryCap bounds how many back-stack entries the model keeps.
// 16 is roomy enough for typical session traversal without letting the
// slice grow unboundedly across long-running TUI sessions.
const viewHistoryCap = 16

// pushHistory records the *current* (top, sub) before a navigation
// changes them, so a subsequent `ctrl+o` can restore it. Skips
// duplicate consecutive entries (e.g. pressing `1` twice when already
// on Tasks) and drops the oldest entry when the stack hits its cap.
func (m *Model) pushHistory() {
	entry := navState{top: m.top, sub: m.sub}
	if n := len(m.viewHistory); n > 0 && m.viewHistory[n-1] == entry {
		return
	}
	m.viewHistory = append(m.viewHistory, entry)
	if extra := len(m.viewHistory) - viewHistoryCap; extra > 0 {
		m.viewHistory = m.viewHistory[extra:]
	}
}

// popHistory restores the most recent (top, sub) recorded by
// pushHistory, returning true on success. No-op when the stack is
// empty so `ctrl+o` at the start of a session is silently dropped.
func (m *Model) popHistory() bool {
	n := len(m.viewHistory)
	if n == 0 {
		return false
	}
	prev := m.viewHistory[n-1]
	m.viewHistory = m.viewHistory[:n-1]
	m.top = prev.top
	m.sub = prev.sub
	m.syncEntityKindFromSub()
	return true
}

// refreshTickMsg drives the realtime refresh loop — emitted every second
// while the user is on a "live" view (board, table, etc.) and not editing.
// shouldRealtimeRefresh decides whether to honor each tick.
type refreshTickMsg struct{}
