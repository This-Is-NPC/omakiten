package tui

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	hookactions "omakiten/internal/hooks/actions"
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

// NotificationBinding carries the loaded notification catalog into the TUI Model.
// Each notification YAML is one notification card with all behaviour
// (animation, position, dismiss, message) baked in — no per-mode
// presets and no global "active" selection. The hooks engine names
// the slug per event and the parent renders it as configured.
type NotificationBinding struct {
	Notifications map[string]config.Notification
}

func NewModel(ctx context.Context, project domain.ProjectContext, repos Repositories, theme config.Theme, counter token.Counter, badge config.TokenBadgeThresholds, priorities []config.PriorityDefinition, severities []config.SeverityDefinition, notifications NotificationBinding) (Model, error) {
	if counter == nil {
		counter = token.ApproxCounter{}
	}
	yellow, red := badge.Effective()
	priorityPairs := make([]domain.PriorityPair, len(priorities))
	for i, p := range priorities {
		priorityPairs[i] = domain.PriorityPair{ID: p.ID, Value: p.Value, Default: p.Default}
	}
	severityPairs := make([]domain.SeverityPair, len(severities))
	for i, s := range severities {
		severityPairs[i] = domain.SeverityPair{ID: s.ID, Value: s.Value, Default: s.Default}
	}
	model := Model{
		ctx:              ctx,
		project:          project,
		repos:            repos,
		theme:            theme,
		styles:           newStyles(theme),
		counter:          counter,
		tokenBadgeYellow: yellow,
		tokenBadgeRed:    red,
		entityKind:       entityKindLaw,
		// -1 = "no project activity card selected" until the activity zone
		// takes focus (mirrors activityCursor's -1 sentinel for the task feed).
		projectActivityCursor: -1,
		entityCursors:         map[entityKind]int{entityKindLaw: 0, entityKindPersona: 0, entityKindSkill: 0, entityKindTag: 0},
		homePicker:            picker.New(picker.Single),
		priorities:            priorities,
		severities:            severities,
		registry:              domain.NewEnumRegistry(priorityPairs, severityPairs),
		markdown:              newMarkdownRenderer(tokensFromTheme(theme)),
		markdownRendered:      true,
		notifications:         notifications.Notifications,
		// Pre-allocated style-by-kind-by-width cache; value-receiver
		// render paths read + write through this so the
		// lipgloss.Style.Width(N) allocation only fires once per
		// (kind, width) pair across the lifetime of the model. Inner
		// maps lazily fill on first write per kind.
		styleByKindWidth:      map[styleKind]map[int]lipgloss.Style{},
		boardStringCache:      &boardStringCacheEntry{},
		planNetworkBuildCache: &planNetworkBuildCacheEntry{},
		tokenCountCache:       map[uint64]int{},
		subtasks:              cardlist.New(),
		planNetwork:           cardlist.New(),
		activityLines:         linelist.New(),
		logsList:              linelist.New(),
		tableList:             linelist.New(),
		graphList:             linelist.New(),
		graphCursor:           cursorwindow.New(0),
		plansCursor:           cursorwindow.New(0),
		planNetworkCursor:     cursorwindow.New(0),
		settingsGeneralLines:  linelist.New(),
	}
	if reg, err := buildPaletteRegistry(repos); err == nil {
		model.paletteRegistry = reg
	}
	model.taskTitleInput = newTaskTitleInput()
	model.taskDescriptionInput = newTaskDescriptionInput()
	model.taskTagsInput = newTaskTagsInput()
	model.taskParentInput = newTaskParentInput()
	model.commentInput = newCommentInput()
	model.moveInput = newMoveInput()
	detailscreen.SetStyles(model.styles.info)
	if project.ID == 0 {
		// Empty project — open on the multi-project Home picker.
		// Do not call refresh() because every per-project query would 404
		// without a resolved project_id.
		model.top = topHome
		if err := model.loadHome(); err != nil {
			return Model{}, err
		}
		return model, nil
	}
	model.top = topTasks
	model.sub = subBoard
	model.lastProjectRoot = project.RootPath
	if err := model.refresh(); err != nil {
		return Model{}, err
	}
	return model, nil
}

// LastProjectRoot returns the absolute root_path of the most recently opened
// project during this TUI session, or an empty string if the user quit from
// Home without picking a project. The CLI entrypoint reads this after the
// program loop returns to drive the cd-on-exit shell-wrapper handshake.
func (m Model) LastProjectRoot() string {
	return m.lastProjectRoot
}

func (m Model) Init() tea.Cmd {
	return scheduleRefreshTick()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevNav := navState{top: m.top, sub: m.sub}
	if next, cmd, handled := m.dispatchNotification(msg); handled {
		return next, cmd
	}
	switch msg := msg.(type) {
	case refreshAfterViewChangeMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.applyRefreshAfterViewChange(msg)
		return m, nil
	case homeProjectDeleteResultMsg:
		// Asynchronous tail of the Home delete flow — folds the
		// ProjectService.Delete outcome into m.status + reloads the
		// read-model. Running on the main goroutine is fine: the
		// Delete itself already ran on a worker via the tea.Cmd
		// returned by executeHomeProjectDelete; what reaches here is
		// the result envelope, not the blocking IO.
		m.handleHomeProjectDeleteResult(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncFocusedColumnScroll()
		m.syncBoardColScroll()
		m.syncFocusedEntityScroll()
		m.syncActivityScrollToCursor()
		m.palette.SetMaxResultRows(m.paletteResultRowsBudget())
	case refreshTickMsg:
		if m.shouldRealtimeRefresh() {
			// reloadBundleIfChanged is a cheap config-mtime gate (a
			// BundleCache stat + pointer-compare, not a DB hit) that mutates
			// the active snapshot in place. It runs on EVERY honored tick,
			// independent of the DB watermark (F1): a config-file edit moves
			// no DB row, so gating it on data_version meant a config
			// hot-reload (#121) was never picked up until an unrelated DB
			// write happened to advance the watermark. Keep it synchronous on
			// the Update goroutine — it mutates m.
			if _, err := m.reloadBundleIfChanged(); err != nil {
				m.status = err.Error()
			}
			// Cheap change-probe gate for the HEAVY DB reload only: decide the
			// current reload domain set, read the DB watermark once (one PRAGMA
			// data_version on a pinned connection), then skip each domain whose
			// baseline is already current. The idle second — the common case while
			// the user reads a live view — therefore costs exactly one probe query
			// (plus the config-mtime stat above) and zero
			// Snapshot/ListRollups/board-rebuild calls.
			// Self-writes are NOT gated here: they repaint inline via the
			// synchronous m.refresh() on their own write path and the
			// watermark deliberately does not move for them, so this gate
			// never delays a self-write.
			reloadKinds := m.currentReloadKinds()
			if len(reloadKinds) == 0 {
				return m, scheduleRefreshTick()
			}
			reloadDecisions := m.dataVersionChanges(reloadKinds)
			if len(reloadDecisions) == 0 {
				return m, scheduleRefreshTick()
			}
			// Realtime tick is renderer-driven, not user-triggered, so
			// every app-service call it spawns (`MetricsService.Summary`,
			// `Logs.List`) bypasses the activity tracker. Otherwise the
			// log viewer fills with one row per second and pushes real
			// agent activity out of the bounded window. The footer hint
			// already advertises this contract. The suppression is applied
			// to m.ctx for the duration of the capture so the worker closure
			// (which captures m.ctx) inherits it, then restored — the
			// closure runs off-thread but holds its own ctx copy.
			savedCtx := m.ctx
			m.ctx = activity.WithoutTracking(m.ctx)
			// Only the heavy DB read pipeline (Snapshot / ListRollups /
			// activity feed / plan Show) is moved off-thread via
			// realtimeRefreshCmd so a slow query can no longer stall a
			// keystroke. The probed watermark version is carried through the
			// reload msg and committed to that reload domain's baseline only
			// after a successful apply (F2) — never consumed up-front by a
			// reload that might fail. ok=false (no-watermark / probe-error path)
			// means there is no trustworthy version to commit, so pass 0 and
			// applyRealtimeReload leaves the baseline alone.
			//
			// Passive reload latency is state-dependent, not purely
			// time-based: this whole branch is gated behind
			// shouldRealtimeRefresh() (false on Home/modals/help/move/
			// entity screens), so an external edit made while in one of
			// those states is not observed until a live view returns and
			// the next tick fires. Expected, not a missed-reload bug.
			reloads := make([]tea.Cmd, 0, len(reloadDecisions)+1)
			for _, decision := range reloadDecisions {
				var commitVersion int64
				if decision.ok {
					commitVersion = decision.version
				}
				if reload := m.realtimeRefreshCmd(decision.kind, commitVersion, decision.ok); reload != nil {
					reloads = append(reloads, reload)
				}
			}
			m.ctx = savedCtx
			// Batch the off-thread reload with the next tick so input keeps
			// flowing while the worker runs. The reload's result lands as a
			// realtimeReloadMsg that applyRealtimeReload folds in.
			if len(reloads) == 0 {
				return m, scheduleRefreshTick()
			}
			reloads = append(reloads, scheduleRefreshTick())
			return m, tea.Batch(reloads...)
		}
		return m, scheduleRefreshTick()
	case realtimeReloadMsg:
		m.applyRealtimeReload(msg)
		return m, nil
	case editorFinishedMsg:
		m.handleEditorFinished(msg)
		return m, nil
	case palette.DismissMsg:
		m.paletteOpen = false
		return m, nil
	case paletteSearchResultMsg:
		// Async tail of dispatchPaletteSearch. Drop the result
		// silently when the user closed the palette in the
		// meantime — SetResults / SetStatus on a closed overlay
		// would leak stale text into the next open.
		if !m.paletteOpen {
			return m, nil
		}
		if len(msg.hits) > 0 {
			m.palette.SetResults(msg.hits)
		} else {
			m.palette.SetStatus(msg.status)
		}
		return m, nil
	case palette.OpenHitMsg:
		return m, m.dispatchOpenHit(msg.Hit)
	case palette.SubmitMsg:
		return m, m.dispatchTrick(msg.Token)
	case palette.SearchMsg:
		return m, m.dispatchPaletteSearch(msg.Query)
	case tea.KeyMsg:
		if m.paletteOpen {
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		}
		if msg.String() == "ctrl+k" && m.canOpenPalette() {
			m.palette = palette.NewModel()
			m.palette.SetMaxResultRows(m.paletteResultRowsBudget())
			m.paletteOpen = true
			return m, nil
		}
		if m.helpOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "a":
				m.helpAll = !m.helpAll
				m.help = m.help.WithScroll(0)
			case "?", "q":
				m.helpOpen = false
				m.helpAll = false
				m.help = m.help.WithScroll(0)
			default:
				// Delegate scroll keys (j/k/pgup/pgdn/g/G) and esc to the
				// embedded viewport sub-model. Esc surfaces as EventCancel
				// which we treat as "close the overlay".
				m.help, _ = m.help.Update(msg, m.helpViewportRows())
				if m.help.LastEvent() == viewport.EventCancel {
					m.helpOpen = false
					m.helpAll = false
					m.help = m.help.WithScroll(0)
				}
			}
			return m, nil
		}
		if msg.String() == "?" && m.mode == modeNormal && !m.commentScreenEditing {
			m.helpOpen = true
			m.helpAll = false
			m.help.Scroll = 0
			return m, nil
		}
		if m.mode != modeNormal {
			return m.updateInput(msg)
		}
		if m.commentScreenOpen {
			return m.updateCommentScreen(msg)
		}
		if m.descriptionScreenOpen {
			return m.updateDescriptionScreen(msg)
		}
		if m.planGoalScreenOpen {
			return m.updatePlanGoalScreen(msg)
		}
		if m.projectFormScreenOpen {
			return m.updateProjectFormScreen(msg)
		}
		if m.taskScreen != taskScreenClosed {
			return m.updateTaskScreen(msg)
		}

		if m.entityScreen != entityScreenClosed {
			return m.updateEntityScreen(msg)
		}

		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		if m.onHome() {
			cmd := m.handleHomeKey(msg)
			return m, cmd
		}
		// The project-view screen owns tab (panel toggle) and the scroll
		// vocabulary before handleCommonKey can rebind them to zone-cycle /
		// refresh, so its handler runs first and only the keys it does not
		// claim (ctrl+o/ctrl+h/ctrl+p back-nav, esc, quit) fall through.
		if m.sub == subProjectView && m.handleProjectKey(msg) {
			return m, nil
		}
		if m.handleCommonKey(msg) {
			return m, m.refreshAfterViewChangeCmd(prevNav)
		}
		switch m.sub {
		case subBoard:
			m.handleBoardKey(msg)
		case subTable:
			m.handleListKey(msg)
		case subGraph:
			m.handleGraphKey(msg)
		case subPlans:
			if m.planNetworkOpen {
				m.handlePlanNetworkKey(msg)
			} else {
				m.handlePlansKey(msg)
			}
		case subStatsGeneral:
			m.handleStatsKey(msg)
		case subStatsLogs:
			m.handleLogsKey(msg)
		case subStatsInsights:
			m.handleInsightsKey(msg)
		case subSettingsGeneral, subSettingsGuards:
			if cmd := m.handleSettingsGeneralKey(msg); cmd != nil {
				return m, cmd
			}
		case subSettingsLaws, subSettingsPersonas, subSettingsSkills, subSettingsTemplates, subSettingsTags:
			if cmd := m.handleConfigKey(msg); cmd != nil {
				return m, cmd
			}
		}
	}
	return m, m.refreshAfterViewChangeCmd(prevNav)
}

// newTaskTitleInput is the canonical textinput config for the task title
// row of the create/edit form. Width is set later from terminal geometry;
// a zero CharLimit keeps long titles editable, and the leading prompt is
// suppressed because the form already kickers the field with `> // TITLE`.
func newTaskTitleInput() textinput.Model {
	t := textinput.New()
	t.Prompt = ""
	t.CharLimit = 0
	return t
}

// newTaskTagsInput owns the §E Tags section. Single-line CSV input
// (`tag1, tag2, tag3`); split on comma at save. Same Prompt/CharLimit
// shape as the title input so the visual baseline matches.
func newTaskTagsInput() textinput.Model {
	t := textinput.New()
	t.Prompt = ""
	t.CharLimit = 0
	return t
}

// newTaskParentInput owns the §E Parent section. Single-line integer
// id (blank = root). Validation happens at blur time (lookup → exists +
// same project) and at save (anti-cycle); the field stays a free
// textinput so the caret behaves like the other sections.
func newTaskParentInput() textinput.Model {
	t := textinput.New()
	t.Prompt = ""
	t.CharLimit = 0
	return t
}

// newTaskDescriptionInput is the canonical textarea config for the
// description row. Line numbers stay off (the form is not a code editor)
// and the soft prompt is blanked so the visible text starts at column 0,
// matching the title row visually. KeyMap.InsertNewline accepts both a
// bare Enter (the form's own save key is ctrl+s, so Enter is free for
// newlines) and the modifier-Enter set so terminals that emit
// alt/shift/ctrl+j-Enter also insert a newline natively — the prior
// hand-rolled InsertString shim is gone.
func newTaskDescriptionInput() textarea.Model {
	t := textarea.New()
	t.Prompt = ""
	t.ShowLineNumbers = false
	t.CharLimit = 0
	t.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("enter", "shift+enter", "alt+enter", "ctrl+j", "ctrl+m"),
		key.WithHelp("enter · alt+enter · shift+enter", "insert newline"),
	)
	clearTextareaCursorLineBackground(&t)
	return t
}

// newCommentInput is the textarea reused by the comment-add and
// comment-edit modal flows. Same defaults as the description field so
// the two modal surfaces feel uniform — including the modifier-Enter
// rebind so updateInput can keep treating bare Enter as save.
func newCommentInput() textarea.Model {
	t := textarea.New()
	t.Prompt = ""
	t.ShowLineNumbers = false
	t.CharLimit = 0
	bindings := newCommentInputBindings()
	t.KeyMap.InsertNewline = bindings.InsertNewline
	clearTextareaCursorLineBackground(&t)
	return t
}

// clearTextareaCursorLineBackground neutralises the textarea's default
// CursorLine background so the reverse-video cursor block stays visible
// when focused. Without this, the focused-style adaptive Background swaps
// over the cursor cell at render time and the caret disappears into the
// line — the user reported "cursor only on title, not description".
// Applied to both focused and blurred styles for symmetry, even though
// only the focused state renders the cursor.
func clearTextareaCursorLineBackground(t *textarea.Model) {
	t.FocusedStyle.CursorLine = lipgloss.NewStyle()
	t.BlurredStyle.CursorLine = lipgloss.NewStyle()
}

// newMoveInput is the canonical textinput used by the modal move flow
// (`m` then type a bucket key). Prompt is blanked because the chrome
// already labels the field with "Target bucket key:"; CharLimit stays
// zero so user-defined bucket slugs of any length round-trip.
func newMoveInput() textinput.Model {
	t := textinput.New()
	t.Prompt = ""
	t.CharLimit = 0
	return t
}

func scheduleRefreshTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

type realtimeReloadDecision struct {
	kind    realtimeReloadKind
	version int64
	ok      bool
}

// dataVersionChanges probes the DB change watermark once for the current tick
// and returns the reload domains whose baselines lag that one probed version.
// It does NOT mutate any domain baseline: the probed version is carried through
// each off-thread reload and committed only after that domain successfully
// applies (applyRealtimeReload). This keeps one failed domain from consuming its
// pending write while allowing another domain from the same tick to advance.
//
// The probed version is meaningful only when ok is true. With no Watermark
// reader, or when the probe errors, every requested domain reloads as a fallback
// with ok=false so no baseline is committed. That preserves the old
// reload-every-tick behaviour for fixtures and keeps probe errors from wedging a
// live view. With a good probe, only domains with no established baseline or a
// different baseline are returned.
func (m *Model) dataVersionChanges(kinds []realtimeReloadKind) []realtimeReloadDecision {
	if len(kinds) == 0 {
		return nil
	}
	if m.repos.Watermark == nil {
		decisions := make([]realtimeReloadDecision, 0, len(kinds))
		for _, kind := range kinds {
			if kind != realtimeReloadNone {
				decisions = append(decisions, realtimeReloadDecision{kind: kind})
			}
		}
		return decisions
	}
	version, err := m.repos.Watermark.DataVersion(m.ctx)
	if err != nil {
		// Surface the failure but treat every requested domain as changed so
		// the reloads still run. ok=false prevents baseline commits.
		m.status = err.Error()
		decisions := make([]realtimeReloadDecision, 0, len(kinds))
		for _, kind := range kinds {
			if kind != realtimeReloadNone {
				decisions = append(decisions, realtimeReloadDecision{kind: kind})
			}
		}
		return decisions
	}
	decisions := make([]realtimeReloadDecision, 0, len(kinds))
	for _, kind := range kinds {
		if kind == realtimeReloadNone {
			continue
		}
		baseline, synced := m.dataVersionBaseline(kind)
		if !synced || version != baseline {
			decisions = append(decisions, realtimeReloadDecision{kind: kind, version: version, ok: true})
		}
	}
	return decisions
}

func (m Model) dataVersionBaseline(kind realtimeReloadKind) (int64, bool) {
	if kind == realtimeReloadNone || m.dataVersionBaselines == nil {
		return 0, false
	}
	version, ok := m.dataVersionBaselines[kind]
	return version, ok
}

// commitDataVersion advances the named domain's watermark baseline after the
// reload it gated has been successfully applied. Map presence is the synced bit;
// the first-probe baseline is now committed downstream of apply (F2).
func (m *Model) commitDataVersion(kind realtimeReloadKind, version int64) {
	if kind == realtimeReloadNone {
		return
	}
	if m.dataVersionBaselines == nil {
		m.dataVersionBaselines = map[realtimeReloadKind]int64{}
	}
	m.dataVersionBaselines[kind] = version
}

func (m *Model) nextRealtimeReloadGen(kind realtimeReloadKind) uint64 {
	if kind == realtimeReloadNone {
		return 0
	}
	if m.realtimeReloadGen == nil {
		m.realtimeReloadGen = map[realtimeReloadKind]uint64{}
	}
	m.realtimeReloadGen[kind]++
	return m.realtimeReloadGen[kind]
}

// realtimeReloadScopeMatches reports whether an entity-scoped reload still
// targets the current context. activity is scoped to a task id, plan-show to a
// plan slug; both are captured when the worker cmd is built and may go stale if
// the user navigates synchronously before the slow worker folds. Unscoped
// payloads (scope zero/empty — bundle/stats/logs, or test-constructed msgs)
// always match so only entity reloads are gated.
func (m Model) realtimeReloadScopeMatches(r realtimeReloadMsg) bool {
	switch r.kind {
	case realtimeReloadActivity:
		return r.scopeTaskID == 0 || r.scopeTaskID == m.taskID
	case realtimeReloadPlanShow:
		return r.scopeSlug == "" || r.scopeSlug == m.planNetworkShow.Plan.Slug
	default:
		return true
	}
}

func (m Model) lastAppliedRealtimeReloadGen(kind realtimeReloadKind) uint64 {
	if kind == realtimeReloadNone || m.lastAppliedReloadGen == nil {
		return 0
	}
	return m.lastAppliedReloadGen[kind]
}

func (m *Model) markRealtimeReloadApplied(kind realtimeReloadKind, gen uint64) {
	if kind == realtimeReloadNone || gen == 0 {
		return
	}
	if m.lastAppliedReloadGen == nil {
		m.lastAppliedReloadGen = map[realtimeReloadKind]uint64{}
	}
	m.lastAppliedReloadGen[kind] = gen
}

func (m Model) shouldRealtimeRefresh() bool {
	if m.onHome() {
		// Home reads cross-project metadata (tags, pending counts) — refresh
		// is driven by ctrl+h / startup, not by the per-project tick.
		return false
	}
	if m.helpOpen || m.mode != modeNormal || m.moveMode {
		return false
	}
	// The Ctrl+K palette is a keyboard-input overlay that can sit over the
	// board / plan-network view (canOpenPalette only blocks task/entity
	// screens, not those). A passive reload underneath an open palette is an
	// unguarded input-mode refresh — mirror canOpenPalette and suppress it.
	if m.paletteOpen {
		return false
	}
	if m.entityScreen != entityScreenClosed {
		return false
	}
	// The single-task view refreshes only in its read-only state. taskScreenEdit
	// owns the title/description/field inputs; reloading under it would discard a
	// half-typed edit. The comment / description / plan-goal / project-form
	// overlays own keyboard focus on top of whatever view is open and must not be
	// reloaded out from under the user either.
	if m.taskScreen == taskScreenEdit {
		return false
	}
	if m.commentScreenOpen || m.descriptionScreenOpen || m.planGoalScreenOpen || m.projectFormScreenOpen {
		return false
	}
	return true
}

// currentReloadKinds is the pure view-state gate for the realtime tick. It maps
// every visible surface to the reload domain(s) whose baselines the tick should
// compare before building worker cmds. Board, table, graph, and entity-family
// projections all share the bundle domain; task detail, plan show, stats, and
// logs each advance independently after their own successful apply. The open
// plan-network consumes both bundle-backed surrounding state and the focused
// PlanShow projection, so it subscribes to both without letting either baseline
// advance the other.
func (m Model) currentReloadKinds() []realtimeReloadKind {
	if m.taskScreen == taskScreenView && m.taskID > 0 {
		return []realtimeReloadKind{realtimeReloadActivity}
	}
	if m.planNetworkOpen {
		if m.planNetworkShow.Plan.Slug == "" {
			return nil
		}
		return []realtimeReloadKind{realtimeReloadBundle, realtimeReloadPlanShow}
	}
	if m.top == topStats && m.sub == subStatsGeneral {
		return []realtimeReloadKind{realtimeReloadStats}
	}
	if m.top == topStats && m.sub == subStatsLogs {
		return []realtimeReloadKind{realtimeReloadLogs}
	}
	if m.top == topStats && m.sub == subStatsInsights {
		return []realtimeReloadKind{realtimeReloadInsights}
	}
	return []realtimeReloadKind{realtimeReloadBundle}
}

// canOpenPalette gates the global Ctrl+K binding so the palette
// never steals focus from another modal input. The matrix mirrors
// shouldRealtimeRefresh's "no modal active" check plus the
// description / comment overlays (which are not background-refresh
// gates but still own keyboard focus when open).
func (m Model) canOpenPalette() bool {
	if m.paletteOpen {
		return false
	}
	if m.helpOpen {
		return false
	}
	if m.mode != modeNormal {
		return false
	}
	if m.commentScreenOpen || m.descriptionScreenOpen || m.planGoalScreenOpen || m.projectFormScreenOpen {
		return false
	}
	if m.taskScreen != taskScreenClosed {
		return false
	}
	if m.entityScreen != entityScreenClosed {
		return false
	}
	if m.moveMode {
		return false
	}
	return true
}

// refreshAfterViewChangeCmd reacts to a sub-tab nav transition. Light
// routes (home, stats, logs) keep running on the Update goroutine
// because their workloads are bounded; the board / table / graph /
// plans routes hand the heavy read pipeline (TUIQueryService.Snapshot
// + PlanService.ListRollups) off to a worker via tea.Cmd so a keystroke
// returns immediately. The previous view stays rendered until the
// resulting refreshAfterViewChangeMsg lands and the Update handler
// folds the loaded slices into the model.
func (m *Model) refreshAfterViewChangeCmd(prev navState) tea.Cmd {
	if m.top == prev.top && m.sub == prev.sub {
		return nil
	}
	// Settings › General and Settings › Guards share `settingsGeneralLines`
	// as their scroll state. When the user flips between them the linelist
	// otherwise carries an offset clamped against the previous body — visible
	// as a Guards view that opens mid-matrix because the General offset was
	// large. Refresh against the new body and rewind to the top so each sub
	// switch starts at row 0.
	if m.top == topSettings && (m.sub == subSettingsGeneral || m.sub == subSettingsGuards) {
		m.refreshSettingsGeneralLines()
		m.settingsGeneralLines = m.settingsGeneralLines.ScrollBy(-(1 << 20))
	}
	if m.onHome() {
		if err := m.loadHome(); err != nil {
			m.status = err.Error()
		}
		return nil
	}
	if m.top == topStats && m.sub == subStatsLogs {
		if err := m.refreshActivityLogs(); err != nil {
			m.status = err.Error()
		}
		return nil
	}
	if m.top == topStats && m.sub == subStatsGeneral {
		if err := m.refreshStats(); err != nil {
			m.status = err.Error()
		}
		return nil
	}
	if m.top == topStats && m.sub == subStatsInsights {
		if err := m.refreshInsights(); err != nil {
			m.status = err.Error()
		}
		return nil
	}
	return m.refreshHeavyAfterViewChangeCmd()
}

// viewChangeRefreshRegistry tracks the function pointer of each cmd
// produced by refreshHeavyAfterViewChangeCmd so test helpers can
// distinguish the async view-change refresh from every other cmd a key
// dispatch returns (write IO, picker pickup, tick reschedule) without
// having to execute the cmd to inspect its message type. Production
// code never reads this map; applyRefreshAfterViewChange does call
// Delete via the cmd pointer carried on the message so a long-running
// TUI session does not leak one entry per nav.
var viewChangeRefreshRegistry sync.Map

func registerViewChangeRefreshCmd(cmd tea.Cmd) uintptr {
	if cmd == nil {
		return 0
	}
	key := reflect.ValueOf(cmd).Pointer()
	viewChangeRefreshRegistry.Store(key, struct{}{})
	return key
}

func isViewChangeRefreshCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := viewChangeRefreshRegistry.Load(reflect.ValueOf(cmd).Pointer())
	return ok
}

// refreshHeavyAfterViewChangeCmd captures every input the worker needs
// (snapshot pointer, ctx, project, sort, services) on the main goroutine
// and returns a tea.Cmd whose closure runs the read pipeline. The result
// message carries the loaded TUISnapshot plus the optional plan rollups
// and resolved languages so the fold path is pure assignment — no fresh
// IO on the Update goroutine.
func (m *Model) refreshHeavyAfterViewChangeCmd() tea.Cmd {
	var preservedTaskID int64
	if task, ok := m.selectedTask(); ok {
		preservedTaskID = task.ID
	}
	views := m.activeViewSettings()
	m.views = views
	cfgSnap := m.repos.activeSnapshot()
	query := app.NewTUIQueryService(m.repos.Tasks, cfgSnap, m.repos.Dependencies, m.repos.Comments, m.repos.Tags)
	var plansSvc *app.PlanService
	if m.repos.Plans != nil {
		plansSvc = app.NewPlanServiceWithSnapshot(m.repos.Plans, cfgSnap)
	}
	langs := m.languages
	if cfgSnap != nil {
		langs = cfgSnap.Settings().EffectiveLanguages()
	}
	ctx := m.ctx
	project := m.project
	sort := domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}
	archived := m.includeArchived
	// Self-referential capture: the closure observes its own cmd value
	// so the msg carries the registry key, letting
	// applyRefreshAfterViewChange Delete the entry once the fold runs.
	// Without that, every nav would add a new sync.Map entry that never
	// drops.
	var cmd tea.Cmd
	cmd = tea.Cmd(func() tea.Msg {
		result := refreshAfterViewChangeMsg{preservedTaskID: preservedTaskID, langs: langs, cmdKey: reflect.ValueOf(cmd).Pointer()}
		s, err := query.Snapshot(ctx, project, sort, app.SnapshotOptions{IncludeArchived: archived})
		if err != nil {
			result.err = err
			return result
		}
		result.snap = s
		result.snapValid = true
		if plansSvc != nil {
			if rollups, perr := plansSvc.ListRollups(ctx, project); perr == nil {
				result.plans = rollups
				result.plansValid = true
			}
		}
		return result
	})
	registerViewChangeRefreshCmd(cmd)
	return cmd
}

// refreshAfterViewChangeMsg is the worker-to-main envelope for the
// async view-change refresh. snapValid / plansValid disambiguate "empty
// slice because the project has none" from "the worker did not load
// this kind of data" — the fold path only overwrites m.plans when the
// worker actually queried it.
type refreshAfterViewChangeMsg struct {
	snap            app.TUISnapshot
	snapValid       bool
	plans           []app.PlanRollup
	plansValid      bool
	langs           config.LanguageSettings
	preservedTaskID int64
	err             error
	// cmdKey carries the function-pointer the worker cmd was registered
	// under so applyRefreshAfterViewChange can Delete the registry entry
	// on fold. Zero when the msg was synthesised by code that bypassed
	// refreshHeavyAfterViewChangeCmd (e.g. test helpers).
	cmdKey uintptr
}

// applyRefreshAfterViewChange folds the worker's result into the model.
// Pure assignment — no IO — so it is safe to run on the Update
// goroutine even when the worker is still running a subsequent tick.
func (m *Model) applyRefreshAfterViewChange(r refreshAfterViewChangeMsg) {
	if r.cmdKey != 0 {
		viewChangeRefreshRegistry.Delete(r.cmdKey)
	}
	if !r.snapValid {
		return
	}
	snap := r.snap
	m.tasks = snap.Tasks
	m.workflow = snap.Workflow
	m.dependencies = snap.Dependencies
	m.comments = snap.Comments
	m.laws = snap.Laws
	m.skills = snap.Skills
	m.personas = snap.Personas
	m.templates = snap.Templates
	m.tags = snap.AllTags
	m.taskTagsMap = snap.TaskTagsByID
	m.metrics = m.computeMetrics(0)
	m.languages = r.langs
	if r.plansValid {
		m.plans = r.plans
	}
	m.invalidateBoardCaches()
	m.rebuildBoardCaches()
	m.clampPlanCursor()
	m.clampGraphCursor()
	m.clampSelection()
	m.clampCardIdx()
	m.clampEntityCursor()
	m.syncSelectedFromBoard()
	if r.preservedTaskID > 0 {
		m.selectTaskByID(r.preservedTaskID)
	}
}

// realtimeReloadKind names the reload domain the changed-tick reload hydrates.
// The kind is decided on the Update goroutine (where view state is read
// race-free), keys the per-domain data_version baseline, and travels into the
// worker closure so the worker runs only the IO that view needs — never the
// whole bundle pipeline when only a task feed or a plan projection is on screen.
type realtimeReloadKind int

const (
	realtimeReloadNone realtimeReloadKind = iota
	realtimeReloadBundle
	realtimeReloadActivity
	realtimeReloadPlanShow
	realtimeReloadStats
	realtimeReloadLogs
	realtimeReloadInsights
)

// realtimeReloadMsg is the worker-to-main envelope for the changed-tick
// reload. It is the tick counterpart of refreshAfterViewChangeMsg: the worker
// cmd does only IO and packs the result here; applyRealtimeReload folds it into
// the model on the Update goroutine. Each payload carries its own *valid flag
// so the fold overwrites only what this kind actually loaded — a torn read
// cannot leak one view's slice into another. status carries a best-effort
// error string (plan/stats/logs paths swallow their error into the status bar
// exactly as the old inline code did) while err is the hard failure the board
// path surfaces.
type realtimeReloadMsg struct {
	kind   realtimeReloadKind
	err    error
	status string
	cmdKey uintptr

	// gen is the monotonic generation stamped when this reload cmd was built
	// on the Update goroutine. applyRealtimeReload drops any msg whose gen is
	// older than the latest already-applied generation so a slow worker can
	// never overwrite a newer snapshot with a staler one (F3).
	gen uint64

	// dataVersion is the DB watermark probed by the tick that spawned this
	// reload; committed to this kind's baseline only on a successful apply (F2).
	// dataVersionValid is false on the no-watermark / probe-error path, where
	// there is no trustworthy baseline to commit.
	dataVersion      int64
	dataVersionValid bool

	// scope identity captured on the Update goroutine when the cmd was built.
	// applyRealtimeReload drops an entity-scoped fold whose captured scope no
	// longer matches the current context (F4). The per-domain gen guard (F3)
	// only orders async reloads *within* a domain; it cannot see a synchronous
	// navigation (open another task, switch plan) that swapped the active entity
	// between request and apply, so a stale slow worker for the previous entity
	// would otherwise clobber the new view. Zero/empty means unscoped
	// (test-constructed or non-entity kind) and is never gated.
	scopeTaskID int64  // realtimeReloadActivity: the task whose feed this loaded
	scopeSlug   string // realtimeReloadPlanShow: the plan slug this loaded

	// bundle payload (kind == realtimeReloadBundle)
	snap       app.TUISnapshot
	snapValid  bool
	plans      []app.PlanRollup
	plansValid bool
	langs      config.LanguageSettings

	// activity payload (kind == realtimeReloadActivity)
	activity      []domain.Event
	activityForID int64
	activityValid bool
	anchorID      int64

	// plan-show payload (kind == realtimeReloadPlanShow)
	planShow  app.PlanShow
	planValid bool

	// stats payload (kind == realtimeReloadStats)
	statsSummary domain.MetricsSummary
	statsValid   bool

	// logs payload (kind == realtimeReloadLogs)
	events      []domain.EventRow
	eventCounts map[domain.EventCategory]int
	logsValid   bool

	// insights payload (kind == realtimeReloadInsights)
	insights      domain.Insights
	insightsValid bool
}

// realtimeRefreshCmd builds the off-thread changed-tick reload. It runs on the
// Update goroutine, reads the current view + every input the worker needs, and
// returns a tea.Cmd whose closure does ONLY IO before packing a
// realtimeReloadMsg — the closure never touches m (Council/Olivier flagged the
// former inline realtimeRefresh, which mutated m under the tick, as a data
// race). applyRealtimeReload performs the assignment back on the Update
// goroutine. Mirrors the proven refreshHeavyAfterViewChangeCmd packer: same
// input-capture-then-pure-IO shape and the same self-referential registry key
// so applyRealtimeReload can drop the entry on fold.
//
// View scope: the kind decided here picks the IO. The single-task view loads
// only its activity feed — the board snapshot is NOT rebuilt, because
// renderTaskScreen fully occludes the board (a board rebuild under it is pure
// waste). Board / plan / stats / logs each reload only their own projection.
// Returns nil when nothing is reloadable for the current view (e.g. an empty
// plan-network), leaving the tick a no-op.
func (m *Model) realtimeRefreshCmd(kind realtimeReloadKind, dataVersion int64, dataVersionValid bool) tea.Cmd {
	ctx := m.ctx
	project := m.project

	// Per-domain generation captured on the Update goroutine: stamped onto every
	// msg this cmd produces so applyRealtimeReload can drop a stale arrival within
	// the same domain (F3). The probed watermark version rides along on the same
	// msgs and is committed only after a successful apply (F2).
	newStamp := func() func(realtimeReloadMsg) realtimeReloadMsg {
		gen := m.nextRealtimeReloadGen(kind)
		return func(r realtimeReloadMsg) realtimeReloadMsg {
			r.gen = gen
			r.dataVersion = dataVersion
			r.dataVersionValid = dataVersionValid
			return r
		}
	}

	switch kind {
	case realtimeReloadActivity:
		// The single-task view is layered over the board; renderTaskScreen takes
		// precedence so the board underneath is invisible. Scope the reload to the
		// activity feed only — skip the bundle snapshot entirely.
		if m.taskID <= 0 || m.repos.Events == nil {
			return nil
		}
		stamp := newStamp()
		taskID := m.taskID
		order := m.activeViewSettings().TaskActivity.Sort.Order
		anchorID := m.focusedActivityEventID()
		events := m.repos.Events
		var cmd tea.Cmd
		cmd = func() tea.Msg {
			result := stamp(realtimeReloadMsg{kind: kind, anchorID: anchorID, scopeTaskID: taskID, cmdKey: reflect.ValueOf(cmd).Pointer()})
			rows, err := events.ListTaskActivity(ctx, project.ID, taskID, order)
			if err != nil {
				result.err = err
				return result
			}
			result.activity = rows
			result.activityForID = taskID
			result.activityValid = true
			return result
		}
		registerRealtimeReloadCmd(cmd)
		return cmd
	case realtimeReloadPlanShow:
		// Plan-network open (and not under a task view): reload only the plan
		// projection.
		if m.repos.Plans == nil || m.planNetworkShow.Plan.Slug == "" {
			return nil
		}
		stamp := newStamp()
		slug := m.planNetworkShow.Plan.Slug
		plansSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
		var cmd tea.Cmd
		cmd = func() tea.Msg {
			result := stamp(realtimeReloadMsg{kind: kind, scopeSlug: slug, cmdKey: reflect.ValueOf(cmd).Pointer()})
			show, err := plansSvc.Show(ctx, project, slug)
			if err != nil {
				// Match the old reloadPlanNetwork: surface to the status bar,
				// not a hard reload failure.
				result.status = err.Error()
				return result
			}
			result.planShow = show
			result.planValid = true
			return result
		}
		registerRealtimeReloadCmd(cmd)
		return cmd
	case realtimeReloadStats:
		// Stats › general.
		if m.repos.Metrics == nil {
			return nil
		}
		stamp := newStamp()
		period := m.statsPeriod
		if period == "" {
			period = "30d"
		}
		metrics := m.repos.Metrics
		var cmd tea.Cmd
		cmd = func() tea.Msg {
			result := stamp(realtimeReloadMsg{kind: kind, cmdKey: reflect.ValueOf(cmd).Pointer()})
			summary, err := metrics.Summary(ctx, project, period, 0)
			if err != nil {
				result.err = err
				return result
			}
			result.statsSummary = summary
			result.statsValid = true
			return result
		}
		registerRealtimeReloadCmd(cmd)
		return cmd
	case realtimeReloadLogs:
		// Stats › logs.
		if m.repos.Events == nil {
			return nil
		}
		stamp := newStamp()
		views := m.activeViewSettings()
		filter := domain.EventFilter{
			ProjectID:  project.ID,
			Categories: m.logsCategoryFilter(),
			Since:      m.logsSinceFloor(views.Logs.WindowDays),
			Limit:      views.Logs.Limit,
			Order:      views.Logs.Sort.Order,
		}
		since := filter.Since
		events := m.repos.Events
		var cmd tea.Cmd
		cmd = func() tea.Msg {
			result := stamp(realtimeReloadMsg{kind: kind, cmdKey: reflect.ValueOf(cmd).Pointer()})
			rows, err := events.ListEvents(ctx, filter)
			if err != nil {
				result.err = err
				return result
			}
			counts, err := events.EventCategoryCounts(ctx, project.ID, since)
			if err != nil {
				result.err = err
				return result
			}
			result.events = rows
			result.eventCounts = counts
			result.logsValid = true
			return result
		}
		registerRealtimeReloadCmd(cmd)
		return cmd
	case realtimeReloadInsights:
		// Stats › insights — the intelligence layer. Mirrors the stats
		// path: bounded read, off-thread, packed into the msg; the apply
		// folds it into m.insights on the Update goroutine.
		if m.repos.Insights == nil {
			return nil
		}
		stamp := newStamp()
		insightsSvc := m.repos.Insights
		projectID := project.ID
		stuckBuckets := insightsStuckBuckets(m.workflow)
		var cmd tea.Cmd
		cmd = func() tea.Msg {
			result := stamp(realtimeReloadMsg{kind: kind, cmdKey: reflect.ValueOf(cmd).Pointer()})
			ins, err := insightsSvc.Today(ctx, project, projectID, 0, stuckBuckets)
			if err != nil {
				result.err = err
				return result
			}
			result.insights = ins
			result.insightsValid = true
			return result
		}
		registerRealtimeReloadCmd(cmd)
		return cmd
	case realtimeReloadBundle:
		stamp := newStamp()
		// Home is gated out by shouldRealtimeRefresh; everything else in the
		// board family (board / table / graph / plans / entity) reloads the heavy
		// read pipeline (snapshot + rollups) off-thread.
		views := m.activeViewSettings()
		cfgSnap := m.repos.activeSnapshot()
		query := app.NewTUIQueryService(m.repos.Tasks, cfgSnap, m.repos.Dependencies, m.repos.Comments, m.repos.Tags)
		var plansSvc *app.PlanService
		if m.repos.Plans != nil {
			plansSvc = app.NewPlanServiceWithSnapshot(m.repos.Plans, cfgSnap)
		}
		langs := m.languages
		if cfgSnap != nil {
			langs = cfgSnap.Settings().EffectiveLanguages()
		}
		sort := domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}
		archived := m.includeArchived
		var preservedTaskID int64
		if task, ok := m.selectedTask(); ok {
			preservedTaskID = task.ID
		}
		var cmd tea.Cmd
		cmd = func() tea.Msg {
			result := stamp(realtimeReloadMsg{kind: kind, langs: langs, anchorID: preservedTaskID, cmdKey: reflect.ValueOf(cmd).Pointer()})
			s, err := query.Snapshot(ctx, project, sort, app.SnapshotOptions{IncludeArchived: archived})
			if err != nil {
				result.err = err
				return result
			}
			result.snap = s
			result.snapValid = true
			if plansSvc != nil {
				if rollups, perr := plansSvc.ListRollups(ctx, project); perr == nil {
					result.plans = rollups
					result.plansValid = true
				}
			}
			return result
		}
		registerRealtimeReloadCmd(cmd)
		return cmd
	default:
		return nil
	}
}

// realtimeReloadRegistry mirrors viewChangeRefreshRegistry: it tracks the
// function pointer of each cmd realtimeRefreshCmd produces so test helpers can
// recognise the async tick reload without executing it, and so
// applyRealtimeReload can drop the entry on fold (otherwise a long session
// leaks one map entry per changed tick).
var realtimeReloadRegistry sync.Map

func registerRealtimeReloadCmd(cmd tea.Cmd) uintptr {
	if cmd == nil {
		return 0
	}
	key := reflect.ValueOf(cmd).Pointer()
	realtimeReloadRegistry.Store(key, struct{}{})
	return key
}

func isRealtimeReloadCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := realtimeReloadRegistry.Load(reflect.ValueOf(cmd).Pointer())
	return ok
}

// applyRealtimeReload folds the worker's view-scoped result into the model.
// Pure assignment + cursor reanchoring — no IO — so it is safe on the Update
// goroutine even while a later tick's worker is still running. It is the
// apply-half of the former inline realtimeRefresh; every m mutation that used
// to run on the worker side now lives here.
func (m *Model) applyRealtimeReload(r realtimeReloadMsg) {
	if r.cmdKey != 0 {
		realtimeReloadRegistry.Delete(r.cmdKey)
	}
	// F3 — stale-generation guard. Two reloads for the same domain can be in
	// flight (consecutive watermark-moving ticks + a slow worker) and complete
	// out of order; folding an older snapshot over a newer one would briefly
	// regress the view. Drop any msg whose generation is older-or-equal to the
	// latest one already applied for that domain. gen==0 means a legacy/unstamped
	// msg (test-constructed) — never gated. The committed-on-apply contract
	// (below) means a dropped stale msg also never advances the domain watermark
	// baseline (F2 interaction): its version is not committed, so the newer reload
	// still owns that domain's watermark.
	if r.gen != 0 && r.gen <= m.lastAppliedRealtimeReloadGen(r.kind) {
		return
	}
	// F4 — stale-scope guard. The gen guard above only orders async reloads
	// within a domain; a synchronous navigation (open another task, switch the
	// focused plan) swaps the active entity WITHOUT advancing the domain gen, so
	// a slow worker captured for the previous entity can still pass F3. Drop the
	// fold when the captured scope no longer matches the current context — and do
	// NOT commit the watermark/generation (fall through to neither markApplied nor
	// commitDataVersion), so the next tick re-observes and reloads the current
	// entity cleanly. Zero/empty scope is unscoped (legacy/test/non-entity) and is
	// never gated.
	if !m.realtimeReloadScopeMatches(r) {
		return
	}
	if r.err != nil {
		// Hard failure: surface it and do NOT commit the watermark or the
		// generation (F2). The baseline stays where it was so the next tick
		// re-observes the same external write and retries the reload.
		m.status = r.err.Error()
		return
	}
	if r.status != "" {
		m.status = r.status
	}
	// applied tracks whether the payload actually folded in; an empty/torn
	// payload (!*Valid) leaves the view untouched, so it must NOT commit the
	// watermark — the write is still pending and the next tick must retry it.
	applied := false
	switch r.kind {
	case realtimeReloadBundle:
		if r.snapValid {
			snap := r.snap
			m.tasks = snap.Tasks
			m.workflow = snap.Workflow
			m.dependencies = snap.Dependencies
			m.comments = snap.Comments
			m.laws = snap.Laws
			m.skills = snap.Skills
			m.personas = snap.Personas
			m.templates = snap.Templates
			m.tags = snap.AllTags
			m.taskTagsMap = snap.TaskTagsByID
			m.metrics = m.computeMetrics(0)
			m.languages = r.langs
			if r.plansValid {
				m.plans = r.plans
			}
			m.invalidateBoardCaches()
			m.rebuildBoardCaches()
			m.clampPlanCursor()
			m.clampGraphCursor()
			m.clampSelection()
			m.clampCardIdx()
			m.clampEntityCursor()
			m.syncSelectedFromBoard()
			if r.anchorID > 0 {
				m.selectTaskByID(r.anchorID)
			}
			applied = true
		}
	case realtimeReloadActivity:
		if r.activityValid {
			m.activity = r.activity
			m.activityForTask = r.activityForID
			m.reanchorActivityCursor(r.anchorID)
			m.refreshActivityLines()
			applied = true
		}
	case realtimeReloadPlanShow:
		if r.planValid {
			m.planNetworkShow = r.planShow
			m.invalidatePlanNetworkRowsCache()
			rows := m.planNetworkBuildRows()
			m.planNetworkCursor = m.planNetworkCursor.WithItemCount(len(rows))
			if m.planNetworkCursor.Cursor() >= len(rows) && len(rows) > 0 {
				m.planNetworkCursor = m.planNetworkCursor.SetCursor(0)
			}
			m.syncPlanNetworkScroll(rows)
			applied = true
		}
	case realtimeReloadStats:
		if r.statsValid {
			m.statsSummary = r.statsSummary
			applied = true
		}
	case realtimeReloadInsights:
		if r.insightsValid {
			m.insights = r.insights
			m.insightsLoaded = true
			applied = true
		}
	case realtimeReloadLogs:
		if r.logsValid {
			rows := r.events
			if m.logsFilterMode == LogsFilterAll {
				rows = filterLogVisibleRows(rows)
			}
			m.events = rows
			if m.logsSelected >= len(m.events) {
				if len(m.events) == 0 {
					m.logsSelected = 0
				} else {
					m.logsSelected = len(m.events) - 1
				}
			}
			m.eventStats = computeEventStats(rows, r.eventCounts)
			applied = true
		}
	}
	if !applied {
		return
	}
	// Successful apply: advance the stale-guard generation (F3) and commit the
	// probed watermark baseline (F2). dataVersionValid is false on the
	// no-watermark / probe-error path, where there is no trustworthy version
	// to commit — leave the baseline alone so the fallback reload-every-tick
	// behaviour is preserved.
	m.markRealtimeReloadApplied(r.kind, r.gen)
	if r.dataVersionValid {
		m.commitDataVersion(r.kind, r.dataVersion)
	}
}

func (m *Model) refreshCurrentView() error {
	if m.onHome() {
		return m.loadHome()
	}
	if m.top == topStats && m.sub == subStatsLogs {
		return m.refreshActivityLogs()
	}
	if m.top == topStats && m.sub == subStatsGeneral {
		return m.refreshStats()
	}
	if m.top == topStats && m.sub == subStatsInsights {
		return m.refreshInsights()
	}
	return m.refreshPreservingTaskSelection()
}

func (m *Model) refreshPreservingTaskSelection() error {
	var selectedTaskID int64
	if task, ok := m.selectedTask(); ok {
		selectedTaskID = task.ID
	}

	if err := m.refresh(); err != nil {
		return err
	}
	if selectedTaskID > 0 {
		m.selectTaskByID(selectedTaskID)
	}
	return nil
}

func (m *Model) refreshActivityLogs() error {
	if m.repos.Events == nil {
		return nil
	}
	views := m.activeViewSettings()
	m.views = views
	since := m.logsSinceFloor(views.Logs.WindowDays)
	listFilter := domain.EventFilter{
		ProjectID: m.project.ID,
		// Categories empty by default so the Logs inspector shows
		// every event_type the project has recorded inside the
		// window. Sub-task #326 will fold the F-chip selection into
		// this slice without rewriting the refresh path.
		Categories: m.logsCategoryFilter(),
		Since:      since,
		Limit:      views.Logs.Limit,
		Order:      views.Logs.Sort.Order,
	}
	rows, err := m.repos.Events.ListEvents(m.ctx, listFilter)
	if err != nil {
		return err
	}
	// Apply the LogVisible registry filter only when the user has not
	// explicitly narrowed via a chip. An explicit chip selection wins —
	// the user asked to see that category, even if a future YAML config
	// marks individual entries LogVisible: false.
	if m.logsFilterMode == LogsFilterAll {
		rows = filterLogVisibleRows(rows)
	}
	m.events = rows
	// Clamp the cursor to the new row buffer. Refresh callers other
	// than cycleLogsFilter (which already pins selection to 0) can
	// land here with a logsSelected that points past len(rows) after
	// the registry / chip filter shrinks the result. In particular,
	// when filterLogVisibleRows empties the buffer we must drop the
	// cursor to 0 so the empty-state renderer in renderLogs and any
	// later cursorMarker lookup do not address a row that no longer
	// exists.
	if m.logsSelected >= len(m.events) {
		if len(m.events) == 0 {
			m.logsSelected = 0
		} else {
			m.logsSelected = len(m.events) - 1
		}
	}
	// Summary tables aggregate across the wider window (still
	// scoped by the snapshot's logs.window_days) so the headline
	// numbers reflect every recorded event regardless of the panel
	// row cap.
	counts, err := m.repos.Events.EventCategoryCounts(m.ctx, m.project.ID, since)
	if err != nil {
		return err
	}
	m.eventStats = computeEventStats(rows, counts)
	return nil
}

// logsSinceFloor converts views.logs.window_days into the inclusive
// time floor passed to EventRepository. Negative or zero values mean
// "no time floor" (return the zero-value time.Time) so test fixtures
// and legacy bundles without the setting still render every row they
// have. The active snapshot is the source of truth — when wired, it
// normalises the day count before us; this helper is the last-mile
// fallback for the headless paths that never opened a bundle.
func (m *Model) logsSinceFloor(windowDays int) time.Time {
	if snap := m.repos.activeSnapshot(); snap != nil {
		if d := snap.LogsWindowDays(); d > 0 {
			return time.Now().Add(-d)
		}
	}
	if windowDays <= 0 {
		return time.Time{}
	}
	return time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)
}

// logsCategoryFilter projects the active LogsFilterMode onto the
// repository's EventFilter.Categories slice. Returns nil for
// LogsFilterAll so domain.EventFilter treats it as "no category
// filter" — the default view surfaces every event_type recorded in
// the snapshot's logs.window_days horizon. Sub-task #326 wires the
// F-chip toggle through here; the renderer and refreshActivityLogs
// both read the same seam so chip state and panel rows stay aligned.
func (m *Model) logsCategoryFilter() []domain.EventCategory {
	return logsFilterCategories(m.logsFilterMode)
}

func (m *Model) refreshStats() error {
	if m.repos.Metrics == nil {
		return nil
	}
	if m.statsPeriod == "" {
		m.statsPeriod = "30d"
	}
	summary, err := m.repos.Metrics.Summary(m.ctx, m.project, m.statsPeriod, 0)
	if err != nil {
		return err
	}
	m.statsSummary = summary
	return nil
}

// insightsStuckBuckets resolves the in-flight bucket ids the stuck scan
// should target from a workflow, translating InFlightBucketIDs' (ids, ok)
// into the repository's tri-state stuckBuckets contract: an unresolved
// workflow (ok=false) becomes nil so the repo applies its canonical
// fallback; a resolved workflow (ok=true) passes its ids through as
// authoritative — an empty-but-non-nil slice when the preset has no
// in-flight stage, so the repo scans nothing instead of falling back.
func insightsStuckBuckets(wf domain.Workflow) []int64 {
	ids, ok := wf.InFlightBucketIDs()
	if !ok {
		return nil
	}
	if ids == nil {
		// Preserve the authoritative-empty signal: a known workflow with no
		// in-flight stage must not collapse to the nil "fallback" case.
		return []int64{}
	}
	return ids
}

// refreshInsights re-fetches the intelligence-layer reading for the
// Stats › Insights sub-mode. Mirrors refreshStats: a no-op when the
// service is unwired (stub fixtures), and it never queries directly —
// all six insights come from InsightsService.Today. stuckDays=0 takes
// the service default (DefaultStuckDays).
func (m *Model) refreshInsights() error {
	if m.repos.Insights == nil {
		return nil
	}
	ins, err := m.repos.Insights.Today(m.ctx, m.project, m.project.ID, 0, insightsStuckBuckets(m.workflow))
	if err != nil {
		return err
	}
	m.insights = ins
	m.insightsLoaded = true
	return nil
}

// activeViewSettings reads the resolved per-view sort/filter from the
// active bundle's cached snapshot. Falls back to canonical defaults when
// the snapshot is not wired (tests/headless callers); refresh is on the
// hot path and previously re-walked disk via editor.Load() on every tick.
func (m *Model) activeViewSettings() config.ViewSettings {
	if snap := m.repos.activeSnapshot(); snap != nil {
		return snap.Settings().EffectiveViews()
	}
	return config.Settings{}.EffectiveViews()
}

func (m Model) View() string {
	view := m.renderView()
	// Notification and palette are mutually exclusive overlays —
	// when a notification is active it owns the screen and the
	// palette panel is suppressed (state preserved; the next View
	// pass after the notification dismisses brings the palette back
	// with its prior results / cursor). Matches dispatchNotification's
	// input-layer precedence: notification keystrokes already win
	// over palette routing in Update, so visual precedence here
	// keeps the input contract and the render contract aligned.
	switch {
	case m.notification != nil:
		view = normalizeViewToTerminal(view, m.width, m.height)
		view = notification.Overlay(view, m.notification.View(), m.notification.Position())
	case m.paletteOpen:
		view = normalizeViewToTerminal(view, m.width, m.height)
		view = notification.Overlay(view, m.renderPaletteOverlay(), notification.PositionCenter)
	}
	return view
}

// renderPaletteOverlay wraps palette.Model.View output in a bordered
// panel matching the theme's accent so the overlay reads as a modal
// floating above the base render. Width is fixed at 48 cells — wide
// enough for `verb:operand` + an inline status, narrow enough to fit
// without clipping on standard 80-column terminals. MaxHeight caps
// the overlay so a runaway palette body (e.g. a 200-hit search list)
// cannot push the base render and footer hints off-screen — the
// inner result list runs its own sliding-window cap via
// `palette.Model.SetMaxResultRows`.
func (m Model) renderPaletteOverlay() string {
	body := m.palette.View()
	kicker := m.styles.kicker("palette")
	hint := m.styles.hint.Render("enter submit · tab toggles tabs · esc close")
	panel := lipgloss.JoinVertical(lipgloss.Left, kicker, hint, "", body)
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.hintAccent.GetForeground()).
		Padding(0, 2).
		Width(48)
	if m.height > 0 {
		style = style.MaxHeight(m.height)
	}
	return style.Render(panel)
}

// paletteResultRowsBudget computes the maximum number of result
// rows the palette overlay can render given the current terminal
// height. The constant 13 accounts for the overlay's non-row
// chrome: 2 border lines, 3 lines of header (kicker, hint, blank
// separator), 2 lines for the tabs label + search input, 2 blank
// lines that precede the result list, the "N results" header, the
// two optional `↑/↓ N more` indicators, and a status line. Floor
// at 3 so the cursor row plus a sliver of context always fits even
// on a one-line terminal; zero (unlimited) is reserved for the
// pre-resize path.
func (m Model) paletteResultRowsBudget() int {
	const chromeLines = 13
	const floor = 3
	if m.height <= 0 {
		return 0
	}
	budget := m.height - chromeLines
	if budget < floor {
		return floor
	}
	return budget
}

// normalizeViewToTerminal rectangularises the rendered view so the
// notification overlay positions relative to the FULL terminal grid instead
// of the (often shorter / narrower) rendered content. Without this
// "center" lands inside the active card and "top-right" can fall off
// the visible columns when the status badge wraps wide.
//
// width/height come from the most recent tea.WindowSizeMsg. When
// either is zero the view is returned untouched — the overlay path
// still works against the natural content rectangle.
func normalizeViewToTerminal(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		w := ansi.StringWidth(line)
		switch {
		case w < width:
			lines[i] = line + strings.Repeat(" ", width-w)
		case w > width:
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderView() string {
	if m.helpOpen {
		return clampViewToHeight(m.height, m.renderHeader(), m.renderHelp(), m.renderHelpFooter())
	}
	if m.mode != modeNormal && !m.isEmbeddedCommentInput() && !m.isFullPanelTextareaInput() {
		return clampViewToHeight(m.height, m.renderHeader(), m.renderInput(), m.renderCurrentView(), m.renderFooter())
	}

	parts := []string{m.renderHeader()}
	if m.status != "" && !m.isEmbeddedCommentInput() {
		parts = append(parts, "  "+m.styles.statusBadge(m.status))
	}
	parts = append(parts, m.renderCurrentView(), m.renderFooter())
	return clampViewToHeight(m.height, parts...)
}

// isFullPanelTextareaInput is true while a modal owns the whole main
// panel (not a single-line top-bar input). The plan goal editor is one
// such mode — it renders the textarea inside renderPlanNetwork, so the
// chrome must NOT also stack renderInput above it.
func (m Model) isFullPanelTextareaInput() bool {
	return m.mode == modePlanGoal
}

// dispatchNotification routes notification-related messages to the live notification
// model when present, and intercepts ShowMsg / DismissedMsg to flip
// the notification slot. handled=true means the parent's regular dispatch
// should stop — notification is intentionally exclusive while active so
// dismiss + scroll keys take priority over the app underneath.
func (m Model) dispatchNotification(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if showMsg, ok := msg.(hookactions.NotificationShowMsg); ok {
		// Drop the new request when a notification is still typing in —
		// avoids interrupting an Appearing animation with a fresh
		// payload (Settled notifications are replaceable).
		if m.notification != nil && m.notification.State() == notification.StateAppearing {
			return m, nil, true
		}
		bm, cmd := notification.New(notification.Options{
			Notification: showMsg.Notification,
			Theme:        m.theme,
			Text:         showMsg.Text,
			DetailText:   showMsg.DetailText,
			Catalog:      m.repos.Catalog,
		})
		m.notification = &bm
		return m, cmd, true
	}
	if dm, ok := msg.(notification.DismissedMsg); ok {
		if m.notification != nil && dm.ID == m.notification.ID() {
			m.notification = nil
		}
		// esc on the home-project-delete-confirm overlay must clear the
		// pending project id as well as the notification slot — otherwise
		// a stray ActionMsg arriving from a re-spawn would still hit the
		// "execute" path with the original target.
		m.homeProjectDeletePendingID = 0
		m.homeProjectDeletePendingCounters = domain.ProjectDeleteCounters{}
		m.revertConfigSwap()
		return m, nil, true
	}
	if am, ok := msg.(notification.ActionMsg); ok {
		if m.notification != nil && am.ID == m.notification.ID() {
			m.notification = nil
		}
		// Explicit action choice means the user accepted the swap; clear
		// the revert-on-dismiss intent so a later esc on a different
		// notification doesn't accidentally roll back the active config.
		m.pendingSwapRevertPath = ""
		// Home project-delete overlay: the YAML carries no Command (no
		// DispatchCommand re-open of the SQLite handle); the action is
		// routed through the same ProjectService.Delete the second-`d`
		// fallback uses so the TUI's already-open store handle stays
		// authoritative.
		if am.Slug == "home-project-delete-confirm" {
			cmd := m.handleHomeProjectDeleteAction(am)
			return m, cmd, true
		}
		m.handleNotificationAction(am)
		return m, nil, true
	}
	if m.notification == nil {
		return m, nil, false
	}
	switch msg.(type) {
	case tea.KeyMsg:
		// Notification consumes keys exclusively while active: scroll +
		// dismiss handled, others swallowed so the app doesn't react.
		next, cmd := m.notification.Update(msg)
		m.notification = &next
		return m, cmd, true
	}
	// Forward non-key messages (ticks, timeouts, window size) to
	// notification without consuming the parent's chance to react. Notification
	// returns nil cmd for unrelated messages so this is cheap.
	next, cmd := m.notification.Update(msg)
	m.notification = &next
	if cmd != nil {
		return m, cmd, true
	}
	return m, nil, false
}

// clampViewToHeight joins the segments of a view (header → middle → footer)
// and ensures the result never exceeds the terminal height. When the joined
// output is too tall, the trailing segment (footer) is preserved as the
// bottom anchor and the middle segments are truncated from the bottom — so
// the top-of-screen header and the keybinding footer always stay visible.
//
// Without this clamp the alt-screen renderer would scroll the top off the
// terminal whenever a view exceeds the available rows (e.g. the config view
// with five entity columns), making nav tabs and the project header invisible.
func clampViewToHeight(height int, segments ...string) string {
	output := strings.Join(segments, "\n")
	if height <= 0 {
		return output
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= height {
		return output
	}
	if len(segments) < 2 {
		return strings.Join(lines[:height], "\n")
	}
	footer := strings.Split(segments[len(segments)-1], "\n")
	body := strings.Split(strings.Join(segments[:len(segments)-1], "\n"), "\n")
	budget := height - len(footer)
	if budget <= 0 {
		// Footer alone exceeds the terminal — fall back to a plain top clamp
		// so something always renders rather than producing an empty screen.
		return strings.Join(lines[:height], "\n")
	}
	if len(body) > budget {
		body = body[:budget]
	}
	return strings.Join(append(body, footer...), "\n")
}

func (m Model) availableWidth() int {
	width := m.width
	if width <= 0 {
		width = 120
	}
	if width-4 < 24 {
		return 24
	}
	return width - 4
}

func (m Model) taskFormWidth() int {
	return clampInt(m.availableWidth()-8, 32, 120)
}

func (m Model) commentInputWidth() int {
	return clampInt(m.availableWidth()-8, 24, m.activityPanelWidth()-8)
}

// activityPanelWidth returns the activity column width based on the current
// terminal size. On narrow screens the column collapses below the side-by-side
// threshold (handled by the caller), so this only matters in wide layout —
// where it grows up to a max so a 200-col terminal doesn't render a single
// 150-col column with awkward whitespace.
func (m Model) activityPanelWidth() int {
	available := m.availableWidth()
	// Reserve ~55% for details, ~45% for activity, with a sensible floor and
	// a hard cap so the column doesn't dominate ultra-wide terminals.
	candidate := available * 45 / 100
	if candidate < taskCommentsPanelMinWidth {
		candidate = taskCommentsPanelMinWidth
	}
	if candidate > taskCommentsPanelMaxWidth {
		candidate = taskCommentsPanelMaxWidth
	}
	// Hard cap at the available width: the min floor (44) exceeds what a very
	// narrow terminal can show, and in the stacked project/task layout the
	// activity rail renders at activityPanelWidth-4 — without this cap that
	// panel tips past the terminal edge. On wide terminals `available` always
	// dwarfs the candidate, so this only bites the narrow stacked case.
	if candidate > available {
		candidate = available
	}
	return candidate
}

// commentCardWidth is the Width() value passed to the commentCard style.
// lipgloss treats Width as content+padding (border excluded), so the visible
// card occupies Width()+2 cells. We subtract enough from the panel width to
// leave a 2-cell margin inside the activity box — without that margin lines
// occasionally tipped past the box's inner edge and gridtable.WrapLines would
// chop the card mid-row, which the user reported as "cards quebram".
func (m Model) commentCardWidth() int {
	return m.activityPanelWidth() - 6
}

// commentCardContentWidth is how many cells the comment body, header, and
// tag badges have to fit in once padding (2) is subtracted from Width().
func (m Model) commentCardContentWidth() int {
	return m.commentCardWidth() - 2
}

func (m Model) hintBoxWidth() int {
	return clampInt(m.availableWidth()-8, 32, 60)
}

func (m Model) isEmbeddedCommentInput() bool {
	if m.taskScreen != taskScreenView || m.taskID <= 0 {
		return false
	}
	return m.mode == modeComment
}

func (m *Model) handleCommonKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "ctrl+h", "0":
		// Multi-project Home is reachable from every per-project view. The
		// underlying picker keeps its prior cursor so re-entry feels stable;
		// we do reload tags/pending counts so a freshly-edited project row
		// reflects the latest state.
		m.moveMode = false
		m.status = ""
		m.pushHistory()
		m.top = topHome
		if err := m.loadHome(); err != nil {
			m.status = err.Error()
		}
		return true
	case "ctrl+p":
		// Open the dedicated project-view screen: project metadata +
		// project-scoped activity feed. Reachable from every per-project
		// view; openProjectView pushes the current view so ctrl+o returns.
		// Suppressed on Home (no project resolved yet) — handled there by
		// the early onHome() branch in Update, so this case only fires on a
		// per-project surface.
		m.openProjectView()
		return true
	case "ctrl+o":
		// Vim-style "older": pop the back-stack to restore the most
		// recent (top, sub). Silent no-op when the stack is empty so
		// repeated presses at the start of a session do not spam status.
		if m.popHistory() {
			m.moveMode = false
			m.status = ""
		}
		return true
	case "esc":
		if m.moveMode {
			m.moveMode = false
			m.status = ""
			return true
		}
	case "tab":
		m.pushHistory()
		m.cycleTop(1)
		m.moveMode = false
		return true
	case "shift+tab":
		m.pushHistory()
		m.cycleTop(-1)
		m.moveMode = false
		return true
	case "1":
		m.pushHistory()
		m.jumpTop(topTasks)
		m.moveMode = false
		return true
	case "2":
		m.pushHistory()
		m.jumpTop(topStats)
		m.moveMode = false
		return true
	case "3":
		m.pushHistory()
		m.jumpTop(topSettings)
		m.moveMode = false
		return true
	case ",":
		m.pushHistory()
		m.cycleSub(-1)
		m.moveMode = false
		return true
	case "/":
		m.pushHistory()
		m.cycleSub(1)
		m.moveMode = false
		return true
	case "n":
		if m.top != topTasks || m.inPlanNetwork() {
			return false
		}
		m.openTaskCreate()
		return true
	case "e":
		if m.top != topTasks || m.inPlanNetwork() {
			return false
		}
		if task, ok := m.selectedTask(); ok {
			m.openTaskEdit(task)
		}
		return true
	case "c":
		if m.top != topTasks || m.inPlanNetwork() {
			return false
		}
		if _, ok := m.selectedTask(); ok {
			m.beginInput(modeComment, m.t("tui.input.comment_body"), "")
		}
		return true
	case "r":
		if m.inPlanNetwork() {
			return false
		}
		if err := m.refreshCurrentView(); err != nil {
			m.status = err.Error()
		} else {
			m.status = m.t("tui.status.refreshed")
		}
		return true
	case "A":
		if m.top != topTasks {
			return false
		}
		m.includeArchived = !m.includeArchived
		if err := m.refreshCurrentView(); err != nil {
			m.status = err.Error()
		} else if m.includeArchived {
			m.status = m.t("tui.status.showing_archived")
		} else {
			m.status = m.t("tui.status.hiding_archived")
		}
		return true
	}
	return false
}

// inPlanNetwork returns true when the user has drilled into the plans
// sub-tab's column-per-wave network view. Used by handleCommonKey to
// release `c`/`e`/`n`/`r` so the network handler can rebind them
// (claim, future edit-goal, future add-task-to-wave, plan-show reload)
// without colliding with the task-centric bindings that normally win
// across sub-tabs.
func (m *Model) inPlanNetwork() bool {
	return m.sub == subPlans && m.planNetworkOpen
}

// cycleTop advances the active top by delta positions (positive forward,
// negative backward) along topOrder. The sub always lands on the first
// sub of the new top — there is no per-top "last sub used" memory in T1.
func (m *Model) cycleTop(delta int) {
	idx := topIndex(m.top)
	if idx < 0 {
		idx = 0
	}
	n := len(topOrder)
	next := topOrder[((idx+delta)%n+n)%n]
	m.top = next
	m.sub = firstSub(next)
	m.syncEntityKindFromSub()
}

// jumpTop moves directly to a target top (bound to the digit keys 1/2/3),
// landing on its first sub. No-op when the model is already on that top
// and its first sub — keeps repeated digit presses from clobbering nav.
func (m *Model) jumpTop(target topID) {
	if m.top == target {
		return
	}
	m.top = target
	m.sub = firstSub(target)
	m.syncEntityKindFromSub()
}

// cycleSub moves the active sub forward (delta=1) or backward (delta=-1)
// inside the current top. No-op when the top exposes a single sub — the
// binding is silently dropped so users on a single-sub top do not have
// to learn "this only works on Tasks/Stats/Settings".
func (m *Model) cycleSub(delta int) {
	subs := subsByTop[m.top]
	if len(subs) <= 1 {
		return
	}
	idx := subIndex(m.top, m.sub)
	if idx < 0 {
		idx = 0
	}
	n := len(subs)
	m.sub = subs[((idx+delta)%n+n)%n]
	m.syncEntityKindFromSub()
}

// syncEntityKindFromSub mirrors the active Settings sub onto m.entityKind
// so the existing entity handlers (handleConfigKey, scaffold/edit/delete
// helpers) keep reading the right list. No-op for non-Settings subs and
// for `subSettingsGeneral` — general is read-only and does not bind to
// any entity list.
func (m *Model) syncEntityKindFromSub() {
	if k, ok := entityKindForSub(m.sub); ok {
		m.entityKind = k
		m.syncFocusedEntityScroll()
	}
}

func (m *Model) refresh() error {
	views := m.activeViewSettings()
	m.views = views

	// Phase 2-bis routes every per-project view through the BundleCache —
	// production wires one at boot, tests wire one via
	// testfixtures/runtimecache.Install. Reads hit r.Cache.Get(r.ProjectID).Snapshot
	// unconditionally.

	query := app.NewTUIQueryService(m.repos.Tasks, m.repos.activeSnapshot(), m.repos.Dependencies, m.repos.Comments, m.repos.Tags)
	snap, err := query.Snapshot(m.ctx, m.project, domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}, app.SnapshotOptions{IncludeArchived: m.includeArchived})
	if err != nil {
		return err
	}

	m.tasks = snap.Tasks
	m.workflow = snap.Workflow
	m.dependencies = snap.Dependencies
	m.comments = snap.Comments
	m.laws = snap.Laws
	m.skills = snap.Skills
	m.personas = snap.Personas
	m.templates = snap.Templates
	m.tags = snap.AllTags
	m.taskTagsMap = snap.TaskTagsByID
	m.metrics = m.computeMetrics(0)
	if bundleSnap := m.repos.activeSnapshot(); bundleSnap != nil {
		m.languages = bundleSnap.Settings().EffectiveLanguages()
	}
	if m.repos.Plans != nil {
		// Plans sub-tab is best-effort: a query failure stays out of the
		// refresh return value so an unrelated sub-tab (board/table/graph)
		// still loads. The plans renderer shows the cached slice or the
		// empty-state hint when m.plans is nil.
		planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
		if rollups, err := planSvc.ListRollups(m.ctx, m.project); err == nil {
			m.plans = rollups
		}
	}
	m.invalidateBoardCaches()
	m.rebuildBoardCaches()
	m.clampPlanCursor()
	m.clampGraphCursor()
	m.clampSelection()
	m.clampCardIdx()
	m.clampEntityCursor()
	m.syncSelectedFromBoard()
	return nil
}

// clampPlanCursor keeps the plansCursor inside the [0, len(plans)-1]
// window after refresh trims the rollup slice. Routes through
// cursorwindow.WithItemCount so the resync contract (clamp + scroll
// follow) lands in one method call instead of being re-litigated
// inline. Deleted plans cannot leave the cursor stranded past end.
func (m *Model) clampPlanCursor() {
	m.plansCursor = m.plansCursor.WithItemCount(len(m.plans))
}

// clampGraphCursor keeps graphCursor inside the [0, selectable-1] window
// after refresh reassigns m.dependencies. The renderer derives its cursor
// line via sel[graphCursor.Cursor()] (sel = dagSelectableIndices), so a
// stale cursor pointing past a shrunk selectable set would panic the value-
// receiver render path. Mirrors clampPlanCursor: route through
// cursorwindow.WithItemCount using the same selectable-node count the
// renderer derives. Removing a dependency while the graph subnav is open
// can no longer strand the cursor past end.
func (m *Model) clampGraphCursor() {
	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)
	m.graphCursor = m.graphCursor.WithItemCount(len(sel))
}

func (m Model) computeMetrics(maxTokens int) domain.TokenMetrics {
	total := 0
	for _, law := range m.laws {
		// m.laws now carries the full catalog; only the active subset is in
		// the agent context, so inactive entries must not inflate the budget.
		if !law.Active {
			continue
		}
		total += m.countTokens(law.Key + " " + law.Body)
	}
	for _, persona := range m.personas {
		// Persona descriptions count toward the budget; skill bodies do not.
		// Skip inactive catalog entries — only wired personas hit the budget.
		if !persona.Active {
			continue
		}
		total += m.countTokens(persona.Description)
	}
	for _, comment := range m.comments {
		total += m.countTokens(comment.Body)
	}
	return domain.TokenMetrics{EstimatedTotal: total, MaxTokens: maxTokens, Truncated: maxTokens > 0 && total > maxTokens}
}

// countTokens looks the body's token count up in m.tokenCountCache (key
// = fnv64a hash) and falls through to m.counter.Count on a miss. A nil
// cache (uninitialised model in tests) degrades to a direct counter
// call so the helper stays safe to call from value-receiver paths.
func (m Model) countTokens(body string) int {
	if body == "" {
		return 0
	}
	if m.tokenCountCache == nil {
		return m.counter.Count(body)
	}
	key := fnv64aString(body)
	if cached, ok := m.tokenCountCache[key]; ok {
		return cached
	}
	count := m.counter.Count(body)
	m.tokenCountCache[key] = count
	return count
}

// fnv64aString fingerprints body for the token cache key. Collisions
// inside a single TUI session are statistically negligible against
// 64-bit space; on collision the worst case is two bodies sharing a
// stale count, which a future fresh body insert overwrites. Delegates
// to the shared fingerprint helper so the wire shape stays aligned
// with the four cache-key callsites that use it.
func fnv64aString(body string) uint64 {
	f := newFingerprint()
	f.writeString(body)
	return f.sum()
}
