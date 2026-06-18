package tui

import (
	"context"
	"sync"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/activity"
	"omakiten/internal/agentruntime"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/cardlist"
	"omakiten/internal/tui/components/cursorwindow"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/linelist"
	"omakiten/internal/tui/components/notification"
	"omakiten/internal/tui/components/picker"
	"omakiten/internal/tui/components/viewport"
	"omakiten/internal/tui/palette"
)

// styleKind enumerates the chrome variants that benefit from per-width
// lipgloss.Style memoisation. Routing through a single map-of-maps
// keeps the four caches behind a single field on Model instead of four
// parallel ones; adding a new variant is one constant + one map entry.
type styleKind int

const (
	styleKindCard styleKind = iota
	styleKindCardSelected
	styleKindCardArchived
	styleKindInput
)

const (
	columnWidth               = 28
	taskDetailsPanelWidth     = 40
	taskCommentsPanelMinWidth = 44
	taskCommentsPanelMaxWidth = 96
	// projectMetaPanelMinWidth floors the project-view metadata column and
	// gates the side-by-side ↔ stacked decision: the two columns sit
	// side-by-side only when the terminal can give the meta panel at least
	// this many cells next to the activity rail (plus the 2-cell gutter).
	projectMetaPanelMinWidth   = 32
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
	Tags         app.TagRepository
	Editor       *app.BundleEditor
	BundleStore  app.BundleStore
	EntityFiles  app.EntityFileWriter
	Slugger      app.Slugifier
	ActivityLogs activity.ActivityLogRepository
	Events       app.EventRepository
	Metrics      *app.MetricsService
	Orphans      app.OrphanRepository
	Plans        app.PlanRepository
	// Search powers the trick palette Search tab (#182). Optional —
	// when nil, palette.SearchMsg surfaces a "search not wired" status
	// inline so the rest of the palette (Tricks tab + Ctrl+K
	// open/close) stays usable on test fixtures that do not need the
	// FTS5 path.
	Search *app.SearchService
	// Checkpointer is invoked right before a destructive snapshot
	// (project delete) so the live SQLite WAL frames land in the
	// main .db file the BackupService will copy. Optional — when
	// nil, destructive flows still snapshot (best-effort).
	Checkpointer app.Checkpointer

	// Watermark probes the DB change watermark (PRAGMA data_version) so
	// the realtime tick can skip a full reload on idle seconds. Optional —
	// when nil the tick reloads every cadence (pre-watermark behaviour),
	// so test fixtures that don't wire it keep working. Production wires
	// *sqlite.Store here (it satisfies the port via a pinned connection).
	Watermark app.DataVersionReader

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

	// Catalog is the TUI-surface i18n catalog the model consults via
	// Model.t for every render literal (labels, empty states, help
	// bindings, status messages). nil-safe: Catalog.Get on a nil
	// receiver returns the key verbatim so test fixtures that omit
	// this field still produce deterministic output without exploding.
	// Production wire-up routes rt.Snapshot().Catalog(SurfaceTUI) into
	// this field at composition time so the renderer follows the user's
	// configured TUI language without touching the snapshot directly.
	Catalog *config.Catalog
}

// t resolves the catalog key for the active TUI language. Production
// passes a non-nil Repositories.Catalog from rt.Snapshot().Catalog(...),
// so the active TUI pack drives every rendered literal. Tests typically
// build a Model without populating the field; in that case t falls back
// to a package-level singleton catalog backed by the bundled English
// pack so renderers still emit human-readable strings (instead of the
// raw catalog key literal, which would force every test that asserts
// on a label to thread its own catalog wiring). Catalog.Get is also
// nil-safe at the bottom of both chains, so a load failure produces
// the key literal rather than a panic.
func (m Model) t(key string) string {
	if m.repos.Catalog != nil {
		return m.repos.Catalog.Get(key)
	}
	return pkgTUICatalog().Get(key)
}

var (
	pkgTUICatalogPtr  *config.Catalog
	pkgTUICatalogOnce sync.Once
)

// pkgTUICatalog returns a lazily-initialized Catalog wrapping the
// bundled English language pack. Used as the fallback resolver for
// Model.t when Repositories.Catalog is nil (test paths). The bundled
// pack always ships in the binary's embed FS so the load cannot fail
// at runtime under normal conditions; if it does, the returned
// *Catalog is nil and Catalog.Get's nil-receiver chain preserves the
// key-literal fallback.
func pkgTUICatalog() *config.Catalog {
	pkgTUICatalogOnce.Do(func() {
		baseline, err := config.LoadBundledLanguage("en")
		if err != nil {
			return
		}
		pkgTUICatalogPtr = config.NewCatalog(&baseline, &baseline)
	})
	return pkgTUICatalogPtr
}

// entityServiceRepos aggregates the editor/file/slugger triple every
// entity service shares so inline TUI construction sites no longer
// repeat the same three field reads at each callsite. Mirrors the
// CLI runtime's entityServiceRepos helper.
func (r *Repositories) entityServiceRepos() app.EntityServiceRepos {
	return app.EntityServiceRepos{Editor: r.Editor, Files: r.EntityFiles, Slugger: r.Slugger}
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
	// moveInputTargetID names the task the next modeMove submission
	// rewrites. Zero means "fall back to selectedTask" (the legacy
	// parent/board behaviour). Non-zero is set by `m` on the sub-tasks
	// pane so the entered bucket key applies to the focused child, not
	// the parent the task view is open against. Reset on submit /
	// cancel so it cannot leak across moves.
	moveInputTargetID int64
	status            string
	moveMode          bool
	helpOpen          bool
	helpAll           bool
	// paletteOpen tracks the trick palette overlay (#182). When true,
	// every keypress routes through palette.Model.Update until the
	// overlay emits palette.DismissMsg (esc) or a SubmitMsg /
	// SearchMsg the root Update consumes and closes the overlay
	// against.
	paletteOpen bool
	palette     palette.Model
	// paletteRegistry resolves nav:<code> codes to Route slugs. Built
	// once at composition (newPickerModel-style helpers and cli/tui.go)
	// from palette.DefaultScreens + the user's config.tricks.nav
	// overrides so the palette dispatch path stays allocation-free at
	// each open.
	paletteRegistry *palette.Registry
	// viewHistory is the in-memory back-stack populated whenever the user
	// makes an intentional zone/sub navigation (tab / digit / `,`/`/`,
	// `0`, `ctrl+h`). Bound to a small cap so long sessions cannot grow
	// it unbounded; `ctrl+o` (vim-style "older") pops the most recent
	// entry. Refreshes and overlay close events do not touch this — the
	// stack is a record of *navigation*, not of every state change.
	viewHistory []navState

	taskScreen taskScreenMode
	taskID     int64
	// taskCreateParentID names the parent task when the create form was
	// opened as a sub-task (key `n` from inside the detail view). nil
	// means "create a root task"; non-nil triggers the AddSub branch in
	// saveTaskForm and renders the parent breadcrumb in the form header.
	// Reset on every closeTaskScreen so the next root-create cannot
	// inherit a stale FK.
	taskCreateParentID *int64
	// taskTitleInput / taskDescriptionInput own caret state and inline text
	// editing for the create/edit form. Replacing the prior `taskTitle` /
	// `taskDescription` strings with bubbles components fixed the "no
	// cursor / can only erase from end" UX bug — these models handle
	// arrow-key navigation, home/end, word-wise delete, paste, etc.
	taskTitleInput       textinput.Model
	taskDescriptionInput textarea.Model
	taskPriority         domain.Priority
	taskField            taskFormField
	// §E sectioned edit form — Tags is a CSV single-line input split on
	// save, Parent accepts a task id with a blur-lookup validation hook.
	// Both fields live on every open form (create + edit) so the section
	// rotation chain stays uniform; they are no-ops on create when left
	// empty.
	taskTagsInput   textinput.Model
	taskParentInput textinput.Model
	// taskParentLookupError surfaces the blur-time validation hint when
	// the parent id input doesn't resolve in the active project.
	// Persists across mid-edit keystrokes so the user can read the hint
	// long enough to act on it; cleared on (a) input emptied to "" by a
	// keystroke, (b) successful blur revalidation, or (c) form
	// open/close lifecycle.
	taskParentLookupError string
	// taskEditInitial holds the values captured at openTaskEdit time so
	// esc can prompt before discarding edits. Zero value = "no edit in
	// flight" so create flow short-circuits the dirty check.
	taskEditInitial taskEditSnapshot
	// taskEscPendingDiscard arms the dirty-discard prompt: the first esc
	// on a dirty edit surfaces a status hint; a second esc closes
	// without saving. Cleared by any non-esc key.
	taskEscPendingDiscard bool
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

	tasks        []domain.Task
	workflow     domain.Workflow
	dependencies []domain.TaskDependency
	comments     []domain.Comment
	laws         []domain.Law
	skills       []domain.Skill
	personas     []domain.Persona
	templates    []config.TaskTemplate
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
	// languages is the resolved per-surface language selection from the
	// active bundle, with EffectiveLanguages defaults applied (CLI/TUI
	// fall back to "en"; AgentOutput stays empty when unset). Populated
	// by reloadBundle so Settings › General can render the three rows
	// without re-reading the snapshot at render time.
	languages               config.LanguageSettings
	themePickerOptions      []themeOption
	configPickerOptions     []configOption
	subtaskKitPickerOptions []subtaskKitOption
	tags                    []domain.Tag
	taskTagsMap             map[int64][]domain.Tag
	metrics                 domain.TokenMetrics
	selected                int
	colIdx                  int
	cardIdx                 int

	entityKind    entityKind
	entityCursors map[entityKind]int
	// entityLists owns the per-kind cardlist.Model whose items
	// represent ROWS of the entity grid (cards wrap into rows of
	// entityGridCols(contentWidth)). Scroll is a row index inside
	// the cardlist; storing rows-as-items means a partial-row
	// alignment cannot drift — the cardlist guarantees the cursor
	// row stays inside the slice.
	entityLists  map[entityKind]cardlist.Model
	entityScreen entityScreenMode

	// settingsGeneralLines owns the read-only scroll state for the
	// Settings › General sub-tab. The view has no card cursor so
	// the linelist's internal cursor stays at the no-selection
	// sentinel (-1); ScrollBy drives pgup/pgdn / j / k. The
	// component clamps the offset against the body length +
	// viewport on each mutation.
	settingsGeneralLines linelist.Model
	entityForm           entityForm
	deletePending        bool
	deleteKind           entityKind
	deleteSlug           string

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
	// homeProjectDeletePendingID arms the destructive Home delete gate.
	// First `d` press records the highlighted project's id and surfaces
	// a confirmation hint in the status badge; second `d` on the same
	// project runs ProjectService.Delete (with auto-backup). esc on
	// Home (or any other key) clears the arm so the user cannot
	// accidentally confirm a delete after moving the cursor onto a
	// different project. Mirrors the task-delete arm-then-confirm
	// shape rather than spinning up a notification overlay — the spec
	// prefers an overlay but the codebase precedent (task delete +
	// orphan rebind revert) is the simpler, well-tested gate.
	homeProjectDeletePendingID int64
	// homeProjectDeletePendingCounters captures the per-table snapshot
	// resolved at arm-time so executeHomeProjectDelete can hand it to
	// ProjectService.Delete without a second round-trip. Zeroed when
	// pendingID clears.
	homeProjectDeletePendingCounters domain.ProjectDeleteCounters
	// lastProjectRoot is the root_path of the last project the user opened
	// during the session. CLI-side cd-on-exit reads this after program.Run()
	// returns so the parent shell wrapper can `cd` into the project.
	lastProjectRoot string

	// lastDataVersion is the SQLite change watermark (PRAGMA data_version)
	// observed at the previous realtime tick. The tick re-probes
	// repos.Watermark each second and only triggers the expensive reload
	// (reloadBundleIfChanged + realtimeRefresh) when the watermark moved —
	// an idle second (no external write) costs one cheap probe and skips
	// the rebuild. Self-writes are exempt by design: they commit on the
	// pool, not the pinned probe connection, AND repaint inline via the
	// synchronous m.refresh() on the write path, so the watermark gate
	// never starves them. Zero before the first probe; -1 has no special
	// meaning, any value is just compared for equality.
	lastDataVersion int64
	// dataVersionSynced records whether lastDataVersion holds a real probe
	// result yet. The first tick after launch always reloads (treat as
	// changed) so the view reflects writes that landed between Open and
	// the first tick; thereafter the equality compare gates reloads.
	dataVersionSynced bool

	// realtimeReloadGen is a monotonic generation counter incremented on the
	// Update goroutine each time a changed-tick reload cmd is built. The value
	// is stamped onto the realtimeReloadMsg the worker produces so
	// applyRealtimeReload can recognise stale arrivals: two reloads in flight
	// (consecutive watermark-moving ticks + a slow worker) can complete out of
	// order, and folding an older snapshot over a newer one would briefly
	// regress the view. lastAppliedReloadGen tracks the newest generation
	// already folded; any msg with an older generation is dropped (F3).
	realtimeReloadGen    uint64
	lastAppliedReloadGen uint64

	// events is the bounded row buffer rendered by the unified Logs
	// inspector (umbrella #320, sub-task #325). Populated by
	// refreshActivityLogs from EventRepository.ListEvents with the
	// active views.logs.window_days time floor and views.logs.limit
	// cap; the summary tables aggregate the wider window via
	// EventRepository.EventCategoryCounts.
	events       []domain.EventRow
	logsSelected int
	// logsFilterMode is the active filter preset for the Stats › Logs
	// inspector. Cycled by `f` (forward) and `shift+F` (backward) via
	// handleLogsKey; folded into the repository fetch through
	// Model.logsCategoryFilter on every refreshActivityLogs tick. Zero
	// value (LogsFilterAll) is the no-op filter, so a freshly opened
	// Logs view surfaces every event_type without any user action.
	// State preserved across refresh because it lives on Model.
	logsFilterMode LogsFilterMode
	// eventStats holds the unbounded aggregates the Logs inspector
	// summary tables render: per-category counts (every known
	// category present, count 0 acceptable) and a tool_call health
	// breakdown (ok/error/running across tool_call + hook subset).
	// Populated alongside `events` on every refresh of the Stats ›
	// Logs view so the headline numbers reflect everything in the
	// snapshot's logs.window_days horizon — not just the rows that
	// fit in the panel beneath.
	eventStats eventStats

	// activity holds the unified activity feed (comments + system events)
	// for the currently-open task detail view. Populated on openTaskView
	// and cleared when the screen closes — refresh does not refetch this
	// because the rest of refresh() loads all-task data.
	activity        []domain.Event
	activityForTask int64
	// activityLines owns the activity panel's body scroll via
	// linelist.Model. The cursor inside the linelist is the line
	// index of the focused card's top row — syncActivityScrollToCursor
	// keeps it aligned with m.activityCursor (the card index) via
	// cardLineRanges. Scroll is always a LINE offset; the bug class
	// (line vs card index) cannot be written because the field is
	// unexported on the component.
	activityLines linelist.Model
	// activityCursor is the index into the visible activity feed; -1 means
	// "no card selected" and disables the focused-border styling. Card
	// navigation moves it; the scroll offset auto-follows.
	activityCursor int
	// taskFocus tracks which column inside the task detail screen owns
	// navigation keys. Default: form/details column. Tab rotates through
	// form → sub-tasks (when present) → activity so j/k applies to the
	// section the user just jumped to, instead of always scrolling the
	// description and forcing modal switches per surface.
	taskFocus taskScreenFocus

	// projectFocus tracks which zone (form / dashboard / activity) owns
	// navigation keys inside the project-view screen (subProjectView).
	// Reset to projectFocusForm whenever the screen is (re)opened by
	// Ctrl+P so re-entry always lands on the form side. Mirrors taskFocus.
	projectFocus projectScreenFocus

	// projectActivity caches the project- and universal-scoped comment
	// events fetched by refreshProjectSummary for the project-view
	// screen. Held on the model so the render pass and the activity
	// scroll handlers read the same slice without re-querying the store
	// on every keystroke.
	projectActivity []domain.Event

	// projectActivityScroll is the line offset of the project-view
	// activity feed viewport. j/k/pgup/pgdn nudge it while the activity
	// panel owns focus; the form + dashboard zones are not scroll-windowed
	// (they draw a full fixed body), so the scroll keys no-op there. The
	// per-card cursor (projectActivityCursor) drives the offset so the
	// focused card stays in view; the raw line offset survives pgup/pgdn.
	projectActivityScroll int

	// projectActivityCursor is the index into m.projectActivity of the
	// selected card; -1 means "no card selected" and disables the
	// focused-border styling. j/k/arrow move it by card (mirroring
	// activityCursor for the task feed); the scroll offset auto-follows so
	// the selected project/universal comment stays inside the viewport.
	projectActivityCursor int

	// projectDescription caches the current project's description body,
	// fetched (alongside the activity feed) by refreshProjectSummary via
	// FindProjectByID. m.project carries only the identity fields resolved
	// at boot, so the description is loaded here for the project-view
	// metadata panel and re-read on every render.
	projectDescription string

	// projectTags caches the current project's tag attachments
	// (project_tags bridge), fetched by refreshProjectSummary via
	// ListProjectTags. Rendered as a chip row in the project-view
	// metadata panel.
	projectTags []domain.Tag

	// projectDashboard caches the per-project status dashboard counts
	// (tasks-per-bucket + total, root/sub split, plan progress) computed
	// by refreshProjectSummary. Held on the model so the render pass reads
	// the same snapshot without re-querying on every keystroke. Mirrors
	// the task view's sub-tasks slot, replaced here by the dashboard.
	projectDashboard projectDashboardData

	// projectFormScreenOpen is the modal "project form detail" overlay
	// layered on top of the project view, opened with `f`. Long project
	// descriptions overflow the form zone, so this dedicated full-width
	// screen renders the metadata + uncapped, scrollable description.
	// esc/f returns to the project view with focus preserved. Clones the
	// task view's descriptionScreen / planGoalScreen pattern.
	projectFormScreenOpen bool
	// projectFormScreen owns the scroll offset for the dedicated project
	// form overlay; reset via detailscreen.New on each open so prior
	// scroll state never leaks across project switches.
	projectFormScreen detailscreen.Model

	// subtasks owns cursor + scroll for the sub-tasks pane. The
	// cardlist.Model encapsulates the (cursor, scroll, items,
	// viewport) tuple so no callsite can write the scroll field
	// directly — the W11 refactor that closed the unit-mismatch
	// bug class (line offset vs card index) routes every mutation
	// through MoveCursor / WithItems / WithViewport.
	//
	// In the #281 bucket-grouped panel the cardlist is scoped to the
	// CURRENTLY FOCUSED bucket column — every other column renders
	// flush from a transient cardlist so cursor + scroll mirror the
	// root board's per-lane semantics.
	subtasks cardlist.Model

	// subtaskColIdx is the focused bucket-column index inside the
	// detail-view sub-tasks panel (#281 cascade phase 5). Mirrors the
	// root board's colIdx but scoped to the sub-task panel only.
	subtaskColIdx int

	// subtaskColOffset is the leftmost-visible bucket index for the
	// sub-tasks panel's horizontal column carousel. Used when the
	// terminal cannot fit every column at once; mirrors boardColOffset
	// with the same scrollIntoView semantics.
	subtaskColOffset int

	// taskViewStack records ancestor task IDs the user drilled in from
	// (via Enter on a sub-task card). Esc pops back to the most recent
	// ancestor instead of jumping straight to the board, so parent
	// context survives N-level drilling. Empty means "current task was
	// opened from a list view; esc closes the detail screen".
	taskViewStack []int64

	// descriptionScreenOpen is the modal "description detail" overlay
	// layered on top of taskScreenView. Long descriptions overflow the
	// form column, so `f` opens this dedicated full-width screen where
	// the markdown can scroll freely. esc returns to the task detail
	// view with focus preserved.
	descriptionScreenOpen bool
	// descriptionScreen owns the scroll offset for the dedicated
	// description overlay; reset via detailscreen.New on each open so
	// prior scroll state never leaks across tasks.
	descriptionScreen detailscreen.Model

	// commentScreenOpen is the modal "comment detail" view layered on top of
	// taskScreenView. Long comments overflow the activity column, so Enter on
	// a focused comment opens this dedicated screen where the body can scroll
	// freely. Returning with esc preserves taskScreen + activityCursor so the
	// user lands back on the same card.
	commentScreenOpen bool
	commentScreenID   int64
	// commentScreenFromProject marks that the open comment detail was
	// launched from the project-view activity feed (project/universal scope)
	// rather than the task activity panel. The detail lookup (activeComment)
	// reads m.projectActivity instead of the task feed when set, and esc
	// restores the project activity cursor rather than the task cursor.
	commentScreenFromProject bool
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

	// boardColOffset is the leftmost-visible bucket index for the
	// board's horizontal column carousel. Renamed from the prior
	// boardColScroll to escape the W11 arch-test pattern that
	// guards vertical card/line scroll fields — this is a
	// horizontal carousel offset, not a card-or-line viewport
	// scroll, so the cardlist/linelist component contract does
	// not apply here.
	boardColOffset int

	// boardLists owns the per-bucket cardlist.Model (cursor + scroll
	// + items + viewport) so long columns can be scrolled vertically
	// without losing context when navigating between lanes. Keys
	// are bucket keys (domain.Bucket.Key). The cardlist's cursor
	// mirrors m.cardIdx (the global focused-column cursor) — every
	// sync routes through WithCursor + WithItems + WithViewport so
	// the component's scrollwindow.Resync owns scroll correctness.
	boardLists map[string]cardlist.Model

	// entityView owns the detail-grid builder + scroll offset for the focused
	// entity detail screen.
	entityView detailscreen.Model

	// logsList / tableList / graphList / plansList own scroll state
	// for the line-based panels (logs, table, plan list, graph
	// outline). Each is a linelist.Model whose cursor mirrors the
	// surface's authoritative selection field via WithCursor at sync
	// time; the cardlist/linelist component owns the scroll offset
	// so the bug class (line vs index unit mismatch) cannot be
	// written into the parent Model.
	logsList  linelist.Model
	tableList linelist.Model
	graphList linelist.Model
	// graphCursor owns the selected-node cursor as a cursorwindow.Model
	// indexed into the selectable-row slice (sel) produced by
	// dagSelectableIndices. graphList stays as the linelist for line-
	// based scroll math because the DAG renderer interleaves
	// non-selectable connector lines with selectable nodes — the two
	// state holders sit on different units (selectable index vs raw
	// line offset) by design.
	graphCursor cursorwindow.Model

	// plans holds the project's plan rollups for the Tasks › plans sub-tab
	// list view. Populated by refresh() via PlanService.ListRollups.
	// plansCursor (cursorwindow.Model) owns the cursor + scroll pair —
	// the prior raw `planCursor int` + linelist hybrid is gone; W11
	// extends the cursor-as-unexported-state contract to fixed-row
	// surfaces like this one.
	plans       []app.PlanRollup
	plansCursor cursorwindow.Model

	// planNetworkOpen flips when the user presses enter on a row in the
	// plans list view — it swaps the renderer from the list view to the
	// rails+filaments outline. esc / `q` flips back.
	planNetworkOpen bool
	planNetworkShow app.PlanShow
	// planNetworkCursor is the linear cursor into the flat row
	// projection (planNetworkBuildRows). It walks BOTH wave-header
	// rows AND task rows so the user can space-toggle a wave without
	// first leaving the task list. cursorwindow.Model owns the
	// (cursor, scroll) pair indexed into the row slice; the parent
	// cardlist (planNetwork) continues to own variable-height scroll
	// math by mirroring this cursor through WithCursor at sync time.
	planNetworkCursor cursorwindow.Model
	// planNetwork owns the scroll state for the flat row list via
	// cardlist.Model. Cursor mirrors planNetworkCursor through
	// WithCursor at sync time; the cardlist's internal
	// scrollwindow.Resync makes the unit-mismatch bug class
	// impossible — there is no `planNetworkScroll int` field
	// callers could write the wrong unit to.
	planNetwork cardlist.Model
	// planNetworkCollapsed records which waves are folded down to a
	// single header row. Default state is expanded (entries missing
	// from the map render expanded). Toggled with `space` on the
	// focused wave header.
	planNetworkCollapsed map[int64]bool
	// planGoalScreenOpen flips when the user presses `f` on a row in the
	// plans list view — it swaps the renderer to the read-only full-width
	// goal-body overlay (mirrors the task view's `f` description screen).
	// esc / `f` flips back. Read-only: the goal is never edited from here.
	planGoalScreenOpen bool
	// planGoalShow is the PlanShow projection fetched on `f` so the goal
	// overlay can render the cursored plan's slug/name/status header plus
	// its markdown goal_body. Reset on close so the next open re-fetches.
	planGoalShow app.PlanShow
	// planGoalScreen owns the scroll offset for the goal overlay; reset via
	// detailscreen.New on each open so prior scroll state never leaks across
	// plans.
	planGoalScreen detailscreen.Model
	// planGoalEditingID names the plan whose goal_body is open in the
	// modePlanGoal textarea overlay. Non-zero while the overlay is
	// active; reset to 0 on submit / cancel.
	planGoalEditingID int64
	// planAssignTaskID names the task whose assignee is being typed in
	// the modePlanAssign single-line input. Captured at `c` press
	// time (cursor row) so the row the user saw when they triggered
	// the modal is the one that gets the write — toggling waves or
	// scrolling under the input does not move the target. Reset to 0
	// on submit / cancel.
	planAssignTaskID int64

	// tokenCountCache memoises m.counter.Count(body) per content hash so
	// computeMetrics does not re-tokenise every law / persona / comment
	// body on every refresh. Bodies are stable until the user edits one;
	// the cache only grows for new bodies. Cleared on theme reload via
	// the same hook that drops the markdown caches (themes do not affect
	// token counts, so theme-reload clearing is conservative — the cache
	// stays warm across normal refresh ticks).
	tokenCountCache map[uint64]int

	// cachedTasksByBucket memoises tasksByBucket() so the board renderer
	// + every cursor handler that calls tasksInCurrentBucket share one
	// filter+group pass per refresh. Invalidated whenever m.tasks /
	// m.views.Board.Filter / m.priorities change (refresh() and any
	// mutation handler that bumps the latter). nil = not yet built.
	cachedTasksByBucket map[string][]domain.Task

	// cachedTableView memoises applyTableView() — the renderTable hot
	// path + cursor math share the same filtered+sorted slice. Same
	// invalidation rule as cachedTasksByBucket plus the Table view
	// settings (filter / sort).
	cachedTableView []domain.Task

	// styleByKindWidth memoises lipgloss.Style.Width(N) for the four
	// chrome variants (card / cardSelected / archivedCard / input) so
	// taskCardSpec + form-field rendering reuse the same Style across
	// every cell with the same (kind, width) pair. Long board columns
	// previously allocated a fresh Style per card per render; the map
	// lookup amortises the cost across the whole column. Cleared at
	// theme change (rare) via styles rebuild — entire outer map is
	// re-allocated so stale colours never serve from a stale inner.
	styleByKindWidth map[styleKind]map[int]lipgloss.Style

	// activityCardsCache memoises activityRowsForRender so the three
	// scroll-math hot paths (clampActivityScroll, syncActivityScroll-
	// ToCursor, visibleActivityCardRange) plus the renderTaskComments-
	// Cell view pass share a single render per keystroke instead of
	// rebuilding the lipgloss-styled comment / system-event cards on
	// each call. Keyed on (taskID, cursor, commentCardWidth, per-event
	// id+type+bodyLen+sorted tag-id checksum).
	activityCardsCache activityCardsCacheEntry

	// taskDetailsBoxHeightCache memoises lipgloss.Height(renderTaskDetails-
	// Box(...)) so subtasksViewportRows + taskFocusedSectionOffset stop
	// re-rendering the form on every j/k keystroke in stacked layout.
	// Keyed on (taskID, formValueWidth, blockerCount, tagCount,
	// title/description lengths, bucket, priority) so any model field
	// that changes the rendered form structure bumps the key. *Model
	// handlers (syncSubtaskScrollToCursor) warm the cache; the
	// value-receiver render path reads it.
	taskDetailsBoxHeightCache taskDetailsBoxHeightCacheEntry

	// planNetworkRowsCache memoises planNetworkBuildRows() across the
	// 11 invocation sites in handlePlanNetworkKey (j/k/h/l/space/pgup/
	// pgdn/g/G/enter all rebuild for cursor / scroll math). Keyed on
	// (planID, collapsedMap, per-task id+bucket) so any state change
	// that affects the projection bumps the key; identical inputs
	// short-circuit to the cached slice without re-running the DFS.
	planNetworkRowsCache planNetworkRowsCacheEntry

	// planNetworkBuildCache memoises the FULL planNetworkBuildData
	// (rows + cross-blocker index + next-claimable id + the
	// critical-path DFS), which the renderer used to recompute on
	// every View(). Keyed on the same structural fingerprint as the
	// row cache PLUS the dependency edge set (the critical-path DFS
	// reads show.Dependencies, which the row-only key omits because the
	// row skeleton does not depend on it). Identical inputs short-
	// circuit to the cached build so the DFS runs once per state change
	// rather than once per frame.
	//
	// Held behind a pointer so the value-receiver renderer
	// (renderPlanNetwork, reached from View()) can persist the freshly
	// built projection through the shared entry — the idle-tick render
	// path has no pointer-receiver handler to warm it. Lazily allocated
	// on first build.
	planNetworkBuildCache *planNetworkBuildCacheEntry

	// boardMutationEpoch is the local mutation counter the board-string
	// memo keys on. It is bumped on every rebuildBoardCaches() call —
	// which every inline m.refresh() and every realtime-reload board
	// fold runs — so the epoch moves on same-connection self-writes
	// that the data_version watermark deliberately does NOT move for.
	// Keying the board memo on this epoch (NOT the watermark) is what
	// stops a stale render from being served after a local edit.
	boardMutationEpoch uint64

	// boardStringCache memoises renderBoard()'s whole output string so a
	// frame whose render inputs are unchanged reuses the prior string
	// instead of re-walking every lane + card. Keyed on the local
	// mutation epoch + the board cursor (colIdx/cardIdx), horizontal
	// carousel offset, terminal width, move-mode flag, and the
	// archived-toggle — the full set of inputs renderBoard reads.
	//
	// Held behind a pointer so the value-receiver render path
	// (renderBoard, reached from View()) can write the freshly-rendered
	// string back through the shared entry — there is no pointer-receiver
	// handler on the idle-tick render path to warm it otherwise. The
	// pointer is lazily allocated on first render.
	boardStringCache *boardStringCacheEntry

	// boardBadgeCounts holds the per-task badge-count maps the kanban
	// card reads (dependency / comment / direct-child counts). Rebuilt
	// once per rebuildBoardCaches() from the loaded slices so
	// taskBoardBadges does O(1) map lookups instead of an O(n) scan per
	// card per frame.
	boardBadgeCounts boardBadgeCounts

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
	notification  *notification.Model

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
	// modePlanGoal is the in-TUI plan goal_body editor — multi-line
	// textarea bound to the focused plan. Reuses commentInput as the
	// underlying bubbles textarea so a single resize / cursor path
	// covers every multi-line modal in the TUI.
	modePlanGoal
	// modePlanAssign is the in-TUI assignee editor for the task
	// focused in the plan-network outline. Single-line input on the
	// shared modal bar (reuses moveInput) — submit calls
	// TaskService.Assign on the captured planAssignTaskID. The claim
	// binding (`c`) NEVER moves the task between buckets; bucket
	// transitions remain governed by the preset's own move guards.
	modePlanAssign
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
	taskFocusSubtasks
	taskFocusActivity
)

// projectScreenFocus is which zone owns navigation keys inside the
// project-view screen (Ctrl+P). Tab cycles form → dashboard → activity
// so the user can scroll any zone without a per-zone binding — the same
// three-zone model the task view uses (form / sub-tasks / activity),
// with the sub-tasks slot replaced by a project status dashboard.
type projectScreenFocus int

const (
	projectFocusForm projectScreenFocus = iota
	projectFocusDashboard
	projectFocusActivity
)

// projectDashboardData is the per-project status snapshot the project
// view's dashboard zone renders: tasks per workflow bucket (+ total),
// the root/sub-task split, and aggregate plan progress. Computed by
// refreshProjectSummary from the already-wired Tasks/Plans repos so the
// render pass stays pure. Zero value is a valid empty dashboard.
type projectDashboardData struct {
	// bucketCounts holds the task count for every workflow bucket, in
	// bucket order, paired with the localized bucket name. Roots and
	// sub-tasks both count toward their bucket (unlike the kanban board,
	// which hides sub-tasks) so the total reflects every active task.
	bucketCounts []projectBucketCount
	// totalTasks is the sum across every bucket — all active tasks.
	totalTasks int
	// rootTasks / subTasks split the total by parent_id (children vs
	// roots). rootTasks + subTasks == totalTasks.
	rootTasks int
	subTasks  int

	// planCount is the number of plans in the project; planDone /
	// planTotal are the aggregate done/total task counts across every
	// plan's rollup (PlanService.ListRollups).
	planCount int
	planDone  int
	planTotal int
}

// projectBucketCount pairs a workflow bucket's display name with its
// active task count for the dashboard's per-bucket rows.
type projectBucketCount struct {
	name  string
	count int
}

// taskFormField identifies which field of the create/edit form is focused.
// Tab cycles forward; the priority field has its own ←/→ cycle for the
// fixed enum (low/normal/high).
type taskFormField int

const (
	taskFieldTitle taskFormField = iota
	taskFieldDescription
	taskFieldPriority
	// §E adds Tags (CSV input) and Parent (id input) to the section
	// rotation. Order matches the spec heading order: Title →
	// Description → Priority → Tags → Parent.
	taskFieldTags
	taskFieldParent
)

// taskEditSnapshot captures the form values present when an edit form
// opens so esc can detect "dirty" and prompt before discarding. Tags
// are stored as the normalised CSV the field will display (lowercased
// + sorted) so a re-order of equivalent tag sets reads as clean.
type taskEditSnapshot struct {
	active      bool
	title       string
	description string
	priority    domain.Priority
	tagsCSV     string
	parent      string
}

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
	subPlans
	subStatsGeneral
	subStatsLogs
	subSettingsGeneral
	subSettingsLaws
	subSettingsPersonas
	subSettingsSkills
	subSettingsTemplates
	subSettingsTags
	subSettingsGuards
	// subProjectView is the dedicated project-view screen (Ctrl+P):
	// project metadata + a project-scoped activity feed. It is addressed
	// like any other sub for routing/footer purposes but is intentionally
	// absent from subsByTop — it is reachable only via the Ctrl+P binding
	// (like topHome's sentinel), never through the ,// sub cycle or tab.
	subProjectView
)

// topOrder is the canonical cycle order for tab/shift+tab and the order
// the top kicker renders left to right.
var topOrder = []topID{topTasks, topStats, topSettings}

// subsByTop lists the subs each top exposes, in render and cycle order.
// The sub strip is suppressed when the active top has only one sub.
var subsByTop = map[topID][]subID{
	topTasks:    {subBoard, subTable, subGraph, subPlans},
	topStats:    {subStatsGeneral, subStatsLogs},
	topSettings: {subSettingsGeneral, subSettingsLaws, subSettingsPersonas, subSettingsSkills, subSettingsTemplates, subSettingsTags, subSettingsGuards},
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
	subPlans:             "plans",
	subStatsGeneral:      "general",
	subStatsLogs:         "logs",
	subSettingsGeneral:   "general",
	subSettingsLaws:      "laws",
	subSettingsPersonas:  "personas",
	subSettingsSkills:    "skills",
	subSettingsTemplates: "templates",
	subSettingsTags:      "tags",
	subSettingsGuards:    "guards",
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
	top topID
	sub subID
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
