package tui

import (
	"context"
	"sync"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"

	"omakiten/internal/activity"
	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/studioprojection"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/palette"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/board"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/description"
	"omakiten/internal/tui/screens/entitydetail"
	"omakiten/internal/tui/screens/entitylist"
	"omakiten/internal/tui/screens/graph"
	"omakiten/internal/tui/screens/home"
	"omakiten/internal/tui/screens/insights"
	"omakiten/internal/tui/screens/logs"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/plans"
	projectscreen "omakiten/internal/tui/screens/project"
	"omakiten/internal/tui/screens/projectresume"
	"omakiten/internal/tui/screens/relationshippicker"
	settingsscreen "omakiten/internal/tui/screens/settings"
	"omakiten/internal/tui/screens/settingspicker"
	"omakiten/internal/tui/screens/stats"
	"omakiten/internal/tui/screens/studio"
	"omakiten/internal/tui/screens/table"
	"omakiten/internal/tui/screens/taskdetail"
	"omakiten/internal/tui/screens/taskform"
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
	projectMetaPanelMinWidth = 32
	commentInputHeight       = 5
	metaRowLabelWidth        = 14
	selectionMarker          = "▌"
	normalMarker             = " "
	cardBoxWidth             = 26
	cardContentWidth         = 24 // cardBoxWidth - horizontal padding(2); text fits here
)

// Repositories is the dependency-injection bundle the TUI receives from its
// composition root. Fields are local ports (D20) so this package does not
// import internal/app. Production wires *sqlite.Store / BundleEditor
// which satisfy the ports structurally.
type Repositories struct {
	Tasks        TaskStore
	Projects     ProjectStore
	Comments     CommentStore
	Dependencies interface {
		ListTaskDependencies(ctx context.Context, projectID, taskID int64) ([]domain.TaskDependency, error)
	}
	Tags           TagStore
	Editor         BundleEditor
	BundleStore    BundleLoader
	ActivityLogs   activity.ActivityLogRepository
	Events         EventStore
	Metrics        MetricsPort
	Insights       InsightsPort
	Orphans        OrphanStore
	Plans          PlanStore
	Search         SearchPort
	Checkpointer   Checkpointer
	SnapshotWriter SnapshotWriter
	Watermark      DataVersionReader

	DispatchCommand func(ctx context.Context, args []string) ([]byte, error)

	ConfigPath   string
	DBPath       string
	Version      string
	RepoLocalDir string

	Cache     *agentruntime.BundleCache
	ProjectID int64
	Catalog   *config.Catalog
	// runtimeOverride is used only by a staged reload Model. It lets refresh
	// validate the candidate service and snapshot without publishing the
	// candidate through BundleCache before acceptance succeeds.
	runtimeOverride *agentruntime.ProjectRuntime
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

// operationService returns the per-project operation.Service from the
// BundleCache entry the runtime installed at boot. Returns nil when the
// cache is not wired or the entry has no Service yet — callers must treat
// that as "facade unavailable".
func (r *Repositories) operationService() *operation.Service {
	if r.runtimeOverride != nil {
		if r.runtimeOverride.Service == nil {
			return nil
		}
		return r.runtimeOverride.Service.ForTUI()
	}
	if r.Cache == nil {
		return nil
	}
	pr := r.Cache.Get(r.ProjectID)
	if pr == nil {
		return nil
	}
	if pr.Service == nil {
		return nil
	}
	return pr.Service.ForTUI()
}

// activeSnapshot returns the per-project *config.Snapshot from the
// BundleCache entry the runtime installed at boot. TUI inline service
// constructions capture this pointer at the moment of dispatch; the
// cache rotates a fresh pointer on each rebuild, so subsequent calls
// see the new snapshot through the same accessor. Returns nil when the
// cache is not wired (rare test paths that bypass the runtime
// composition root).
func (r *Repositories) activeSnapshot() *config.Snapshot {
	if r.runtimeOverride != nil {
		return r.runtimeOverride.Snapshot
	}
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
	if r.runtimeOverride != nil {
		return r.runtimeOverride.PreviousSnapshot
	}
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
	// from the authoritative screen descriptors + the user's config.tricks.nav
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

	// commentInput is reused by modeComment (add) and modeCommentEdit
	// (rewrite). Reset on every beginInput call so the placeholder and
	// pre-fill values reflect the active mode without leaking text across
	// flows.
	commentInput textarea.Model

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
	// TUI constructs on the fly (TaskService, TUIQuery) so they
	// resolve labels/ids against this bundle instead of the deprecated
	// process-global tables.
	registry *domain.EnumRegistry
	// languages is the resolved per-surface language selection from the
	// active bundle, with EffectiveLanguages defaults applied (CLI/TUI
	// fall back to "en"; AgentOutput stays empty when unset). Populated
	// by reloadBundle so Settings › General can render the three rows
	// without re-reading the snapshot at render time.
	languages   config.LanguageSettings
	tags        []domain.Tag
	taskTagsMap map[int64][]domain.Tag
	metrics     domain.TokenMetrics

	deletePending bool
	deleteKind    entityKind
	deleteSlug    string

	// Non-zero means a second `d` press will confirm task deletion.
	// Comment deletion state is owned by the Comment detail screen.
	taskDeletePendingID int64

	// lastProjectRoot is the root_path of the last project the user opened
	// during the session. CLI-side cd-on-exit reads this after program.Run()
	// returns so the parent shell wrapper can `cd` into the project.
	lastProjectRoot string
	// projectGeneration changes on every project selection. Async project-bound
	// results carry this generation so a delayed A result cannot apply after an
	// A -> B -> A switch.
	projectGeneration uint64
	// studioRuntimeGeneration changes whenever the active project runtime or
	// snapshot rotates. Every async result that captured runtime-derived state
	// carries it so stale workflow, language, config, or projection data cannot
	// cross a project/config boundary.
	studioRuntimeGeneration uint64
	// studioOutcomeGeneration is stamped before dispatching a Studio message.
	// Reload can rotate the runtime while Screen.Update is running, so outcome
	// folding needs the generation that existed before that update.
	studioOutcomeGeneration    uint64
	studioOutcomeGenerationSet bool
	// viewChangeGeneration changes for every view navigation or project
	// selection. View-refresh results carry it so an older same-project
	// snapshot cannot overwrite the result of a newer navigation.
	viewChangeGeneration uint64
	// paletteOpenGeneration identifies the current palette session. Search
	// results must belong to the session that issued them, not merely an open
	// palette with the same project.
	paletteOpenGeneration uint64
	// paletteSearchGeneration orders queries within the current palette session.
	// A newer query invalidates every older worker result.
	paletteSearchGeneration uint64

	// dataVersionBaselines stores the SQLite change watermark (PRAGMA
	// data_version) per realtime reload domain. A scoped reload in one domain
	// (for example the task activity view) must not consume another domain's
	// pending board/bundle change. Map presence means the domain has an
	// established baseline; zero is a valid SQLite value. Baselines advance only
	// from applyRealtimeReload after that domain's payload has successfully
	// folded into m.
	dataVersionBaselines map[realtimeReloadKind]int64

	// realtimeReloadGen is a per-domain monotonic generation counter incremented
	// on the Update goroutine each time a changed-tick reload cmd is built. The
	// value is stamped onto the realtimeReloadMsg the worker produces so
	// applyRealtimeReload can recognise stale arrivals within the same domain.
	// lastAppliedReloadGen tracks the newest generation already folded per domain;
	// a high-gen activity reload must never make a lower-gen bundle reload stale.
	realtimeReloadGen    map[realtimeReloadKind]uint64
	lastAppliedReloadGen map[realtimeReloadKind]uint64

	// Studio owns its draft, cursor, scroll, and confirmation state. The root
	// only stores the screen value and binds fresh host dependencies to it.
	studioScreen                studio.Screen
	studioHookHistory           map[int][]studioprojection.HookExecuted
	studioHookHistoryReady      bool
	studioHookHistoryProjectID  int64
	studioHookHistoryGeneration uint64
	studioHookHistoryLoading    bool
	// studioApplyDiff carries the DiffStudioBundles output for the apply
	// currently in flight so emitBundleSwapped can fold it into the
	// bundle.swapped audit payload instead of discarding it after render.
	// Set immediately before StudioDraft.Apply and consumed/cleared inside
	// emitBundleSwapped; never read outside that single call chain.
	studioApplyDiff []string
	// views caches the resolved per-view sort/filter pulled from the active
	// bundle on each refresh. Render and query helpers read it instead of
	// the raw Settings so omitted fields show up as their canonical defaults.
	views config.ViewSettings

	// Extracted screens own their local projection and interaction state
	// behind the screenhost contract. The root stores only live screen values.
	boardScreen             board.Screen
	homeScreen              home.Screen
	tableScreen             table.Screen
	graphScreen             graph.Screen
	plansScreen             plans.Screen
	planGoalReaderScreen    plans.GoalScreen
	planNetworkScreen       plannetwork.Screen
	projectScreen           projectscreen.Screen
	projectFormReaderScreen projectscreen.FormScreen
	projectResumeScreen     projectresume.Screen
	settingsGeneralScreen   settingsscreen.Screen
	settingsGuardsScreen    settingsscreen.Screen
	themePickerScreen       settingspicker.Screen
	configPickerScreen      settingspicker.Screen
	subtaskKitPickerScreen  settingspicker.Screen
	entityDetailScreen      entitydetail.Screen
	entityListScreens       map[screenhost.ID]entitylist.Screen
	personaSkillsScreen     relationshippicker.Screen
	templateDefaultScreen   relationshippicker.Screen
	taskDetailScreen        taskdetail.Screen
	taskFormScreen          taskform.Screen
	descriptionReaderScreen description.Screen
	commentDetailScreen     commentdetail.Screen
	// screenStack contains modal route IDs pushed above the addressable base
	// route. Unlike legacy booleans it drives descriptor, lifecycle and back
	// navigation through the same host contract as every other screen.
	screenStack []screenhost.ID

	// planNetworkGeneration guards async open/reload/save results. A result is
	// applied only when both this generation and the screen's current slug match.
	planNetworkGeneration uint64
	// relationshipPickerGeneration invalidates outcomes after navigation or a
	// bundle projection refresh, before they can overwrite hosted screen state.
	relationshipPickerGeneration uint64
	taskFormGeneration           uint64
	taskDetailGeneration         uint64

	// tokenCountCache memoises m.counter.Count(body) per content hash so
	// computeMetrics does not re-tokenise every law / persona / comment
	// body on every refresh. Bodies are stable until the user edits one;
	// the cache only grows for new bodies. Cleared on theme reload via
	// the same hook that drops the markdown caches (themes do not affect
	// token counts, so theme-reload clearing is conservative — the cache
	// stays warm across normal refresh ticks).
	tokenCountCache map[uint64]int

	// cardStyles memoises the width-sized card box styles so every cell with
	// the same (variant, width) pair reuses one lipgloss.Style. Long board
	// columns otherwise allocate a fresh Style per card per render.
	//
	// Shared across model copies on purpose: it is a map, so the value-receiver
	// render paths all write into the same table.
	cardStyles card.StyleCache

	// statsScreen, logsScreen and insightsScreen are the extracted
	// observability screens. Each is a value type that owns its own cursor,
	// scroll offset, filter selection and loaded projection, so the root
	// Model carries no screen-local state for these routes — it only stores
	// the instances and binds live host deps onto them at dispatch time.
	statsScreen    stats.Screen
	logsScreen     logs.Screen
	insightsScreen insights.Screen

	// help owns scroll state for the keybindings overlay; instantiated
	// once and reused (the overlay is closed/reopened, not destroyed,
	// so scroll state persists across toggles which feels right — users
	// often re-open help after a tangential keystroke).
	help list.Viewport

	// includeArchived flips the active-only task filter on every list view
	// (board/table/graph/logs). Default false (archived hidden); the `A`
	// keybind toggles it. Archived rows render with a dimmed style so the
	// user can still spot them when the toggle is on.
	includeArchived bool

	// markdownRendered is the session-only toggle bound to `M`. Screens
	// paint bodies through a screen-owned markdown.Renderer built from
	// Kit.Markdown; the root only carries the toggle and the Tokens on the kit.
	markdownRendered bool

	// notifications is the catalog of loaded notification cards keyed by
	// slug; the hooks engine names a slug per event and the parent
	// renders that notification as configured. notification is the live model while
	// one is on screen; nil otherwise.
	notifications map[string]config.Notification
	notification  *notificationModel

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
// key in the screen-level modal bar). Comment editing lives in the registered
// Comment detail screen's explicit mode, not in this enum.
type inputMode int

const (
	modeNormal inputMode = iota
	modeComment
	modeMove
)

// topID identifies a top-level navigation zone. Stable external identity lives
// in screenhost descriptors; this integer remains the legacy root adapter's
// compact navigation state until screen extraction completes.
type topID int

const topHome topID = -1

const (
	topTasks topID = iota
	topStats
	topStudio
	topSettings
)

type subID int

const (
	subBoard subID = iota
	subTable
	subGraph
	subPlans
	subStatsGeneral
	subStatsLogs
	subStatsInsights
	subStudioWorkflow
	subStudioCommands
	subStudioPersonas
	subStudioHooks
	subSettingsGeneral
	subSettingsLaws
	subSettingsPersonas
	subSettingsSkills
	subSettingsTemplates
	subSettingsTags
	subSettingsGuards
)

// These compatibility projections are derived from screenRegistry. Existing
// handlers and renderers keep their established data shape while descriptor
// declaration order becomes the only navigation source of truth.
var (
	topOrder  []topID
	subsByTop map[topID][]subID
	topLabels map[topID]string
	subLabels map[subID]string
)

// navState is the addressable navigation key — used to detect view
// changes across an Update tick (so refreshAfterViewChange can re-fetch
// only when the user actually navigated).
type navState struct {
	top topID
	sub subID
}

// firstSub returns the canonical landing sub for a top — what the user
// sees after `shift+tab`/digit-jump. Stats lands on general, Tasks
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
	return true
}

// refreshTickMsg drives the realtime refresh loop — emitted every second
// while the user is on a "live" view (board, table, etc.) and not editing.
// shouldRealtimeRefresh decides whether to honor each tick.
type refreshTickMsg struct{}
