package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/picker"
	"omakiten/internal/tui/components/viewport"
)

func NewModel(ctx context.Context, project domain.ProjectContext, repos Repositories, theme config.Theme, counter token.Counter, badge config.TokenBadgeThresholds) (Model, error) {
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
		entityKind:       entityKindLaw,
		entityCursors:    map[entityKind]int{entityKindLaw: 0, entityKindPersona: 0, entityKindSkill: 0, entityKindTag: 0},
		homePicker:       picker.New(picker.Single),
	}
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncFocusedColumnScroll()
		m.syncBoardColScroll()
		m.syncFocusedEntityScroll()
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
	case tea.KeyMsg:
		if m.helpOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "a":
				m.helpAll = !m.helpAll
				m.help.Scroll = 0
			case "?", "q":
				m.helpOpen = false
				m.helpAll = false
				m.help.Scroll = 0
			default:
				// Delegate scroll keys (j/k/pgup/pgdn/g/G) and esc to the
				// embedded viewport sub-model. Esc surfaces as EventCancel
				// which we treat as "close the overlay".
				m.help, _ = m.help.Update(msg, m.helpViewportRows())
				if m.help.LastEvent() == viewport.EventCancel {
					m.helpOpen = false
					m.helpAll = false
					m.help.Scroll = 0
				}
			}
			return m, nil
		}
		if msg.String() == "?" && m.mode == modeNormal {
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
			m.handleHomeKey(msg)
			return m, nil
		}
		if m.handleCommonKey(msg) {
			m.refreshAfterViewChange(prevNav)
			return m, nil
		}
		switch m.sub {
		case subBoard:
			m.handleBoardKey(msg)
		case subTable:
			m.handleListKey(msg)
		case subGraph:
			m.handleGraphKey(msg)
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
	m.refreshAfterViewChange(prevNav)
	return m, nil
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

func (m *Model) refreshAfterViewChange(prev navState) {
	if m.top == prev.top && m.sub == prev.sub {
		return
	}
	if err := m.refreshCurrentView(); err != nil {
		m.status = err.Error()
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

// activeViewSettings reads the resolved per-view sort/filter from the active
// bundle. When the bundle editor is not wired (tests, headless callers) it
// falls back to the canonical defaults so the TUI behaves the same way.
func (m *Model) activeViewSettings() config.ViewSettings {
	if m.repos.Editor == nil {
		return config.Settings{}.EffectiveViews()
	}
	bundle, err := m.repos.Editor.Load()
	if err != nil {
		return config.Settings{}.EffectiveViews()
	}
	return bundle.Config.EffectiveViews()
}

func (m Model) View() string {
	if m.helpOpen {
		return clampViewToHeight(m.height, m.renderHeader(), m.renderHelp(), m.renderHelpFooter())
	}
	if m.mode != modeNormal && !m.isEmbeddedCommentInput() {
		return clampViewToHeight(m.height, m.renderHeader(), m.renderInput(), m.renderCurrentView(), m.renderFooter())
	}

	parts := []string{m.renderHeader()}
	if m.status != "" && !m.isEmbeddedCommentInput() {
		parts = append(parts, "  "+m.styles.statusBadge(m.status))
	}
	parts = append(parts, m.renderCurrentView(), m.renderFooter())
	return clampViewToHeight(m.height, parts...)
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
// occasionally tipped past the box's inner edge and wrapLinesToWidth would
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
	return m.mode == modeComment && m.taskScreen == taskScreenView && m.taskID > 0
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
		if m.top != topTasks {
			return false
		}
		m.openTaskCreate()
		return true
	case "e":
		if m.top != topTasks {
			return false
		}
		if task, ok := m.selectedTask(); ok {
			m.openTaskEdit(task)
		}
		return true
	case "c":
		if m.top != topTasks {
			return false
		}
		if _, ok := m.selectedTask(); ok {
			m.beginInput(modeComment, "Comment body", "")
		}
		return true
	case "r":
		if err := m.refreshCurrentView(); err != nil {
			m.status = err.Error()
		} else {
			m.status = "Refreshed"
		}
		return true
	}
	return false
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

	query := app.NewTUIQueryService(m.repos.Tasks, m.repos.Config, m.repos.Dependencies, m.repos.Comments, m.repos.Entries, m.repos.Tags, m.repos.Editor)
	snap, err := query.Snapshot(m.ctx, m.project, domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order})
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
	m.clampSelection()
	m.clampCardIdx()
	m.clampEntityCursor()
	m.syncSelectedFromBoard()
	return nil
}

func (m Model) computeMetrics(maxTokens int) domain.TokenMetrics {
	total := 0
	for _, entry := range m.entries {
		total += entry.TokenEstimate
	}
	for _, law := range m.laws {
		total += m.counter.Count(law.Key + " " + law.Body)
	}
	for _, persona := range m.personas {
		// Persona descriptions count toward the budget; skill bodies do not.
		total += m.counter.Count(persona.Description)
	}
	for _, comment := range m.comments {
		total += m.counter.Count(comment.Body)
	}
	return domain.TokenMetrics{EstimatedTotal: total, MaxTokens: maxTokens, Truncated: maxTokens > 0 && total > maxTokens}
}
