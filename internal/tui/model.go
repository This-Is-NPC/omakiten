package tui

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	hookactions "omakiten/internal/hooks/actions"
	"omakiten/internal/keynav"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/header"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/overlay"
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
	"omakiten/internal/tui/screens/knowledge"
	"omakiten/internal/tui/screens/logs"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/plans"
	projectscreen "omakiten/internal/tui/screens/project"
	"omakiten/internal/tui/screens/projectresume"
	"omakiten/internal/tui/screens/relationshippicker"
	settingsscreen "omakiten/internal/tui/screens/settings"
	"omakiten/internal/tui/screens/stats"
	"omakiten/internal/tui/screens/studio"
	"omakiten/internal/tui/screens/table"
	"omakiten/internal/tui/screens/taskdetail"
	"omakiten/internal/tui/screens/taskform"
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
	model := Model{
		ctx:              ctx,
		project:          project,
		repos:            repos,
		theme:            theme,
		styles:           newStyles(theme),
		counter:          counter,
		tokenBadgeYellow: yellow,
		tokenBadgeRed:    red,
		priorities:       priorities,
		severities:       severities,
		registry:         config.BuildEnumRegistry(config.Bundle{Config: config.Settings{Priorities: priorities, Severities: severities}}),
		markdownRendered: true,
		notifications:    notifications.Notifications,
		// Pre-allocated card box-style cache. Value-receiver render paths
		// read + write through it, so lipgloss.Style.Width(N) fires once
		// per (variant, width) pair over the model's lifetime instead of
		// once per card per keystroke on a long board column.
		cardStyles:              card.NewCache(),
		tokenCountCache:         map[uint64]int{},
		statsScreen:             stats.New(),
		logsScreen:              logs.New(),
		insightsScreen:          insights.New(),
		studioScreen:            studio.New(),
		boardScreen:             board.New(),
		homeScreen:              home.New(),
		tableScreen:             table.New(),
		graphScreen:             graph.New(),
		plansScreen:             plans.New(),
		planGoalReaderScreen:    plans.NewGoal(),
		planNetworkScreen:       plannetwork.New(),
		projectScreen:           projectscreen.New(),
		projectFormReaderScreen: projectscreen.NewForm(),
		projectResumeScreen:     projectresume.New(),
		projectKnowledgeScreen:  knowledge.New(),
		settingsGeneralScreen:   settingsscreen.NewGeneral(),
		settingsGuardsScreen:    settingsscreen.NewGuards(),
		entityDetailScreen:      entitydetail.New(),
		personaSkillsScreen:     relationshippicker.New(relationshippicker.PersonaSkills),
		templateDefaultScreen:   relationshippicker.New(relationshippicker.TemplateDefault),
		taskDetailScreen:        taskdetail.New(),
		taskFormScreen:          taskform.New(),
		descriptionReaderScreen: description.New(),
		commentDetailScreen:     commentdetail.New(),
		entityListScreens: map[screenhost.ID]entitylist.Screen{
			screenhost.SettingsLaws:      entitylist.New(entitylist.Laws()),
			screenhost.SettingsPersonas:  entitylist.New(entitylist.Personas()),
			screenhost.SettingsSkills:    entitylist.New(entitylist.Skills()),
			screenhost.SettingsTemplates: entitylist.New(entitylist.Templates()),
			screenhost.SettingsTags:      entitylist.New(entitylist.Tags())}}
	if reg, err := buildPaletteRegistry(repos); err == nil {
		model.paletteRegistry = reg
	}
	model.commentInput = newCommentInput()
	model.moveInput = newMoveInput()
	if project.ID == 0 {
		// Empty project — open on the multi-project Home picker.
		// Do not call refresh() because every per-project query would 404
		// without a resolved project_id.
		model.navigation = firstSub(screenhost.TopHome)
		if err := model.reloadHome(); err != nil {
			return Model{}, err
		}
		return model, nil
	}
	model.navigation = screenhost.TasksBoard
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
	return tea.Batch(scheduleRefreshTick(), scheduleNoticeTick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevNav := m.navigation
	if next, cmd, handled := m.dispatchNotification(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := updateLifecycleMessage(m, msg); handled {
		return next, cmd
	}
	if next, cmd, handled := updatePaletteMessage(m, msg); handled {
		return next, cmd
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		return updateKey(m, key, prevNav)
	}
	if cmd, handled := m.dispatchActiveScreen(msg); handled {
		return m, cmd
	}
	return m, m.refreshAfterViewChangeCmd(prevNav)
}

func updateLifecycleMessage(m Model, msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case refreshAfterViewChangeMsg:
		m.applyRefreshAfterViewChange(msg)
	case studioHookHistoryResultMsg:
		m.applyStudioHookHistory(msg)
	case homeProjectDeleteResultMsg:
		m.handleHomeProjectDeleteResult(msg)
	case homeReloadResultMsg:
		m.applyHomeReload(msg)
	case tea.WindowSizeMsg:
		m.applyWindowSize(msg)
	case refreshTickMsg:
		return m, m.updateRefreshTick(), true
	case noticeTickMsg:
		return m, m.noticeTailCmd(), true
	case noticeRowsMsg:
		next, cmd := m.applyNoticeRows(msg)
		return next, cmd, true
	case realtimeReloadMsg:
		m.applyRealtimeReload(msg)
	case editorFinishedMsg:
		m.handleEditorFinished(msg)
	case palette.DismissMsg:
		m.paletteOpen = false
	default:
		return m, nil, false
	}
	return m, nil, true
}

func updatePaletteMessage(m Model, msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case paletteSearchResultMsg:
		m.applyPaletteSearchResult(msg)
	case palette.OpenHitMsg:
		return m, m.dispatchOpenHit(msg.Hit), true
	case palette.SubmitMsg:
		return m, m.dispatchTrick(msg.Token), true
	case palette.SearchMsg:
		return m, m.dispatchPaletteSearch(msg.Query), true
	default:
		return m, nil, false
	}
	return m, nil, true
}

func (m *Model) applyPaletteSearchResult(msg paletteSearchResultMsg) {
	if !m.paletteOpen || m.project.ID == 0 || msg.projectID != m.project.ID ||
		msg.projectGeneration != m.projectGeneration || msg.runtimeGeneration != m.studioRuntimeGeneration ||
		msg.paletteOpenGeneration != m.paletteOpenGeneration || msg.paletteSearchGeneration != m.paletteSearchGeneration {
		return
	}
	validHits := make([]domain.SearchHit, 0, len(msg.hits))
	for _, hit := range msg.hits {
		if hit.ProjectID == m.project.ID {
			validHits = append(validHits, hit)
		}
	}
	if len(validHits) > 0 {
		m.palette.SetResults(validHits)
	} else if len(msg.hits) == 0 {
		m.palette.SetStatus(msg.status)
	}
}

func (m *Model) applyWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.boardScreen = m.boundBoardScreen().Lifecycle(m.screenFrame(), screenhost.LifecycleResize).Screen.(board.Screen)
	if screen, ok := m.activeHostedScreen(); ok {
		m.storeScreen(screen.Lifecycle(m.screenFrame(), screenhost.LifecycleResize).Screen)
	}
	m.palette.SetMaxResultRows(m.paletteResultRowsBudget())
}

func (m *Model) updateRefreshTick() tea.Cmd {
	if !m.shouldRealtimeRefresh() {
		return scheduleRefreshTick()
	}
	if _, err := m.reloadBundleIfChanged(); err != nil {
		m.status = err.Error()
	}
	decisions := m.dataVersionChanges(m.currentReloadKinds())
	if len(decisions) == 0 {
		return scheduleRefreshTick()
	}

	savedCtx := m.ctx
	m.ctx = activity.WithoutTracking(m.ctx)
	reloads := make([]tea.Cmd, 0, len(decisions)+1)
	for _, decision := range decisions {
		version := int64(0)
		if decision.ok {
			version = decision.version
		}
		if reload := m.realtimeRefreshCmd(decision.kind, version, decision.ok); reload != nil {
			reloads = append(reloads, reload)
		}
	}
	m.ctx = savedCtx
	if len(reloads) == 0 {
		return scheduleRefreshTick()
	}
	return tea.Batch(append(reloads, scheduleRefreshTick())...)
}

func updateKey(m Model, msg tea.KeyMsg, prevNav screenhost.ID) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updateOverlayKey(msg); handled {
		return next, cmd
	}
	if m.mode != modeNormal {
		return m.updateInput(msg)
	}
	return m.updateNormalKey(msg, prevNav)
}

func (m Model) updateOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.paletteOpen {
		var cmd tea.Cmd
		m.palette, cmd = m.palette.Update(msg)
		return m, cmd, true
	}
	if msg.String() == "ctrl+k" && m.canOpenPalette() {
		m.palette = palette.NewModel()
		m.palette.SetMaxResultRows(m.paletteResultRowsBudget())
		m.paletteOpenGeneration++
		m.paletteOpen = true
		return m, nil, true
	}
	if m.helpOpen {
		next, cmd := m.updateHelpKey(msg)
		return next, cmd, true
	}
	if msg.String() == "?" && m.mode == modeNormal && !m.activeScreenBlocksHelp() {
		m.helpOpen = true
		m.helpAll = false
		m.help.Scroll = 0
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) updateNormalKey(msg tea.KeyMsg, prevNav screenhost.ID) (tea.Model, tea.Cmd) {
	if len(m.screenStack) > 0 {
		if cmd, handled := m.dispatchOwnedScreenKey(msg); handled {
			return m, cmd
		}
	}
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	if m.handleCommonKey(msg) {
		return m, m.refreshAfterViewChangeCmd(prevNav)
	}
	if cmd, handled := m.dispatchActiveScreen(msg); handled && cmd != nil {
		return m, cmd
	}
	return m, nil
}

func (m Model) updateHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.help, _ = m.help.Update(msg, m.helpViewportRows())
		if m.help.LastEvent() == list.ViewportCancel {
			m.helpOpen = false
			m.helpAll = false
			m.help = m.help.WithScroll(0)
		}
	}
	return m, nil
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
		return realtimeReloadFallbackDecisions(kinds)
	}
	version, err := m.repos.Watermark.DataVersion(m.ctx)
	if err != nil {
		m.status = err.Error()
		return realtimeReloadFallbackDecisions(kinds)
	}
	return m.realtimeReloadDecisions(kinds, version)
}

func realtimeReloadFallbackDecisions(kinds []realtimeReloadKind) []realtimeReloadDecision {
	decisions := make([]realtimeReloadDecision, 0, len(kinds))
	for _, kind := range kinds {
		if kind != realtimeReloadNone {
			decisions = append(decisions, realtimeReloadDecision{kind: kind})
		}
	}
	return decisions
}

func (m Model) realtimeReloadDecisions(kinds []realtimeReloadKind, version int64) []realtimeReloadDecision {
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
		return r.scopeTaskID == 0 || r.scopeTaskID == m.taskDetailScreen.Payload().Task.ID
	case realtimeReloadPlanShow:
		return r.scopeSlug == "" || r.scopeSlug == m.planNetworkScreen.Show().Plan.Slug
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
	if m.helpOpen || m.mode != modeNormal || m.activeScreenBlocksHostInput() {
		return false
	}
	// The Ctrl+K palette is a keyboard-input overlay that can sit over the
	// board / plan-network view (canOpenPalette only blocks task/entity
	// screens, not those). A passive reload underneath an open palette is an
	// unguarded input-mode refresh — mirror canOpenPalette and suppress it.
	if m.paletteOpen {
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
	if m.inTaskDetail() {
		return []realtimeReloadKind{realtimeReloadActivity}
	}
	if m.inPlanNetwork() {
		if m.planNetworkScreen.Show().Plan.Slug == "" {
			return nil
		}
		return []realtimeReloadKind{realtimeReloadBundle, realtimeReloadPlanShow}
	}
	switch m.activeReloadPolicy() {
	case screenhost.ReloadManual, screenhost.ReloadHome:
		return nil
	case screenhost.ReloadStats:
		return []realtimeReloadKind{realtimeReloadStats}
	case screenhost.ReloadLogs:
		return []realtimeReloadKind{realtimeReloadLogs}
	case screenhost.ReloadInsights:
		return []realtimeReloadKind{realtimeReloadInsights}
	default:
		return []realtimeReloadKind{realtimeReloadBundle}
	}
}

// canOpenPalette gates the global Ctrl+K binding so the palette
// never steals focus from another modal input. The matrix mirrors
// shouldRealtimeRefresh's "no modal active" check plus the
// description / comment overlays (which are not background-refresh
// gates but still own keyboard focus when open).
func (m Model) canOpenPalette() bool {
	if m.project.ID == 0 {
		return false
	}
	if m.paletteOpen {
		return false
	}
	if m.helpOpen {
		return false
	}
	if m.mode != modeNormal {
		return false
	}
	if m.inTaskDetail() {
		return false
	}
	if m.activeScreenBlocksHostInput() {
		return false
	}
	return true
}

// refreshAfterViewChangeCmd reacts to a sub-tab nav transition. Light
// routes (home, stats, logs) keep running on the Update goroutine
// because their workloads are bounded; the board / table / graph /
// plans routes hand the heavy read pipeline (TUIQuery.Snapshot
// + PlanService.ListRollups) off to a worker via tea.Cmd so a keystroke
// returns immediately. The previous view stays rendered until the
// resulting refreshAfterViewChangeMsg lands and the Update handler
// folds the loaded slices into the model.
func (m *Model) refreshAfterViewChangeCmd(prev screenhost.ID) tea.Cmd {
	if m.navigation == prev {
		return nil
	}
	historyCmd := m.prepareStudioHookHistory()
	if screen, ok := m.activeHostedScreen(); ok {
		if resetter, ok := screen.(screenhost.NavigationResetter); ok && resetter.ResetOnNavigation() {
			m.storeScreen(screen.Lifecycle(m.screenFrame(), screenhost.LifecycleEnter).Screen)
		}
	}
	switch m.activeReloadPolicy() {
	case screenhost.ReloadHome:
		m.viewChangeGeneration++
		if err := m.reloadHome(); err != nil {
			m.status = err.Error()
		}
		return historyCmd
	case screenhost.ReloadLogs:
		m.viewChangeGeneration++
		if err := m.refreshActivityLogs(); err != nil {
			m.status = err.Error()
		}
		return historyCmd
	case screenhost.ReloadStats:
		m.viewChangeGeneration++
		if err := m.refreshStats(); err != nil {
			m.status = err.Error()
		}
		return historyCmd
	case screenhost.ReloadInsights:
		m.viewChangeGeneration++
		if err := m.refreshInsights(); err != nil {
			m.status = err.Error()
		}
		return historyCmd
	}
	return batchStudioHookHistory(historyCmd, m.refreshHeavyAfterViewChangeCmd())
}

func batchStudioHookHistory(historyCmd, refreshCmd tea.Cmd) tea.Cmd {
	if historyCmd == nil {
		return refreshCmd
	}
	return tea.Batch(historyCmd, refreshCmd)
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
	m.viewChangeGeneration++
	var preservedTaskID int64
	if task, ok := m.selectedTask(); ok {
		preservedTaskID = task.ID
	}
	views := m.activeViewSettings()
	m.views = views
	cfgSnap := m.repos.activeSnapshot()
	langs := m.languages
	if cfgSnap != nil {
		langs = cfgSnap.LanguageSettings()
	}
	ctx := m.ctx
	project := m.project
	projectID := project.ID
	projectGeneration := m.projectGeneration
	runtimeGeneration := m.studioRuntimeGeneration
	viewChangeGeneration := m.viewChangeGeneration
	sort := domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}
	archived := m.includeArchived
	svc := m.repos.operationService()
	var cmd tea.Cmd
	cmd = tea.Cmd(func() tea.Msg {
		result := refreshAfterViewChangeMsg{
			preservedTaskID:      preservedTaskID,
			langs:                langs,
			projectID:            projectID,
			projectGeneration:    projectGeneration,
			runtimeGeneration:    runtimeGeneration,
			viewChangeGeneration: viewChangeGeneration,
			cmdKey:               reflect.ValueOf(cmd).Pointer(),
		}
		if svc == nil {
			return result
		}
		s, err := svc.BoardSnapshot(ctx, contract.BoardSnapshotInput{
			ProjectSelector: contract.ProjectSelector{ProjectID: projectID},
			Sort:            sort,
			IncludeArchived: archived,
		})
		if err != nil {
			result.err = err
			return result
		}
		result.snap = s
		result.snapValid = true
		if rollups, perr := svc.ListPlanRollups(ctx, contract.ProjectSelector{ProjectID: projectID}); perr == nil && rollups != nil {
			result.plans = rollups
			result.plansValid = true
		}
		return result
	})
	registerViewChangeRefreshCmd(cmd)
	return cmd
}

// refreshAfterViewChangeMsg is the worker-to-main envelope for the
// async view-change refresh. snapValid / plansValid disambiguate "empty
// slice because the project has none" from "the worker did not load
// this kind of data" — the fold path only updates the Plans screen when the
// worker actually queried it.
type refreshAfterViewChangeMsg struct {
	snap            contract.BoardSnapshot
	snapValid       bool
	plans           []domain.PlanRollup
	plansValid      bool
	langs           config.LanguageSettings
	preservedTaskID int64
	// projectID and projectGeneration identify the project visit captured by
	// the worker. Both are required because a rapid A -> B -> A switch reuses
	// the project ID while still needing to reject the old visit.
	projectID            int64
	projectGeneration    uint64
	runtimeGeneration    uint64
	viewChangeGeneration uint64
	err                  error
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
	if r.projectID != m.project.ID ||
		r.projectGeneration != m.projectGeneration ||
		r.runtimeGeneration != m.studioRuntimeGeneration ||
		r.viewChangeGeneration != m.viewChangeGeneration {
		if r.cmdKey != 0 {
			viewChangeRefreshRegistry.Delete(r.cmdKey)
		}
		return
	}
	if r.cmdKey != 0 {
		viewChangeRefreshRegistry.Delete(r.cmdKey)
	}
	if r.err != nil {
		m.status = r.err.Error()
		return
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
		m.plansScreen = m.boundPlansScreen().Apply(r.plans, nil)
	}
	m.clampSelection()
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
	// projectID/projectGeneration identify the project snapshot captured by the
	// worker. The generation handles a project being selected again after a
	// rapid switch away and back.
	projectID         int64
	projectGeneration uint64
	runtimeGeneration uint64

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
	snap       contract.BoardSnapshot
	snapValid  bool
	plans      []domain.PlanRollup
	plansValid bool
	langs      config.LanguageSettings

	// activity payload (kind == realtimeReloadActivity)
	activity      []domain.Event
	activityForID int64
	activityValid bool
	anchorID      int64

	// plan-show payload (kind == realtimeReloadPlanShow)
	planShow  domain.PlanShow
	planValid bool

	// stats payload (kind == realtimeReloadStats)
	statsSummary domain.MetricsSummary
	statsValid   bool

	// logs payload (kind == realtimeReloadLogs)
	events      []domain.EventRow
	eventCounts map[domain.EventCategory]int
	logsStats   domain.EventStats
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
// only its activity feed — the board snapshot is NOT rebuilt, because the
// hosted Task Detail descriptor fully occludes the board (a board rebuild under
// it is pure waste). Board / plan / stats / logs each reload only their own
// projection.
// Returns nil when nothing is reloadable for the current view (e.g. an empty
// plan-network), leaving the tick a no-op.
func (m *Model) realtimeRefreshCmd(kind realtimeReloadKind, dataVersion int64, dataVersionValid bool) tea.Cmd {
	switch kind {
	case realtimeReloadActivity:
		return m.realtimeActivityReloadCmd(dataVersion, dataVersionValid)
	case realtimeReloadPlanShow:
		return m.realtimePlanReloadCmd(dataVersion, dataVersionValid)
	case realtimeReloadStats:
		return m.realtimeStatsReloadCmd(dataVersion, dataVersionValid)
	case realtimeReloadLogs:
		return m.realtimeLogsReloadCmd(dataVersion, dataVersionValid)
	case realtimeReloadInsights:
		return m.realtimeInsightsReloadCmd(dataVersion, dataVersionValid)
	case realtimeReloadBundle:
		return m.realtimeBundleReloadCmd(dataVersion, dataVersionValid)
	default:
		return nil
	}
}

func (m *Model) realtimeReloadStamp(kind realtimeReloadKind, dataVersion int64, valid bool) func(realtimeReloadMsg) realtimeReloadMsg {
	gen := m.nextRealtimeReloadGen(kind)
	project := m.project
	projectGeneration := m.projectGeneration
	runtimeGeneration := m.studioRuntimeGeneration
	return func(r realtimeReloadMsg) realtimeReloadMsg {
		r.gen = gen
		r.projectID = project.ID
		r.projectGeneration = projectGeneration
		r.runtimeGeneration = runtimeGeneration
		r.dataVersion = dataVersion
		r.dataVersionValid = valid
		return r
	}
}

func newRealtimeReloadCmd(stamp func(realtimeReloadMsg) realtimeReloadMsg, payload func() realtimeReloadMsg) tea.Cmd {
	var cmd tea.Cmd
	cmd = func() tea.Msg {
		result := stamp(payload())
		result.cmdKey = reflect.ValueOf(cmd).Pointer()
		return result
	}
	registerRealtimeReloadCmd(cmd)
	return cmd
}

func (m *Model) realtimeActivityReloadCmd(version int64, valid bool) tea.Cmd {
	taskID := m.taskDetailScreen.Payload().Task.ID
	if !m.inTaskDetail() || taskID <= 0 || m.repos.Events == nil {
		return nil
	}
	ctx, project := m.ctx, m.project
	order := m.activeViewSettings().TaskActivity.Sort.Order
	anchorID := m.taskDetailScreen.FocusedActivityID()
	events := m.repos.Events
	stamp := m.realtimeReloadStamp(realtimeReloadActivity, version, valid)
	return newRealtimeReloadCmd(stamp, func() realtimeReloadMsg {
		result := realtimeReloadMsg{kind: realtimeReloadActivity, anchorID: anchorID, scopeTaskID: taskID}
		rows, err := events.ListTaskActivity(ctx, project.ID, taskID, order)
		if err != nil {
			result.err = err
			return result
		}
		result.activity = rows
		result.activityForID = taskID
		result.activityValid = true
		return result
	})
}

func (m *Model) realtimePlanReloadCmd(version int64, valid bool) tea.Cmd {
	slug := m.planNetworkScreen.Show().Plan.Slug
	if slug == "" {
		return nil
	}
	ctx, project := m.ctx, m.project
	svc := m.repos.operationService()
	stamp := m.realtimeReloadStamp(realtimeReloadPlanShow, version, valid)
	return newRealtimeReloadCmd(stamp, func() realtimeReloadMsg {
		result := realtimeReloadMsg{kind: realtimeReloadPlanShow, scopeSlug: slug}
		if svc == nil {
			result.status = "operation service is not wired"
			return result
		}
		show, err := svc.ShowPlanView(ctx, contract.ShowPlanInput{ProjectSelector: contract.ProjectSelector{ProjectID: project.ID}, Slug: slug})
		if err != nil {
			result.status = err.Error()
			return result
		}
		result.planShow = show
		result.planValid = true
		return result
	})
}

func (m *Model) realtimeStatsReloadCmd(version int64, valid bool) tea.Cmd {
	metrics := m.metricsPort()
	if metrics == nil {
		return nil
	}
	ctx, project := m.ctx, m.project
	period := m.boundStatsScreen().Period()
	stamp := m.realtimeReloadStamp(realtimeReloadStats, version, valid)
	return newRealtimeReloadCmd(stamp, func() realtimeReloadMsg {
		result := realtimeReloadMsg{kind: realtimeReloadStats}
		summary, err := metrics.Summary(ctx, project, period, 0)
		if err != nil {
			result.err = err
			return result
		}
		result.statsSummary = summary
		result.statsValid = true
		return result
	})
}

func (m *Model) realtimeLogsReloadCmd(version int64, valid bool) tea.Cmd {
	if m.repos.Events == nil {
		return nil
	}
	ctx, project := m.ctx, m.project
	filter := m.boundLogsScreen().Filter()
	views := m.activeViewSettings()
	snapshot := m.repos.activeSnapshot()
	events := m.repos.Events
	stamp := m.realtimeReloadStamp(realtimeReloadLogs, version, valid)
	return newRealtimeReloadCmd(stamp, func() realtimeReloadMsg {
		result := realtimeReloadMsg{kind: realtimeReloadLogs}
		payload, err := prepareLogsPayload(ctx, project, events, views, snapshot, filter)
		if err != nil {
			result.err = err
			return result
		}
		result.events = payload.Rows
		result.eventCounts = payload.Stats.Categories
		result.logsStats = payload.Stats
		result.logsValid = true
		return result
	})
}

func (m *Model) realtimeInsightsReloadCmd(version int64, valid bool) tea.Cmd {
	insightsSvc := m.insightsPort()
	if insightsSvc == nil {
		return nil
	}
	ctx, project := m.ctx, m.project
	projectID := project.ID
	stuckBuckets := domain.StuckBucketIDs(m.workflow)
	stamp := m.realtimeReloadStamp(realtimeReloadInsights, version, valid)
	return newRealtimeReloadCmd(stamp, func() realtimeReloadMsg {
		result := realtimeReloadMsg{kind: realtimeReloadInsights}
		ins, err := insightsSvc.Today(ctx, project, projectID, 0, stuckBuckets)
		if err != nil {
			result.err = err
			return result
		}
		result.insights = ins
		result.insightsValid = true
		return result
	})
}

func (m *Model) realtimeBundleReloadCmd(version int64, valid bool) tea.Cmd {
	ctx, project := m.ctx, m.project
	views := m.activeViewSettings()
	langs := m.languages
	if snapshot := m.repos.activeSnapshot(); snapshot != nil {
		langs = snapshot.LanguageSettings()
	}
	sort := domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}
	archived := m.includeArchived
	svc := m.repos.operationService()
	var preservedTaskID int64
	if task, ok := m.selectedTask(); ok {
		preservedTaskID = task.ID
	}
	stamp := m.realtimeReloadStamp(realtimeReloadBundle, version, valid)
	return newRealtimeReloadCmd(stamp, func() realtimeReloadMsg {
		result := realtimeReloadMsg{kind: realtimeReloadBundle, langs: langs, anchorID: preservedTaskID}
		if svc == nil {
			return result
		}
		snapshot, err := svc.BoardSnapshot(ctx, contract.BoardSnapshotInput{ProjectSelector: contract.ProjectSelector{ProjectID: project.ID}, Sort: sort, IncludeArchived: archived})
		if err != nil {
			result.err = err
			return result
		}
		result.snap = snapshot
		result.snapValid = true
		if rollups, err := svc.ListPlanRollups(ctx, contract.ProjectSelector{ProjectID: project.ID}); err == nil {
			result.plans = rollups
			result.plansValid = true
		}
		return result
	})
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
	if !m.realtimeReloadCurrent(r) {
		deleteRealtimeReloadCmd(r)
		return
	}
	deleteRealtimeReloadCmd(r)
	if r.err != nil {
		m.status = r.err.Error()
		return
	}
	if r.status != "" {
		m.status = r.status
	}
	if !m.foldRealtimeReload(r) {
		return
	}
	m.markRealtimeReloadApplied(r.kind, r.gen)
	if r.dataVersionValid {
		m.commitDataVersion(r.kind, r.dataVersion)
	}
}

func deleteRealtimeReloadCmd(r realtimeReloadMsg) {
	if r.cmdKey != 0 {
		realtimeReloadRegistry.Delete(r.cmdKey)
	}
}

func (m Model) realtimeReloadCurrent(r realtimeReloadMsg) bool {
	if r.gen != 0 && r.gen <= m.lastAppliedRealtimeReloadGen(r.kind) {
		return false
	}
	if (r.projectID != 0 || r.projectGeneration != 0) &&
		(r.projectID != m.project.ID || r.projectGeneration != m.projectGeneration) {
		return false
	}
	if r.runtimeGeneration != m.studioRuntimeGeneration {
		return false
	}
	return m.realtimeReloadScopeMatches(r)
}

func (m *Model) foldRealtimeReload(r realtimeReloadMsg) bool {
	switch r.kind {
	case realtimeReloadBundle:
		return m.foldRealtimeBundle(r)
	case realtimeReloadActivity:
		if !r.activityValid {
			return false
		}
		m.taskDetailScreen = m.taskDetailScreen.Apply(taskdetail.Result{
			Generation: m.taskDetailGeneration, TaskID: r.activityForID,
			Activity: r.activity, ActivityValid: true, PreserveAnchor: true})
	case realtimeReloadPlanShow:
		if !r.planValid {
			return false
		}
		m.planNetworkScreen = m.planNetworkScreen.Apply(m.planNetworkPayload(r.planShow))
	case realtimeReloadStats:
		if !r.statsValid {
			return false
		}
		m.statsScreen = m.boundStatsScreen().Apply(stats.Payload{Summary: r.statsSummary})
	case realtimeReloadInsights:
		if !r.insightsValid {
			return false
		}
		m.insightsScreen = m.boundInsightsScreen().Apply(insightsPayload(r.insights, m.insightsBucketNames()))
	case realtimeReloadLogs:
		if !r.logsValid {
			return false
		}
		m.logsScreen = m.boundLogsScreen().Apply(logs.Payload{Rows: r.events, Stats: r.logsStats})
	default:
		return false
	}
	return true
}

func (m *Model) foldRealtimeBundle(r realtimeReloadMsg) bool {
	if !r.snapValid {
		return false
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
		m.plansScreen = m.boundPlansScreen().Apply(r.plans, nil)
	}
	m.clampSelection()
	if r.anchorID > 0 {
		m.selectTaskByID(r.anchorID)
	}
	return true
}

func (m *Model) refreshCurrentView() error {
	switch m.activeReloadPolicy() {
	case screenhost.ReloadHome:
		return m.reloadHome()
	case screenhost.ReloadLogs:
		return m.refreshActivityLogs()
	case screenhost.ReloadStats:
		return m.refreshStats()
	case screenhost.ReloadInsights:
		return m.refreshInsights()
	default:
		return m.refreshPreservingTaskSelection()
	}
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
	// The resolved view settings are root-owned state that other surfaces read,
	// so they are still refreshed here; the screen receives its own copy
	// through boundLogsScreen.
	m.views = m.activeViewSettings()
	payload, err := m.loadLogsPayload(m.boundLogsScreen().Filter())
	if err != nil {
		return err
	}
	m.logsScreen = m.boundLogsScreen().Apply(payload)
	return nil
}

func (m *Model) refreshStats() error {
	port := m.metricsPort()
	if port == nil {
		return nil
	}
	summary, err := port.Summary(m.ctx, m.project, m.boundStatsScreen().Period(), 0)
	if err != nil {
		return err
	}
	m.statsScreen = m.boundStatsScreen().Apply(stats.Payload{Summary: summary})
	return nil
}

// refreshInsights re-fetches the intelligence-layer reading for the
// Stats › Insights sub-mode. Mirrors refreshStats: a no-op when the
// service is unwired (stub fixtures), and it never queries directly —
// all six insights come from InsightsService.Today. stuckDays=0 takes
// the service default (DefaultStuckDays).
func (m *Model) refreshInsights() error {
	port := m.insightsPort()
	if port == nil {
		return nil
	}
	reading, err := port.Today(m.ctx, m.project, m.project.ID, 0, domain.StuckBucketIDs(m.workflow))
	if err != nil {
		return err
	}
	m.insightsScreen = m.boundInsightsScreen().Apply(insightsPayload(reading, m.insightsBucketNames()))
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
		view = overlay.Overlay(view, m.notification.View(), m.notification.Position())
	case m.paletteOpen:
		view = normalizeViewToTerminal(view, m.width, m.height)
		view = overlay.Overlay(view, m.renderPaletteOverlay(), overlay.PositionCenter)
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
	return m.paintPaletteOverlay(m.palette.View())
}

// paintPaletteOverlay is the shared frame painter for the live overlay and
// for paletteOverlayFrameRows. Keeping one constructor means the budget and
// the paint cannot disagree about border, kicker, hint or padding.
func (m Model) paintPaletteOverlay(body string) string {
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

// paletteOverlayFrameRows is the terminal rows paintPaletteOverlay adds
// around its body (border, kicker, hint, blank separator). Measured off the
// same painter the overlay uses, with MaxHeight left off so a tall body
// cannot clip the measurement.
func (m Model) paletteOverlayFrameRows() int {
	const probe = "x"
	kicker := m.styles.kicker("palette")
	hint := m.styles.hint.Render("enter submit · tab toggles tabs · esc close")
	panel := lipgloss.JoinVertical(lipgloss.Left, kicker, hint, "", probe)
	styled := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.hintAccent.GetForeground()).
		Padding(0, 2).
		Width(48).
		Render(panel)
	return lipgloss.Height(styled) - lipgloss.Height(probe)
}

// paletteResultRowsBudget computes the maximum number of result
// rows the palette overlay can render given the current terminal
// height. Chrome is measured: the overlay frame (border + kicker +
// hint + blank) plus the palette package's non-result rows (tabs,
// input, result-list headers, ↑/↓ indicators, status). Floor at 3
// so the cursor row plus a sliver of context always fits even on a
// one-line terminal; zero (unlimited) is reserved for the
// pre-resize path.
func (m Model) paletteResultRowsBudget() int {
	const floor = 3
	if m.height <= 0 {
		return 0
	}
	chrome := m.paletteOverlayFrameRows() + m.palette.ChromeRowsForResultsBudget()
	budget := m.height - chrome
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
		// Help owns the middle slot; its footer is the bottom anchor.
		return header.Stack(m.height, m.renderHeader(), m.renderHelp(), m.renderHelpFooter())
	}
	if m.mode != modeNormal && !m.isFullPanelTextareaInput() {
		top := m.renderHeader() + "\n" + m.renderInput()
		return header.Stack(m.height, top, m.renderCurrentView(), m.renderFooter())
	}

	top := m.renderHeader()
	if m.status != "" {
		top += "\n  " + m.styles.statusBadge(m.status)
	}
	return header.Stack(m.height, top, m.renderCurrentView(), m.renderFooter())
}

// isFullPanelTextareaInput is true while a modal owns the whole main
// panel (not a single-line top-bar input). The plan goal editor is one
// such mode — it renders the textarea inside renderPlanNetwork, so the
// chrome must NOT also stack renderInput above it.
func (m Model) isFullPanelTextareaInput() bool {
	return false
}

// dispatchNotification routes notification-related messages to the live notification
// model when present, and intercepts ShowMsg / DismissedMsg to flip
// the notification slot. handled=true means the parent's regular dispatch
// should stop — notification is intentionally exclusive while active so
// dismiss + scroll keys take priority over the app underneath.
func (m Model) dispatchNotification(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if showMsg, ok := msg.(hookactions.NotificationShowMsg); ok {
		return m.dispatchNotificationShow(showMsg)
	}
	if dm, ok := msg.(DismissedMsg); ok {
		return m.dispatchNotificationDismissed(dm)
	}
	if am, ok := msg.(ActionMsg); ok {
		return m.dispatchNotificationAction(am)
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

func (m Model) dispatchNotificationShow(msg hookactions.NotificationShowMsg) (tea.Model, tea.Cmd, bool) {
	if m.notification != nil && m.notification.State() == notificationStateAppearing {
		return m, nil, true
	}
	bm, cmd := newNotification(notificationOptions{
		Notification: msg.Notification,
		Theme:        m.theme,
		Text:         msg.Text,
		DetailText:   msg.DetailText,
		Catalog:      m.repos.Catalog})
	m.notification = &bm
	return m, cmd, true
}

func (m Model) dispatchNotificationDismissed(msg DismissedMsg) (tea.Model, tea.Cmd, bool) {
	if m.notification != nil && msg.ID == m.notification.ID() {
		m.notification = nil
	}
	m.homeScreen = m.homeScreen.CancelDelete()
	m.revertConfigSwap()
	return m, nil, true
}

func (m Model) dispatchNotificationAction(msg ActionMsg) (tea.Model, tea.Cmd, bool) {
	if m.notification != nil && msg.ID == m.notification.ID() {
		m.notification = nil
	}
	m.pendingSwapRevertPath = ""
	if msg.Slug == "home-project-delete-confirm" {
		return m, m.handleHomeProjectDeleteAction(msg), true
	}
	if msg.Slug == "kitten_orphan_migration" {
		m.handleOrphanMigrationAction(msg)
		return m, nil, true
	}
	m.handleNotificationAction(msg)
	return m, nil, true
}

func (m Model) availableWidth() int {
	return m.screenKit().AvailableWidth()
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

func (m *Model) handleCommonKey(msg tea.KeyMsg) bool {
	key := msg.String()
	if commonKeyBlursScreen(key) || keynav.Default.Tops.Has(key) || keynav.Default.Subs.Has(key) {
		m.blurActiveScreen()
	}
	if m.handleCommonRouteKey(key) || m.handleCommonTaskKey(key) {
		return true
	}
	return m.handleCommonNavigationKey(key)
}

func commonKeyBlursScreen(key string) bool {
	switch key {
	case "ctrl+h", "0", "ctrl+p", "ctrl+o", "1", "2", "3", ",":
		return true
	default:
		return false
	}
}

func (m *Model) handleCommonRouteKey(key string) bool {
	switch key {
	case "ctrl+h", "0":
		wasHome := m.onHome()
		m.status = ""
		m.pushHistory()
		m.navigation = firstSub(screenhost.TopHome)
		if err := m.reloadHome(); err != nil {
			m.status = err.Error()
		} else if wasHome {
			m.status = m.t("tui.status.refreshed")
		}
		return true
	case "ctrl+p":
		m.openProjectView()
		return true
	case "ctrl+o":
		if m.popHistory() {
			m.status = ""
		}
		return true
	case "1":
		m.pushHistory()
		m.jumpTop(screenhost.TopTasks)
		return true
	case "2":
		m.pushHistory()
		m.jumpTop(screenhost.TopStats)
		return true
	case "3":
		m.pushHistory()
		m.jumpTop(screenhost.TopStudio)
		return true
	case "4":
		m.pushHistory()
		m.jumpTop(screenhost.TopSettings)
		return true
	case ",":
		m.pushHistory()
		m.cycleSub(-1)
		return true
	default:
		return false
	}
}

func (m *Model) handleCommonTaskKey(key string) bool {
	switch key {
	case "n":
		return m.handleNewTaskKey()
	case "e":
		return m.handleEditTaskKey()
	case "c":
		return m.handleCommentTaskKey()
	case "r":
		return m.handleRefreshKey()
	case "A":
		return m.handleArchivedKey()
	default:
		return false
	}
}

func (m *Model) taskKeyActive() bool {
	if m.navigationTop() != screenhost.TopTasks || m.inPlanNetwork() {
		return false
	}
	return true
}

func (m *Model) handleNewTaskKey() bool {
	if !m.taskKeyActive() {
		return false
	}
	if screen, ok := m.activeHostedScreen(); ok && m.denyProductKey(screen, "n") {
		return true
	}
	m.openTaskCreate()
	return true
}

func (m *Model) handleEditTaskKey() bool {
	if !m.taskKeyActive() {
		return false
	}
	if screen, ok := m.activeHostedScreen(); ok && m.denyProductKey(screen, "e") {
		return true
	}
	if task, ok := m.selectedTask(); ok {
		m.openTaskEdit(task)
	}
	return true
}

func (m *Model) handleCommentTaskKey() bool {
	if !m.taskKeyActive() {
		return false
	}
	if screen, ok := m.activeHostedScreen(); ok && m.denyProductKey(screen, "c") {
		return true
	}
	if _, ok := m.selectedTask(); ok {
		m.beginInput(modeComment, m.t("tui.input.comment_body"), "")
	}
	return true
}

func (m *Model) handleRefreshKey() bool {
	if m.inPlanNetwork() {
		return false
	}
	if err := m.refreshCurrentView(); err != nil {
		m.status = err.Error()
	} else {
		m.status = m.t("tui.status.refreshed")
	}
	return true
}

func (m *Model) handleArchivedKey() bool {
	if m.navigationTop() != screenhost.TopTasks {
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

func (m *Model) handleCommonNavigationKey(key string) bool {
	if keynav.Default.Tops.Has(key) {
		m.pushHistory()
		m.cycleTop(1)
		return true
	}
	if keynav.Default.Subs.Has(key) {
		m.pushHistory()
		m.cycleSub(1)
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
	return len(m.screenStack) > 0 && m.screenStack[len(m.screenStack)-1] == screenhost.PlanNetwork
}

// cycleTop advances the active top by delta positions (positive forward,
// negative backward) along topOrder. The sub always lands on the first
// sub of the new top — there is no per-top "last sub used" memory in T1.
func (m *Model) cycleTop(delta int) {
	idx := topIndex(m.navigationTop())
	if idx < 0 {
		idx = 0
	}
	n := len(topOrder)
	next := topOrder[((idx+delta)%n+n)%n]
	m.navigation = firstSub(next)
}

// jumpTop moves directly to a target top (bound to the digit keys 1/2/3),
// landing on its first sub. No-op when the model is already on that top
// and its first sub — keeps repeated digit presses from clobbering nav.
func (m *Model) jumpTop(target screenhost.TopID) {
	if m.navigationTop() == target {
		return
	}
	m.navigation = firstSub(target)
}

// cycleSub moves the active sub forward (delta=1) or backward (delta=-1)
// inside the current top. No-op when the top exposes a single sub — the
// binding is silently dropped so users on a single-sub top do not have
// to learn "this only works on Tasks/Stats/Settings".
func (m *Model) cycleSub(delta int) {
	subs := subsByTop[m.navigationTop()]
	if len(subs) <= 1 {
		return
	}
	idx := subIndex(m.navigationTop(), m.navigation)
	if idx < 0 {
		idx = 0
	}
	n := len(subs)
	m.navigation = subs[((idx+delta)%n+n)%n]
}

func (m *Model) refresh() error {
	m.relationshipPickerGeneration++
	views := m.activeViewSettings()
	m.views = views

	// Phase 2-bis routes every per-project view through the BundleCache —
	// production wires one at boot, tests wire one via
	// testfixtures/runtimecache.Install. Reads hit r.Cache.View(r.ProjectID).Snapshot
	// unconditionally.

	snap, err := m.loadBoardSnapshot(m.ctx, m.project, domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order}, m.includeArchived)
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
		m.languages = bundleSnap.LanguageSettings()
	}
	if rollups, plansErr := m.loadPlanRollups(m.ctx, m.project); m.repos.operationService() != nil {
		m.plansScreen = m.boundPlansScreen().Apply(rollups, plansErr)
	}
	m.clampSelection()
	m.boardScreen = m.boundBoardScreen()
	return nil
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
