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
		entityCursors:    map[entityKind]int{entityKindLaw: 0, entityKindPersona: 0, entityKindSkill: 0, entityKindTag: 0},
		homePicker:       picker.New(picker.Single),
		priorities:       priorities,
		severities:       severities,
		registry:         domain.NewEnumRegistry(priorityPairs, severityPairs),
		markdown:         newMarkdownRenderer(tokensFromTheme(theme)),
		markdownRendered: true,
		notifications:    notifications.Notifications,
		// Pre-allocated style-by-kind-by-width cache; value-receiver
		// render paths read + write through this so the
		// lipgloss.Style.Width(N) allocation only fires once per
		// (kind, width) pair across the lifetime of the model. Inner
		// maps lazily fill on first write per kind.
		styleByKindWidth:     map[styleKind]map[int]lipgloss.Style{},
		tokenCountCache:      map[uint64]int{},
		subtasks:             cardlist.New(),
		planNetwork:          cardlist.New(),
		activityLines:        linelist.New(),
		logsList:             linelist.New(),
		tableList:            linelist.New(),
		graphList:            linelist.New(),
		graphCursor:          cursorwindow.New(0),
		plansCursor:          cursorwindow.New(0),
		planNetworkCursor:    cursorwindow.New(0),
		settingsGeneralLines: linelist.New(),
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
	case refreshTickMsg:
		if m.shouldRealtimeRefresh() {
			// Realtime tick is renderer-driven, not user-triggered, so
			// every app-service call it spawns (`MetricsService.Summary`,
			// `Logs.List`) bypasses the activity tracker. Otherwise the
			// log viewer fills with one row per second and pushes real
			// agent activity out of the bounded window. The footer hint
			// already advertises this contract.
			savedCtx := m.ctx
			m.ctx = activity.WithoutTracking(m.ctx)
			if err := m.refreshCurrentView(); err != nil {
				m.status = err.Error()
			}
			m.ctx = savedCtx
		}
		return m, scheduleRefreshTick()
	case editorFinishedMsg:
		m.handleEditorFinished(msg)
		return m, nil
	case palette.DismissMsg:
		m.paletteOpen = false
		return m, nil
	case paletteSearchResultMsg:
		// Async tail of dispatchPaletteSearch. Drop the result
		// silently when the user closed the palette in the
		// meantime — SetStatus on a closed overlay would leak
		// stale text into the next open.
		if !m.paletteOpen {
			return m, nil
		}
		m.palette.SetStatus(msg.status)
		return m, nil
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
		case subSettingsGeneral:
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

func (m Model) shouldRealtimeRefresh() bool {
	if m.onHome() {
		// Home reads cross-project metadata (tags, pending counts) — refresh
		// is driven by ctrl+h / startup, not by the per-project tick.
		return false
	}
	return !m.helpOpen && m.mode == modeNormal && m.taskScreen == taskScreenClosed && m.entityScreen == entityScreenClosed && !m.moveMode
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
	if m.commentScreenOpen || m.descriptionScreenOpen {
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
	query := app.NewTUIQueryService(m.repos.Tasks, cfgSnap, m.repos.Dependencies, m.repos.Comments, m.repos.Entries, m.repos.Tags)
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
	m.entries = snap.Entries
	m.tags = snap.AllTags
	m.taskTagsMap = snap.TaskTagsByID
	m.metrics = m.computeMetrics(snap.Settings.MaxTokens)
	m.languages = r.langs
	if r.plansValid {
		m.plans = r.plans
	}
	m.invalidateBoardCaches()
	m.rebuildBoardCaches()
	m.clampPlanCursor()
	m.clampSelection()
	m.clampCardIdx()
	m.clampEntityCursor()
	m.syncSelectedFromBoard()
	if r.preservedTaskID > 0 {
		m.selectTaskByID(r.preservedTaskID)
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
	if m.repos.ActivityLogs == nil {
		return nil
	}
	views := m.activeViewSettings()
	m.views = views
	sources := make([]domain.ActivitySource, 0, len(views.Logs.Filter.Source))
	for _, src := range views.Logs.Filter.Source {
		sources = append(sources, domain.ActivitySource(src))
	}
	listFilter := domain.ActivityLogFilter{
		ProjectID: m.project.ID,
		Limit:     views.Logs.Limit,
		Order:     views.Logs.Sort.Order,
		Sources:   sources,
	}
	logs, err := m.repos.ActivityLogs.ListActivityLogs(m.ctx, listFilter)
	if err != nil {
		return err
	}
	m.logs = logs
	// Summary tables aggregate the full project history (no limit, no
	// view source filter), so the headline numbers reflect everything
	// the project has logged — not just whichever rows happen to fit
	// in the panel beneath.
	statsFilter := domain.ActivityLogFilter{ProjectID: m.project.ID}
	stats, err := m.repos.ActivityLogs.ActivityLogStats(m.ctx, statsFilter)
	if err != nil {
		return err
	}
	m.logsStats = stats
	return nil
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
	if m.paletteOpen {
		view = normalizeViewToTerminal(view, m.width, m.height)
		view = notification.Overlay(view, m.renderPaletteOverlay(), notification.PositionCenter)
	}
	if m.notification != nil {
		view = normalizeViewToTerminal(view, m.width, m.height)
		view = notification.Overlay(view, m.notification.View(), m.notification.Position())
	}
	return view
}

// renderPaletteOverlay wraps palette.Model.View output in a bordered
// panel matching the theme's accent so the overlay reads as a modal
// floating above the base render. Width is fixed at 48 cells — wide
// enough for `verb:operand` + an inline status, narrow enough to fit
// without clipping on standard 80-column terminals.
func (m Model) renderPaletteOverlay() string {
	body := m.palette.View()
	kicker := m.styles.kicker("palette")
	hint := m.styles.hint.Render("enter submit · tab toggles tabs · esc close")
	panel := lipgloss.JoinVertical(lipgloss.Left, kicker, hint, "", body)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.hintAccent.GetForeground()).
		Padding(0, 2).
		Width(48).
		Render(panel)
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

	query := app.NewTUIQueryService(m.repos.Tasks, m.repos.activeSnapshot(), m.repos.Dependencies, m.repos.Comments, m.repos.Entries, m.repos.Tags)
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
	m.entries = snap.Entries
	m.tags = snap.AllTags
	m.taskTagsMap = snap.TaskTagsByID
	m.metrics = m.computeMetrics(snap.Settings.MaxTokens)
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

func (m Model) computeMetrics(maxTokens int) domain.TokenMetrics {
	total := 0
	for _, entry := range m.entries {
		total += entry.TokenEstimate
	}
	for _, law := range m.laws {
		total += m.countTokens(law.Key + " " + law.Body)
	}
	for _, persona := range m.personas {
		// Persona descriptions count toward the budget; skill bodies do not.
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
