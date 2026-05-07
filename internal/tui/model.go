package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/picker"
	"omakiten/internal/tui/components/viewport"
)

func NewModel(ctx context.Context, project domain.ProjectContext, repos Repositories, theme config.Theme, counter token.Counter) (Model, error) {
	if counter == nil {
		counter = token.ApproxCounter{}
	}
	model := Model{
		ctx:           ctx,
		project:       project,
		repos:         repos,
		theme:         theme,
		styles:        newStyles(theme),
		counter:       counter,
		entityKind:    entityKindLaw,
		entityCursors: map[entityKind]int{entityKindLaw: 0, entityKindPersona: 0, entityKindSkill: 0, entityKindTag: 0},
		homePicker:    picker.New(picker.Single),
	}
	detailscreen.SetStyles(model.styles.info)
	if project.ID == 0 {
		// Empty project — open on the multi-project Home picker.
		// Do not call refresh() because every per-project query would 404
		// without a resolved project_id.
		model.view = viewHome
		if err := model.loadHome(); err != nil {
			return Model{}, err
		}
		return model, nil
	}
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
	prevView := m.view
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncFocusedColumnScroll()
		m.syncBoardColScroll()
		m.syncEntityKindScroll()
	case refreshTickMsg:
		if m.shouldRealtimeRefresh() {
			if err := m.refreshCurrentView(); err != nil {
				m.status = err.Error()
			}
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
		if m.view == viewHome {
			m.handleHomeKey(msg)
			return m, nil
		}
		if m.handleCommonKey(msg) {
			m.refreshAfterViewChange(prevView)
			return m, nil
		}
		switch m.view {
		case 0:
			m.handleBoardKey(msg)
		case 2:
			m.handleGraphKey(msg)
		case 3:
			if cmd := m.handleConfigKey(msg); cmd != nil {
				return m, cmd
			}
		case 4:
			m.handleLogsKey(msg)
		case 5:
			m.handleStatsKey(msg)
		default:
			m.handleListKey(msg)
		}
	}
	m.refreshAfterViewChange(prevView)
	return m, nil
}

func scheduleRefreshTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

func (m Model) shouldRealtimeRefresh() bool {
	if m.view == viewHome {
		// Home reads cross-project metadata (tags, pending counts) — refresh
		// is driven by ctrl+h / startup, not by the per-project tick.
		return false
	}
	return !m.helpOpen && m.mode == modeNormal && m.taskScreen == taskScreenClosed && m.entityScreen == entityScreenClosed && !m.moveMode
}

func (m *Model) refreshAfterViewChange(prevView int) {
	if m.view == prevView {
		return
	}
	if err := m.refreshCurrentView(); err != nil {
		m.status = err.Error()
	}
}

func (m *Model) refreshCurrentView() error {
	if m.view == viewHome {
		return m.loadHome()
	}
	if m.view == 4 {
		return m.refreshActivityLogs()
	}
	if m.view == 5 {
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
	logs, err := m.repos.ActivityLogs.ListActivityLogs(m.ctx, domain.ActivityLogFilter{
		Limit:   views.Logs.Limit,
		Order:   views.Logs.Sort.Order,
		Sources: sources,
	})
	if err != nil {
		return err
	}
	m.logs = logs
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
	case "ctrl+h":
		// Multi-project Home is reachable from every per-project view. The
		// underlying picker keeps its prior cursor so re-entry feels stable;
		// we do reload tags/pending counts so a freshly-edited project row
		// reflects the latest state.
		m.moveMode = false
		m.status = ""
		m.view = viewHome
		if err := m.loadHome(); err != nil {
			m.status = err.Error()
		}
		return true
	case "esc":
		if m.moveMode {
			m.moveMode = false
			m.status = ""
			return true
		}
	case "tab":
		m.view = (m.view + 1) % len(viewNames)
		m.moveMode = false
		return true
	case "shift+tab":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
		m.moveMode = false
		return true
	case "1", "2", "3", "4", "5", "6":
		m.view = int(msg.String()[0] - '1')
		m.moveMode = false
		return true
	case "n":
		if m.view == 3 || m.view == 4 || m.view == 5 {
			return false
		}
		m.openTaskCreate()
		return true
	case "e":
		if m.view == 3 || m.view == 4 || m.view == 5 {
			return false
		}
		if task, ok := m.selectedTask(); ok {
			m.openTaskEdit(task)
		}
		return true
	case "c":
		if m.view == 3 || m.view == 4 || m.view == 5 {
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
