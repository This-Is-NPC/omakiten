package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/token"
)

const (
	columnWidth                = 28
	taskDetailsPanelWidth      = 40
	taskCommentsPanelWidth     = 44
	commentInputHeight         = 5
	taskFormInputWidth         = 72
	taskDescriptionInputHeight = 8
	metaRowLabelWidth          = 14
	selectionMarker            = "▌"
	normalMarker               = " "
	cardBoxWidth               = 26
	cardContentWidth           = 24 // cardBoxWidth - horizontal padding(2); text fits here
	commentCardContentWidth    = taskCommentsPanelWidth - 10 // card Width(36) - padding(2) - border(2)
)

type Repositories struct {
	Tasks        app.TaskRepository
	Comments     app.CommentRepository
	Dependencies app.DependencyRepository
	Entries      app.ContextEntryRepository
	Config       app.ConfigRepository
	Tags         app.TagRepository
	Editor       *app.BundleEditor
	ActivityLogs activity.ActivityLogRepository
}

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
	blockerPickerCursor int
	blockerPickerChecks map[int64]bool

	tasks                  []domain.Task
	workflow               domain.Workflow
	dependencies           []domain.TaskDependency
	comments               []domain.Comment
	laws                   []domain.Law
	skills                 []domain.Skill
	personas               []domain.Persona
	templates              []config.TaskTemplate
	activeTaskTemplateSlug string
	themePickerOptions     []themeOption
	configPickerOptions    []configOption
	entries                []domain.ContextEntry
	tags                   []domain.Tag
	taskTagsMap            map[int64][]domain.Tag
	metrics      domain.TokenMetrics
	selected     int
	colIdx       int
	cardIdx      int

	entityKind    entityKind
	entityCursors map[entityKind]int
	entityScreen  entityScreenMode
	entityForm    entityForm
	deletePending bool
	deleteKind    entityKind
	deleteSlug    string

	logs         []domain.ActivityLog
	logsSelected int

	taskViewScroll int

	// boardScroll holds a per-bucket scroll offset (in cards) so long columns
	// can be scrolled vertically without losing context when navigating between
	// lanes. Keys are bucket keys (domain.Bucket.Key).
	boardScroll map[string]int

	entityViewScroll int

	logsScroll int

	tableScroll int

	graphScroll  int
	graphCursor  int

	helpScroll int

	pickerScroll int

	blockerPickerScroll int
}

type inputMode int

const (
	modeNormal inputMode = iota
	modeComment
	modeMove
)

type taskScreenMode int

const (
	taskScreenClosed taskScreenMode = iota
	taskScreenView
	taskScreenCreate
	taskScreenEdit
)

type taskFormField int

const (
	taskFieldTitle taskFormField = iota
	taskFieldDescription
	taskFieldPriority
)

var viewNames = []string{"BOARD", "TABLE", "GRAPH", "CONFIG", "LOGS"}

type refreshTickMsg struct{}

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
	}
	if err := model.refresh(); err != nil {
		return Model{}, err
	}
	return model, nil
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
				m.helpScroll = 0
			case "?", "esc", "q":
				m.helpOpen = false
				m.helpAll = false
				m.helpScroll = 0
			case "j", "down":
				m.helpScroll++
			case "k", "up":
				if m.helpScroll > 0 {
					m.helpScroll--
				}
			case "pgdown", "ctrl+d":
				m.helpScroll += taskViewPageStep(m.helpViewportRows())
			case "pgup", "ctrl+u":
				m.helpScroll -= taskViewPageStep(m.helpViewportRows())
				if m.helpScroll < 0 {
					m.helpScroll = 0
				}
			case "home", "g":
				m.helpScroll = 0
			case "end", "G":
				m.helpScroll = 1 << 20
			}
			return m, nil
		}
		if msg.String() == "?" && m.mode == modeNormal {
			m.helpOpen = true
			m.helpAll = false
			m.helpScroll = 0
			return m, nil
		}
		if m.mode != modeNormal {
			return m.updateInput(msg)
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
	if m.view == 4 {
		return m.refreshActivityLogs()
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
	logs, err := m.repos.ActivityLogs.ListActivityLogs(m.ctx, domain.ActivityLogFilter{Limit: 50})
	if err != nil {
		return err
	}
	m.logs = logs
	return nil
}

func (m Model) View() string {
	if m.helpOpen {
		return strings.Join([]string{m.renderHeader(), m.renderHelp(), m.renderHelpFooter()}, "\n")
	}
	if m.mode != modeNormal && !m.isEmbeddedCommentInput() {
		return strings.Join([]string{m.renderHeader(), m.renderInput(), m.renderCurrentView(), m.renderFooter()}, "\n")
	}

	parts := []string{m.renderHeader()}
	if m.status != "" && !m.isEmbeddedCommentInput() {
		parts = append(parts, "  "+m.styles.statusBadge(m.status))
	}
	parts = append(parts, m.renderCurrentView(), m.renderFooter())
	return strings.Join(parts, "\n")
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
	return clampInt(m.availableWidth()-8, 24, taskCommentsPanelWidth-8)
}

func (m Model) hintBoxWidth() int {
	return clampInt(m.availableWidth()-8, 32, 60)
}

func (m Model) isEmbeddedCommentInput() bool {
	return m.mode == modeComment && m.taskScreen == taskScreenView && m.taskID > 0
}

func (m *Model) handleCommonKey(msg tea.KeyMsg) bool {
	switch msg.String() {
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
	case "1", "2", "3", "4", "5":
		m.view = int(msg.String()[0] - '1')
		m.moveMode = false
		return true
	case "n":
		if m.view == 3 || m.view == 4 {
			return false
		}
		m.openTaskCreate()
		return true
	case "e":
		if m.view == 3 || m.view == 4 {
			return false
		}
		if task, ok := m.selectedTask(); ok {
			m.openTaskEdit(task)
		}
		return true
	case "c":
		if m.view == 3 || m.view == 4 {
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

func (m *Model) handleBoardKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "enter":
		if task, ok := m.selectedTask(); ok {
			m.openTaskView(task)
		}
	case "m":
		if _, ok := m.selectedTask(); ok {
			m.moveMode = !m.moveMode
			if m.moveMode {
				m.status = "Move mode: left/right moves the selected task"
			} else {
				m.status = "Move cancelled"
			}
		}
	case "left", "h":
		if m.moveMode {
			m.moveSelectedToColumn(m.colIdx - 1)
			return
		}
		if m.colIdx > 0 {
			m.colIdx--
			m.clampCardIdx()
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	case "right", "l":
		if m.moveMode {
			m.moveSelectedToColumn(m.colIdx + 1)
			return
		}
		if m.colIdx < len(m.workflow.Buckets)-1 {
			m.colIdx++
			m.clampCardIdx()
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	case "up", "k":
		if m.cardIdx > 0 {
			m.cardIdx--
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	case "down", "j":
		bucketTasks := m.tasksInCurrentBucket()
		if m.cardIdx < len(bucketTasks)-1 {
			m.cardIdx++
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	case "pgup", "ctrl+u":
		m.cardIdx -= boardScrollPageStep(m)
		if m.cardIdx < 0 {
			m.cardIdx = 0
		}
		m.syncSelectedFromBoard()
		m.syncFocusedColumnScroll()
	case "pgdown", "ctrl+d":
		bucketTasks := m.tasksInCurrentBucket()
		m.cardIdx += boardScrollPageStep(m)
		if m.cardIdx > len(bucketTasks)-1 {
			m.cardIdx = len(bucketTasks) - 1
		}
		if m.cardIdx < 0 {
			m.cardIdx = 0
		}
		m.syncSelectedFromBoard()
		m.syncFocusedColumnScroll()
	case "home", "g":
		m.cardIdx = 0
		m.syncSelectedFromBoard()
		m.syncFocusedColumnScroll()
	case "end", "G":
		bucketTasks := m.tasksInCurrentBucket()
		if len(bucketTasks) > 0 {
			m.cardIdx = len(bucketTasks) - 1
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	}
}

// syncFocusedColumnScroll keeps m.boardScroll[focusedBucket] aligned so the
// selected card stays fully visible inside the column viewport. Rendered card
// heights vary (1- vs 2-line titles, badges line) so we render each card to
// measure the actual height instead of using an approximation, otherwise
// `down` arrow lags behind the cursor by ~1 card.
func (m *Model) syncFocusedColumnScroll() {
	bucket, ok := m.focusedBucketKey()
	if !ok {
		return
	}
	viewport := m.boardViewportRows()
	if viewport <= 0 {
		return
	}
	tasks := m.tasksInCurrentBucket()
	if len(tasks) == 0 {
		if m.boardScroll != nil {
			delete(m.boardScroll, bucket)
		}
		return
	}

	layout := m.computeBoardLayout(len(m.workflow.Buckets))
	heights := make([]int, len(tasks))
	for i, task := range tasks {
		rendered := m.renderCard(task, false, layout)
		heights[i] = strings.Count(rendered, "\n") + 1
	}

	if m.boardScroll == nil {
		m.boardScroll = map[string]int{}
	}
	offset := m.boardScroll[bucket]
	if offset > m.cardIdx {
		offset = m.cardIdx
	}
	for offset < m.cardIdx {
		used := 0
		fits := true
		for i := offset; i <= m.cardIdx; i++ {
			used += heights[i]
			// Reserve 1 row for the "▼ N below" hint when more cards remain.
			reserve := 0
			if i < len(tasks)-1 {
				reserve = 1
			}
			if used+reserve > viewport {
				fits = false
				break
			}
		}
		if fits {
			break
		}
		offset++
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(tasks)-1 {
		offset = len(tasks) - 1
	}
	m.boardScroll[bucket] = offset
}

func (m Model) focusedBucketKey() (string, bool) {
	if len(m.workflow.Buckets) == 0 || m.colIdx < 0 || m.colIdx >= len(m.workflow.Buckets) {
		return "", false
	}
	return m.workflow.Buckets[m.colIdx].Key, true
}

func boardScrollPageStep(m *Model) int {
	step := m.boardViewportRows() / 8 // each card is ~4 rows; half-page ≈ rows/8 cards
	if step < 2 {
		return 2
	}
	return step
}

func (m *Model) handleListKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.syncTableScroll()
		}
	case "down", "j":
		if m.selected < len(m.tasks)-1 {
			m.selected++
			m.syncTableScroll()
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.tableViewportRows())
		m.selected -= step
		if m.selected < 0 {
			m.selected = 0
		}
		m.syncTableScroll()
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.tableViewportRows())
		m.selected += step
		if m.selected > len(m.tasks)-1 {
			m.selected = len(m.tasks) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		m.syncTableScroll()
	case "home", "g":
		m.selected = 0
		m.syncTableScroll()
	case "end", "G":
		if len(m.tasks) > 0 {
			m.selected = len(m.tasks) - 1
			m.syncTableScroll()
		}
	case "enter":
		if task, ok := m.selectedTask(); ok {
			m.openTaskView(task)
		}
	case "m":
		if _, ok := m.selectedTask(); ok {
			m.beginInput(modeMove, "Target bucket key", "")
		}
	}
}

// handleGraphKey handles input on the dependency graph view.
// j/k/pgup/pgdn/g/G navigate between nodes; enter opens the selected task.
func (m *Model) handleGraphKey(msg tea.KeyMsg) {
	lines := buildDAGLines(m.dependencies, m.tasks)
	sel := dagSelectableIndices(lines)
	maxCursor := len(sel) - 1
	if maxCursor < 0 {
		maxCursor = 0
	}

	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
	case "up", "k":
		if m.graphCursor > 0 {
			m.graphCursor--
		}
	case "down", "j":
		if m.graphCursor < maxCursor {
			m.graphCursor++
		}
	case "pgup", "ctrl+u":
		m.graphCursor -= taskViewPageStep(m.graphViewportRows())
		if m.graphCursor < 0 {
			m.graphCursor = 0
		}
	case "pgdown", "ctrl+d":
		m.graphCursor += taskViewPageStep(m.graphViewportRows())
		if m.graphCursor > maxCursor {
			m.graphCursor = maxCursor
		}
	case "home", "g":
		m.graphCursor = 0
	case "end", "G":
		m.graphCursor = maxCursor
	case "enter":
		if m.graphCursor >= 0 && m.graphCursor < len(sel) {
			taskID := lines[sel[m.graphCursor]].taskID
			if task, ok := m.taskByID(taskID); ok {
				m.openTaskView(task)
			}
		}
	}
	m.syncGraphScroll(sel, len(lines))
}

// syncGraphScroll keeps m.graphScroll aligned so the cursor node stays in the viewport.
func (m *Model) syncGraphScroll(sel []int, totalLines int) {
	viewport := m.graphViewportRows()
	if viewport <= 0 || len(sel) == 0 {
		return
	}
	cursorLine := sel[clampInt(m.graphCursor, 0, len(sel)-1)]
	if cursorLine < m.graphScroll {
		m.graphScroll = cursorLine
	}
	if cursorLine >= m.graphScroll+viewport {
		m.graphScroll = cursorLine - viewport + 1
	}
	if m.graphScroll < 0 {
		m.graphScroll = 0
	}
}

// graphViewportRows returns how many DAG lines fit in the graph panel viewport.
// Returns 0 when the terminal is too small to scroll.
func (m Model) graphViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 5 screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 2 panel header rows (kicker + blank) = 12.
	chrome := 12
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

// syncTableScroll keeps m.tableScroll aligned so the selected task row stays
// in view. Each row is exactly 1 line — no height heuristic, same pattern as
// syncLogsScroll.
func (m *Model) syncTableScroll() {
	viewport := m.tableViewportRows()
	if viewport <= 0 {
		return
	}
	if m.selected < m.tableScroll {
		m.tableScroll = m.selected
	}
	if m.selected >= m.tableScroll+viewport {
		m.tableScroll = m.selected - viewport + 1
	}
	if m.tableScroll < 0 {
		m.tableScroll = 0
	}
}

// tableViewportRows returns how many task rows fit in the table panel after
// the screen chrome and the panel's internal header rows. Returns 0 when the
// height is unknown or too small.
func (m Model) tableViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 5 screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 3 panel header rows (kicker/info/separator) = 13.
	chrome := 13
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m *Model) handleLogsKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
	case "up", "k":
		if m.logsSelected > 0 {
			m.logsSelected--
			m.syncLogsScroll()
		}
	case "down", "j":
		if m.logsSelected < len(m.logs)-1 {
			m.logsSelected++
			m.syncLogsScroll()
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.logsViewportRows())
		m.logsSelected -= step
		if m.logsSelected < 0 {
			m.logsSelected = 0
		}
		m.syncLogsScroll()
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.logsViewportRows())
		m.logsSelected += step
		if m.logsSelected > len(m.logs)-1 {
			m.logsSelected = len(m.logs) - 1
		}
		if m.logsSelected < 0 {
			m.logsSelected = 0
		}
		m.syncLogsScroll()
	case "home", "g":
		m.logsSelected = 0
		m.syncLogsScroll()
	case "end", "G":
		if len(m.logs) > 0 {
			m.logsSelected = len(m.logs) - 1
			m.syncLogsScroll()
		}
	}
}

// syncLogsScroll keeps m.logsScroll aligned so the selected log row stays
// inside the viewport. Each log row is exactly 1 line (no wrapping) so this is
// a simple cursor-following scroll — no height heuristic needed.
func (m *Model) syncLogsScroll() {
	viewport := m.logsViewportRows()
	if viewport <= 0 {
		return
	}
	if m.logsSelected < m.logsScroll {
		m.logsScroll = m.logsSelected
	}
	if m.logsSelected >= m.logsScroll+viewport {
		m.logsScroll = m.logsSelected - viewport + 1
	}
	if m.logsScroll < 0 {
		m.logsScroll = 0
	}
}

// logsViewportRows returns how many data rows fit in the activity log panel
// after accounting for the screen chrome, panel borders, and the panel's
// internal header (kicker + column header + separator) and footer (blank +
// hint) rows. Returns 0 when the height is unknown or too small to scroll.
func (m Model) logsViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 5 screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 3 panel header rows + 2 panel footer rows = 15.
	chrome := 15
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m *Model) updateTaskScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.blockerPickerOpen {
		return m.updateBlockerPicker(msg)
	}

	if m.taskScreen == taskScreenView {
		switch msg.String() {
		case "ctrl+c", "q":
			return *m, tea.Quit
		case "esc":
			m.closeTaskScreen("")
		case "e":
			if task, ok := m.activeTask(); ok {
				m.openTaskEdit(task)
			}
		case "b":
			if _, ok := m.activeTask(); ok {
				m.openBlockerPicker()
			}
		case "c":
			if _, ok := m.activeTask(); ok {
				m.beginInput(modeComment, "Comment body", "")
			}
		case "m":
			if _, ok := m.activeTask(); ok {
				m.beginInput(modeMove, "Target bucket key", "")
			}
		case "r":
			if err := m.refresh(); err != nil {
				m.status = err.Error()
			} else {
				m.status = "Refreshed"
			}
		case "j", "down":
			m.taskViewScroll++
		case "k", "up":
			if m.taskViewScroll > 0 {
				m.taskViewScroll--
			}
		case "pgdown", "ctrl+d":
			m.taskViewScroll += taskViewPageStep(m.taskViewportHeight())
		case "pgup", "ctrl+u":
			m.taskViewScroll -= taskViewPageStep(m.taskViewportHeight())
			if m.taskViewScroll < 0 {
				m.taskViewScroll = 0
			}
		case "home", "g":
			m.taskViewScroll = 0
		case "end", "G":
			m.taskViewScroll = 1 << 20
		}
		return *m, nil
	}

	switch {
	case msg.String() == "ctrl+c":
		return *m, tea.Quit
	case msg.String() == "esc":
		if m.taskScreen == taskScreenCreate {
			m.closeTaskScreen("Cancelled")
		} else if task, ok := m.activeTask(); ok {
			m.openTaskView(task)
		} else {
			m.closeTaskScreen("Cancelled")
		}
	case msg.String() == "ctrl+s":
		m.saveTaskForm()
	case msg.String() == "tab" || msg.String() == "shift+tab":
		m.toggleTaskField()
	case msg.String() == "ctrl+b":
		if m.taskScreen == taskScreenEdit {
			m.openBlockerPicker()
		}
	case isTaskFormNewline(msg):
		m.insertTaskFormNewline()
	case msg.String() == "backspace" || msg.String() == "ctrl+h":
		m.deleteTaskFormRune()
	case m.taskField == taskFieldPriority && (msg.String() == "left" || msg.String() == "h"):
		m.cycleTaskPriority(-1)
	case m.taskField == taskFieldPriority && (msg.String() == "right" || msg.String() == "l"):
		m.cycleTaskPriority(1)
	default:
		if len(msg.Runes) > 0 {
			m.appendTaskFormText(string(msg.Runes))
		} else if msg.String() == " " {
			m.appendTaskFormText(" ")
		}
	}
	return *m, nil
}

func (m *Model) updateBlockerPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	candidates := m.blockerPickerCandidates()
	rowCount := len(candidates)
	switch msg.String() {
	case "ctrl+c", "q":
		return *m, tea.Quit
	case "esc":
		m.closeBlockerPicker("Cancelled")
	case "up", "k":
		if m.blockerPickerCursor > 0 {
			m.blockerPickerCursor--
			m.syncBlockerPickerScroll(rowCount)
		}
	case "down", "j":
		if m.blockerPickerCursor < rowCount-1 {
			m.blockerPickerCursor++
			m.syncBlockerPickerScroll(rowCount)
		}
	case "pgup", "ctrl+u":
		step := taskViewPageStep(m.blockerPickerViewportRows())
		m.blockerPickerCursor -= step
		if m.blockerPickerCursor < 0 {
			m.blockerPickerCursor = 0
		}
		m.syncBlockerPickerScroll(rowCount)
	case "pgdown", "ctrl+d":
		step := taskViewPageStep(m.blockerPickerViewportRows())
		m.blockerPickerCursor += step
		if m.blockerPickerCursor > rowCount-1 {
			m.blockerPickerCursor = rowCount - 1
		}
		if m.blockerPickerCursor < 0 {
			m.blockerPickerCursor = 0
		}
		m.syncBlockerPickerScroll(rowCount)
	case "home", "g":
		m.blockerPickerCursor = 0
		m.syncBlockerPickerScroll(rowCount)
	case "end", "G":
		if rowCount > 0 {
			m.blockerPickerCursor = rowCount - 1
			m.syncBlockerPickerScroll(rowCount)
		}
	case " ", "space":
		if m.blockerPickerCursor >= 0 && m.blockerPickerCursor < rowCount {
			taskID := candidates[m.blockerPickerCursor].ID
			if m.blockerPickerChecks == nil {
				m.blockerPickerChecks = map[int64]bool{}
			}
			m.blockerPickerChecks[taskID] = !m.blockerPickerChecks[taskID]
		}
	case "ctrl+s":
		m.saveBlockerPicker()
	}
	return *m, nil
}

// syncBlockerPickerScroll keeps m.blockerPickerScroll aligned so the cursor
// row stays visible (cursor-following, same pattern as syncPickerScroll).
func (m *Model) syncBlockerPickerScroll(rowCount int) {
	viewport := m.blockerPickerViewportRows()
	if viewport <= 0 {
		return
	}
	if m.blockerPickerCursor < m.blockerPickerScroll {
		m.blockerPickerScroll = m.blockerPickerCursor
	}
	if m.blockerPickerCursor >= m.blockerPickerScroll+viewport {
		m.blockerPickerScroll = m.blockerPickerCursor - viewport + 1
	}
	if m.blockerPickerScroll < 0 {
		m.blockerPickerScroll = 0
	}
	if rowCount > 0 && m.blockerPickerScroll > rowCount-1 {
		m.blockerPickerScroll = rowCount - 1
	}
	if m.blockerPickerScroll < 0 {
		m.blockerPickerScroll = 0
	}
}

// blockerPickerViewportRows returns how many candidate rows fit in the picker
// panel after the screen chrome and the panel's internal header rows.
func (m Model) blockerPickerViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 2 task-screen header + 1 leading blank + 2 footer + 2 panel borders
	// + 6 panel header rows (kicker/hint/blank/metaRow/blank/separator) = 13.
	chrome := 13
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

func (m Model) blockerPickerCandidates() []domain.Task {
	candidates := make([]domain.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task.ID != m.blockerPickerTaskID {
			candidates = append(candidates, task)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	return candidates
}

func isTaskFormNewline(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlJ {
		return true
	}
	switch msg.String() {
	case "enter", "alt+enter", "shift+enter", "ctrl+j", "alt+ctrl+j":
		return true
	default:
		return false
	}
}

func (m *Model) openTaskCreate() {
	m.taskScreen = taskScreenCreate
	m.taskID = 0
	m.taskTitle = ""
	m.taskDescription = ""
	m.taskPriority = "normal"
	m.taskField = taskFieldTitle
	m.status = "New task"
	m.moveMode = false
}

func (m *Model) openTaskView(task domain.Task) {
	m.taskScreen = taskScreenView
	m.taskID = task.ID
	m.taskTitle = ""
	m.taskDescription = ""
	m.taskField = taskFieldTitle
	m.status = ""
	m.moveMode = false
	m.taskViewScroll = 0
}

func (m *Model) openTaskEdit(task domain.Task) {
	m.taskScreen = taskScreenEdit
	m.taskID = task.ID
	m.taskTitle = task.Title
	m.taskDescription = task.Description
	m.taskPriority = string(task.Priority)
	m.taskField = taskFieldTitle
	m.status = "Editing task"
	m.moveMode = false
}

func (m *Model) closeTaskScreen(status string) {
	m.blockerPickerOpen = false
	m.blockerPickerTaskID = 0
	m.blockerPickerCursor = 0
	m.blockerPickerChecks = nil
	m.taskScreen = taskScreenClosed
	m.taskID = 0
	m.taskTitle = ""
	m.taskDescription = ""
	m.taskPriority = ""
	m.taskField = taskFieldTitle
	m.status = status
	m.moveMode = false
	m.taskViewScroll = 0
}

func (m *Model) toggleTaskField() {
	switch m.taskField {
	case taskFieldTitle:
		m.taskField = taskFieldDescription
	case taskFieldDescription:
		m.taskField = taskFieldPriority
	default:
		m.taskField = taskFieldTitle
	}
}

func (m *Model) appendTaskFormText(text string) {
	switch m.taskField {
	case taskFieldTitle:
		m.taskTitle += text
	case taskFieldDescription:
		m.taskDescription += text
	}
}

func (m *Model) insertTaskFormNewline() {
	switch m.taskField {
	case taskFieldTitle:
		m.taskField = taskFieldDescription
	case taskFieldDescription:
		m.taskDescription += "\n"
	}
}

func (m *Model) deleteTaskFormRune() {
	switch m.taskField {
	case taskFieldTitle:
		m.taskTitle = trimLastRune(m.taskTitle)
	case taskFieldDescription:
		m.taskDescription = trimLastRune(m.taskDescription)
	}
}

func (m *Model) cycleTaskPriority(delta int) {
	levels := []string{"low", "normal", "high"}
	idx := 1 // default to normal
	for i, v := range levels {
		if v == m.taskPriority {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(levels) {
		idx = len(levels) - 1
	}
	m.taskPriority = levels[idx]
}

func (m *Model) openBlockerPicker() {
	if m.taskID <= 0 {
		return
	}
	m.blockerPickerOpen = true
	m.blockerPickerTaskID = m.taskID
	m.blockerPickerCursor = 0
	m.blockerPickerChecks = map[int64]bool{}
	for _, dep := range m.dependencies {
		if dep.TaskID == m.taskID {
			m.blockerPickerChecks[dep.DependsOnTaskID] = true
		}
	}
	m.blockerPickerScroll = 0
}

func (m *Model) closeBlockerPicker(status string) {
	m.blockerPickerOpen = false
	m.blockerPickerTaskID = 0
	m.blockerPickerCursor = 0
	m.blockerPickerChecks = nil
	m.status = status
	m.blockerPickerScroll = 0
}

func (m *Model) saveBlockerPicker() {
	if m.blockerPickerTaskID <= 0 {
		m.closeBlockerPicker("")
		return
	}
	service := app.NewDependencyService(m.repos.Dependencies)
	// Determine additions and removals
	existing := map[int64]bool{}
	for _, dep := range m.dependencies {
		if dep.TaskID == m.blockerPickerTaskID {
			existing[dep.DependsOnTaskID] = true
		}
	}
	var added []int64
	for taskID, checked := range m.blockerPickerChecks {
		if checked && !existing[taskID] {
			added = append(added, taskID)
		}
	}
	var removed []int64
	for taskID := range existing {
		if !m.blockerPickerChecks[taskID] {
			removed = append(removed, taskID)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	sort.Slice(removed, func(i, j int) bool { return removed[i] < removed[j] })
	for _, depID := range removed {
		if err := service.Remove(m.ctx, m.project, m.blockerPickerTaskID, depID); err != nil {
			m.status = err.Error()
			return
		}
	}
	for _, depID := range added {
		if _, err := service.Add(m.ctx, m.project, m.blockerPickerTaskID, depID); err != nil {
			m.status = err.Error()
			return
		}
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.closeBlockerPicker("Blockers saved")
}

func (m *Model) saveTaskForm() {
	title := strings.TrimSpace(m.taskTitle)
	if title == "" {
		m.status = "Task title is required"
		return
	}
	description := strings.TrimSpace(m.taskDescription)

	var task domain.Task
	var err error
	switch m.taskScreen {
	case taskScreenCreate:
		task, err = app.NewTaskService(m.repos.Tasks).Add(m.ctx, m.project, title, description, m.taskPriority, "backlog")
	case taskScreenEdit:
		current, ok := m.activeTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		update := domain.TaskUpdate{Title: &title, Description: &description}
		if m.taskPriority != "" {
			p := domain.Priority(m.taskPriority)
			update.Priority = &p
		}
		task, err = app.NewTaskService(m.repos.Tasks).Edit(m.ctx, m.project, current.ID, update)
	default:
		return
	}
	if err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	if m.selectTaskByID(task.ID) {
		m.openTaskView(task)
	}
	m.status = "Saved"
}

func (m *Model) beginInput(mode inputMode, status, input string) {
	m.mode = mode
	m.input = input
	m.status = status
	m.moveMode = false
}

func (m Model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modeNormal
		m.input = ""
		m.status = "Cancelled"
	case "alt+enter", "shift+enter", "ctrl+j", "alt+ctrl+j":
		if m.mode == modeComment {
			m.input += "\n"
		}
	case "enter":
		m.submitInput()
	case "backspace", "ctrl+h":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		m.input += msg.String()
	}
	return m, nil
}

func (m *Model) submitInput() {
	input := strings.TrimSpace(m.input)
	if input == "" {
		m.status = "Input is required"
		return
	}

	var savedTask domain.Task
	selectSavedTask := false
	var err error
	switch m.mode {
	case modeComment:
		task, ok := m.selectedTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		_, err = app.NewCommentService(m.repos.Comments).Add(m.ctx, m.project, task.ID, input, "human", nil)
	case modeMove:
		task, ok := m.selectedTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		savedTask, err = app.NewTaskService(m.repos.Tasks).Move(m.ctx, m.project, task.ID, input)
		selectSavedTask = true
	}

	if err != nil {
		m.status = err.Error()
		m.mode = modeNormal
		m.input = ""
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		if selectSavedTask && m.selectTaskByID(savedTask.ID) {
			m.taskID = savedTask.ID
		}
		m.status = "Saved"
	}
	m.mode = modeNormal
	m.input = ""
}

func (m *Model) moveSelectedToColumn(targetColIdx int) {
	if targetColIdx < 0 || targetColIdx >= len(m.workflow.Buckets) {
		m.status = "No target column"
		return
	}
	task, ok := m.selectedTask()
	if !ok {
		m.status = "No selected task"
		return
	}
	target := m.workflow.Buckets[targetColIdx]
	if _, err := app.NewTaskService(m.repos.Tasks).Move(m.ctx, m.project, task.ID, target.Key); err != nil {
		m.status = err.Error()
		m.moveMode = false
		return
	}
	m.colIdx = targetColIdx
	m.moveMode = false
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	} else {
		m.status = fmt.Sprintf("Moved #%d to %s", task.ID, target.Key)
	}
	m.selectTaskByID(task.ID)
	m.syncFocusedColumnScroll()
}

func (m *Model) refresh() error {
	tasks, err := m.repos.Tasks.ListTasks(m.ctx, m.project.ID, domain.TaskFilter{})
	if err != nil {
		return err
	}
	workflow, err := m.repos.Config.ActiveWorkflow(m.ctx)
	if err != nil {
		return err
	}
	dependencies, err := m.repos.Dependencies.ListTaskDependencies(m.ctx, m.project.ID, 0)
	if err != nil {
		return err
	}
	comments, err := m.repos.Comments.ListComments(m.ctx, m.project.ID, 0)
	if err != nil {
		return err
	}
	laws, err := m.repos.Config.ListActiveLaws(m.ctx)
	if err != nil {
		return err
	}
	skills, err := m.repos.Config.ListActiveSkills(m.ctx)
	if err != nil {
		return err
	}
	personas, err := m.repos.Config.ListActivePersonas(m.ctx)
	if err != nil {
		return err
	}
	// The store returns identity-level fields only (id, key, name, severity).
	// Merge frontmatter + body + source_path from the on-disk bundle so the
	// detail views and the $EDITOR shell-out have the file path they need.
	var templates []config.TaskTemplate
	var activeTemplate string
	if m.repos.Editor != nil {
		bundle, err := m.repos.Editor.Load()
		if err != nil {
			return err
		}
		skills = enrichSkillsFromBundle(skills, bundle)
		laws = enrichLawsFromBundle(laws, bundle)
		personas = enrichPersonasFromBundle(personas, bundle)
		// Templates live only in the bundle (no SQLite materialization), so
		// the TUI mirrors them straight from disk on every refresh.
		templates = append([]config.TaskTemplate(nil), bundle.Templates...)
		activeTemplate = bundle.Config.Templates.Task
	}
	entries, err := m.repos.Entries.ListContextEntries(m.ctx, m.project.ID)
	if err != nil {
		return err
	}
	settings, err := m.repos.Config.ContextSettings(m.ctx)
	if err != nil {
		return err
	}
	var allTags []domain.Tag
	taskTagsMap := map[int64][]domain.Tag{}
	if m.repos.Tags != nil {
		allTags, err = m.repos.Tags.ListAllTags(m.ctx)
		if err != nil {
			return err
		}
		taskTagsMap, err = m.repos.Tags.ListTaskTagsByProject(m.ctx, m.project.ID)
		if err != nil {
			return err
		}
	}

	m.tasks = tasks
	m.workflow = workflow
	m.dependencies = dependencies
	m.comments = comments
	m.laws = laws
	m.skills = skills
	m.personas = personas
	m.templates = templates
	m.activeTaskTemplateSlug = activeTemplate
	m.entries = entries
	m.tags = allTags
	m.taskTagsMap = taskTagsMap
	m.metrics = m.computeMetrics(settings.MaxTokens)
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

func (m Model) renderHeader() string {
	var sb strings.Builder
	sb.WriteString("\n  ")
	sb.WriteString(m.styles.title.Render("omakiten"))
	sb.WriteString(m.styles.hint.Render(" › "))
	sb.WriteString(m.styles.nav.Render(m.project.Slug))
	sb.WriteString(m.styles.hint.Render(" · local checkpoint"))
	if m.helpOpen || m.taskScreen != taskScreenClosed || m.entityScreen != entityScreenClosed {
		return sb.String()
	}
	sb.WriteString("\n\n  ")

	const navGap = "   "
	items := make([]string, 0, len(viewNames))
	rules := make([]string, 0, len(viewNames))
	for i, name := range viewNames {
		label := fmt.Sprintf("%02d // %s", i+1, name)
		width := lipgloss.Width(label)
		if i == m.view {
			items = append(items, m.styles.activeNav.Render(label))
			rules = append(rules, m.styles.activeNav.Render(strings.Repeat("─", width)))
		} else {
			items = append(items, m.styles.nav.Render(label))
			rules = append(rules, strings.Repeat(" ", width))
		}
	}
	if lipgloss.Width(strings.Join(items, navGap)) > m.availableWidth() {
		active := fmt.Sprintf("%02d // %s", m.view+1, viewNames[m.view])
		sb.WriteString(m.styles.activeNav.Render(active))
		sb.WriteString(m.styles.hint.Render("  tab/1-5 switch views"))
		return sb.String()
	}
	sb.WriteString(strings.Join(items, navGap))
	sb.WriteString("\n  ")
	sb.WriteString(strings.Join(rules, navGap))
	return sb.String()
}

func (m Model) renderInput() string {
	return indentBlock(m.styles.input.Render(fmt.Sprintf("%s: %s", m.status, m.input)), 2)
}

func (m Model) renderCurrentView() string {
	if m.taskScreen != taskScreenClosed {
		return m.renderTaskScreen()
	}
	if m.entityScreen != entityScreenClosed {
		return m.renderEntityScreen()
	}
	switch m.view {
	case 0:
		return m.renderBoard()
	case 1:
		return m.renderTable()
	case 2:
		return m.renderGraph()
	case 3:
		return m.renderConfig()
	case 4:
		return m.renderLogs()
	default:
		return ""
	}
}

// boardLayout holds the per-render geometry for the kanban board so columns
// and cards can grow with the available terminal width instead of being pinned
// to the legacy fixed constants.
type boardLayout struct {
	columnInner      int // kanban column inner content width (passed to Width())
	cardWidth        int // card.Width() — content width of each card box
	cardContentWidth int // text area inside a card (cardWidth - 2 padding)
	cardHeight       int // rendered on-screen height of a single card (incl. borders)
	viewportRows     int // rows available inside a column for cards (after header+sep)
}

func (m Model) computeBoardLayout(n int) boardLayout {
	const (
		minColumnInner = 28
		maxColumnInner = 44
	)
	available := m.availableWidth()
	colOnScreen := minColumnInner + 2
	if n > 0 {
		colOnScreen = (available - (n - 1)) / n
	}
	columnInner := colOnScreen - 2
	if columnInner < minColumnInner {
		columnInner = minColumnInner
	}
	if columnInner > maxColumnInner {
		columnInner = maxColumnInner
	}
	// The card style has its own border (+2 cols) and Padding(0,1) which adds
	// 2 cols inside the Width() box, so:
	//   on-screen card width = card.Width() + 2 (border)
	//   card text width      = card.Width() - 2 (padding)
	// To make the card fit exactly inside the column's inner area we set
	// cardWidth = columnInner - 2.
	cardWidth := columnInner - 2
	cardContent := cardWidth - 2

	return boardLayout{
		columnInner:      columnInner,
		cardWidth:        cardWidth,
		cardContentWidth: cardContent,
		cardHeight:       4,
		viewportRows:     m.boardViewportRows(),
	}
}

// boardViewportRows is the number of terminal rows the kanban columns can use
// for cards (after the column header, separator, and the surrounding chrome).
// Returns 0 when the height is unknown — callers should treat 0 as "no scroll
// limit" and render every card.
func (m Model) boardViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 9 // header(2) + nav(2) + view-leading-blank(1) + footer(2) + col header+sep(2)
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 6 {
		return 0
	}
	return rows
}

func (m Model) renderBoard() string {
	if len(m.workflow.Buckets) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No workflow buckets. Add buckets in the active workflow config."), 2)
	}

	tasksByBucket := m.tasksByBucket()
	totalTasks := 0
	for _, bucket := range m.workflow.Buckets {
		totalTasks += len(tasksByBucket[bucket.Key])
	}

	n := len(m.workflow.Buckets)
	layout := m.computeBoardLayout(n)
	columnStyle := m.styles.kanbanColumn.Width(layout.columnInner)
	emptyStyle := m.styles.empty.Width(layout.columnInner)

	// Trigger narrow fallback when the side-by-side layout no longer fits.
	totalSideBySide := n*(layout.columnInner+2) + (n - 1)
	if totalSideBySide > m.availableWidth() {
		current := clampInt(m.colIdx, 0, n-1)
		bucket := m.workflow.Buckets[current]
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if len(bucketTasks) > 0 {
			selectedIdx = clampInt(m.cardIdx, 0, len(bucketTasks)-1)
		}
		board := columnStyle.Render(m.renderKanbanCell(bucket, bucketTasks, true, selectedIdx, layout, emptyStyle))
		hint := m.styles.hint.Render(fmt.Sprintf("Column %d/%d · left/right switches lane", current+1, n))
		var sb strings.Builder
		sb.WriteString("\n")
		sb.WriteString(indentBlock(board+"\n"+hint, 2))
		if totalTasks == 0 {
			sb.WriteString("\n\n")
			sb.WriteString(indentBlock(m.renderEmptyBoardHint(), 2))
		}
		return sb.String()
	}

	cells := make([]string, 0, n)
	for i, bucket := range m.workflow.Buckets {
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if i == m.colIdx {
			selectedIdx = m.cardIdx
		}
		cellContent := m.renderKanbanCell(bucket, bucketTasks, i == m.colIdx, selectedIdx, layout, emptyStyle)
		cells = append(cells, columnStyle.Render(cellContent))
	}

	var parts []string
	for i, cell := range cells {
		parts = append(parts, cell)
		if i < len(cells)-1 {
			parts = append(parts, " ")
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(board, 2))
	if totalTasks == 0 {
		sb.WriteString("\n\n")
		sb.WriteString(indentBlock(m.renderEmptyBoardHint(), 2))
	}
	return sb.String()
}

func (m Model) renderKanbanCell(bucket domain.Bucket, tasks []domain.Task, focused bool, selectedIdx int, layout boardLayout, emptyStyle lipgloss.Style) string {
	headerStyle := m.styles.hintAccent
	if !focused {
		headerStyle = m.styles.muted
	}
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(bucket.Name), len(tasks))
	lines := []string{
		headerStyle.Render(headerText),
		m.styles.separator.Render(strings.Repeat("─", layout.columnInner)),
	}

	if len(tasks) == 0 {
		lines = append(lines, emptyStyle.Render("empty"))
		return strings.Join(lines, "\n")
	}

	// Render every card first so we know the real rendered height of each one.
	rendered := make([]string, len(tasks))
	heights := make([]int, len(tasks))
	for i, task := range tasks {
		rendered[i] = m.renderCard(task, focused && i == selectedIdx, layout)
		heights[i] = strings.Count(rendered[i], "\n") + 1
	}

	viewport := layout.viewportRows
	if viewport <= 0 {
		// Height unknown — render everything; the terminal will scroll natively.
		lines = append(lines, rendered...)
		return strings.Join(lines, "\n")
	}

	offset := m.boardScroll[bucket.Key]
	if offset < 0 {
		offset = 0
	}
	if offset > len(rendered)-1 {
		offset = len(rendered) - 1
	}

	used := 0
	end := offset
	for end < len(rendered) {
		// Reserve 1 row for the "▼ N below" hint when there is more content.
		reserve := 0
		if end < len(rendered)-1 {
			reserve = 1
		}
		if used+heights[end]+reserve > viewport {
			break
		}
		used += heights[end]
		end++
	}
	if end == offset && offset < len(rendered) {
		// Never produce an empty viewport: render at least one card.
		end = offset + 1
	}

	above := offset
	below := len(rendered) - end
	if above > 0 {
		lines = append(lines, m.styles.hint.Render(fmt.Sprintf("▲ %d above", above)))
	}
	lines = append(lines, rendered[offset:end]...)
	if below > 0 {
		lines = append(lines, m.styles.hint.Render(fmt.Sprintf("▼ %d below", below)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderCard(task domain.Task, selected bool, layout boardLayout) string {
	prefix := fmt.Sprintf("#%d ", task.ID)
	prefixWidth := lipgloss.Width(prefix)

	firstWidth := layout.cardContentWidth - prefixWidth
	restWidth := layout.cardContentWidth - prefixWidth
	if firstWidth < 1 {
		firstWidth = 1
	}
	if restWidth < 1 {
		restWidth = 1
	}

	wrapped := wrapWords(task.Title, firstWidth, restWidth)
	lines := make([]string, 0, len(wrapped)+1)
	for i, part := range wrapped {
		if i == 0 {
			lines = append(lines, prefix+part)
		} else {
			lines = append(lines, strings.Repeat(" ", prefixWidth)+part)
		}
	}

	if badgeLine := m.renderTaskBadges(task, layout.cardContentWidth); badgeLine != "" {
		lines = append(lines, badgeLine)
	}

	style := m.styles.card.Width(layout.cardWidth)
	if selected {
		style = m.styles.cardSelected.Width(layout.cardWidth)
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderTaskBadges builds a line of colored badges for a task: priority,
// blocker count, and comment count. Each badge is rendered as a filled pill
// using Lipgloss background colors. wrapBadges breaks badges onto a new line
// whenever the next would overflow maxWidth so every badge stays visible.
func (m Model) renderTaskBadges(task domain.Task, maxWidth int) string {
	var badges []string

	switch task.Priority {
	case domain.PriorityHigh:
		badges = append(badges, m.styles.badgeHigh.Render("HIGH"))
	case domain.PriorityLow:
		badges = append(badges, m.styles.badgeLow.Render("LOW"))
	default:
		badges = append(badges, m.styles.badgeNormal.Render("NORMAL"))
	}
	if deps := m.dependencyCount(task.ID); deps > 0 {
		badges = append(badges, m.styles.badgeBlocker.Render(fmt.Sprintf("%d %s", deps, plural(deps, "BLOCKER", "BLOCKERS"))))
	}
	if cmts := m.commentCount(task.ID); cmts > 0 {
		badges = append(badges, m.styles.badgeComment.Render(fmt.Sprintf("%d %s", cmts, plural(cmts, "COMMENT", "COMMENTS"))))
	}

	return wrapBadges(badges, maxWidth)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func (m Model) renderHelp() string {
	type binding struct{ key, desc string }
	type group struct {
		title    string
		bindings []binding
	}
	groups := []group{
		{"Global", []binding{
			{"?", "close this overlay"},
			{"a", "toggle all bindings"},
			{"q · ctrl+c", "quit"},
			{"tab · shift+tab", "cycle views"},
			{"1 · 2 · 3 · 4 · 5", "jump to view"},
			{"r", "refresh"},
		}},
		{"Board", []binding{
			{"← ↑ ↓ → · h j k l", "navigate lanes and tasks (auto-scrolls column)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll focused column by page"},
			{"g · G", "first / last card in column"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"c", "add comment"},
			{"m", "move task between lanes"},
		}},
		{"Task list", []binding{
			{"↑ ↓ · j k", "select task (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last task"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"m", "move by bucket key"},
		}},
		{"Graph", []binding{
			{"← →", "switch view"},
			{"↑ ↓ · j k", "move cursor"},
			{"enter", "open task"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
		}},
		{"Task view", []binding{
			{"↑ ↓ · j k", "scroll description"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
			{"e", "edit"},
			{"b", "edit blockers"},
			{"c", "add comment"},
			{"m", "move"},
			{"esc", "back to board"},
		}},
		{"Comment input", []binding{
			{"enter", "save comment"},
			{"alt+enter · shift+enter", "insert newline"},
			{"esc", "cancel"},
		}},
		{"Task form", []binding{
			{"tab", "switch field"},
			{"← → · h l", "change priority"},
			{"ctrl+b", "edit blockers when editing an existing task"},
			{"enter · alt+enter · shift+enter", "newline in description"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Blocker picker", []binding{
			{"↑ ↓ · j k", "move (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last candidate"},
			{"space", "toggle blocker"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Config", []binding{
			{"← →", "switch entity kind"},
			{"↑ ↓", "select entity"},
			{"enter", "open detail"},
			{"n", "new entity"},
			{"e", "edit in $EDITOR"},
			{"d · d", "arm delete, then confirm"},
			{"p", "skill picker (persona)"},
		}},
		{"Entity view", []binding{
			{"↑ ↓ · j k", "scroll body"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "jump to top / bottom"},
			{"e", "edit (opens $EDITOR)"},
			{"d · d", "arm delete, then confirm"},
			{"p", "skill picker (persona)"},
			{"esc", "back, or cancel pending delete"},
		}},
		{"Skill picker", []binding{
			{"↑ ↓ · j k", "move (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last row"},
			{"space", "toggle"},
			{"enter on '+ create new'", "scaffold new skill"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Logs", []binding{
			{"← →", "switch view"},
			{"↑ ↓ · j k", "select row (auto-scrolls)"},
			{"pgup · pgdn · ctrl+u · ctrl+d", "scroll by half page"},
			{"g · G", "first / last row"},
			{"r", "refresh"},
		}},
	}

	if !m.helpAll {
		wanted := map[string]bool{"Global": true}
		for _, title := range m.currentHelpTitles() {
			wanted[title] = true
		}
		filtered := make([]group, 0, len(wanted))
		for _, g := range groups {
			if wanted[g.title] {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}

	const keyW = 34
	var lines []string
	title := "Keybindings · current context"
	if m.helpAll {
		title = "Keybindings · all contexts"
	}
	lines = append(lines, m.styles.kicker(title), m.styles.hint.Render("press a to toggle scope"), "")
	for _, g := range groups {
		lines = append(lines, m.styles.kicker(g.title))
		lines = append(lines, m.styles.separator.Render(strings.Repeat("─", keyW+24)))
		for _, b := range g.bindings {
			pad := keyW - lipgloss.Width(b.key)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, m.styles.hintAccent.Render(b.key)+strings.Repeat(" ", pad)+b.desc)
		}
		lines = append(lines, "")
	}

	viewport := m.helpViewportRows()
	if viewport > 0 && len(lines) > viewport {
		visibleHeight := viewport - 1
		maxOffset := len(lines) - visibleHeight
		offset := m.helpScroll
		if offset < 0 {
			offset = 0
		}
		if offset > maxOffset {
			offset = maxOffset
		}
		visible := lines[offset : offset+visibleHeight]
		above := offset
		below := len(lines) - (offset + visibleHeight)
		if below < 0 {
			below = 0
		}
		hint := m.styles.hint.Render(fmt.Sprintf("▲ %d above · ▼ %d below  · j/k pgup/pgdn g/G", above, below))
		return "\n" + indentBlock(strings.Join(visible, "\n")+"\n"+hint, 2)
	}
	return "\n" + indentBlock(strings.Join(lines, "\n"), 2)
}

// helpViewportRows returns the line budget for the help screen content. Help
// view chrome is small: header (2) + leading blank from renderHelp (1) + help
// footer (1).
func (m Model) helpViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 4
	rows := m.height - chrome
	if rows < 8 {
		return 0
	}
	return rows
}

func (m Model) renderHelpFooter() string {
	return indentBlock(m.styles.footer.Render("j/k pgup/pgdn g/G scroll · a all/current · ?/esc/q close help"), 2)
}

func (m Model) currentHelpTitles() []string {
	switch {
	case m.isEmbeddedCommentInput():
		return []string{"Comment input"}
	case m.blockerPickerOpen:
		return []string{"Blocker picker"}
	case m.taskScreen == taskScreenCreate || m.taskScreen == taskScreenEdit:
		return []string{"Task form"}
	case m.taskScreen == taskScreenView:
		return []string{"Task view"}
	case m.entityScreen == entityScreenSkillPicker:
		return []string{"Skill picker"}
	case m.entityScreen == entityScreenView:
		return []string{"Entity view"}
	case m.view == 0:
		return []string{"Board"}
	case m.view == 1:
		return []string{"Task list"}
	case m.view == 2:
		return []string{"Graph"}
	case m.view == 3:
		return []string{"Config"}
	case m.view == 4:
		return []string{"Logs"}
	default:
		return []string{"Board"}
	}
}

func (m Model) renderEmptyBoardHint() string {
	lines := []string{
		m.styles.hintAccent.Render("No tasks yet."),
		"",
		m.styles.hint.Render("Press ") + m.styles.hintAccent.Render("n") + m.styles.hint.Render(" to create the first task, or use the CLI:"),
		m.styles.hint.Render("  okt add -t \"Implement the next slice\""),
		"",
		m.styles.hintAccent.Render("m") + m.styles.hint.Render(" move  ") + m.styles.hintAccent.Render("enter") + m.styles.hint.Render(" open  ") + m.styles.hintAccent.Render("c") + m.styles.hint.Render(" comment"),
	}
	return m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join(lines, "\n"))
}

func (m Model) renderTaskScreen() string {
	if m.blockerPickerOpen {
		return m.renderBlockerPicker()
	}
	switch m.taskScreen {
	case taskScreenCreate:
		return m.renderTaskForm("Create task")
	case taskScreenEdit:
		return m.renderTaskForm("Edit task")
	case taskScreenView:
		return m.renderTaskView()
	default:
		return ""
	}
}

func (m Model) renderTaskView() string {
	task, ok := m.activeTask()
	if !ok {
		return "\n" + indentBlock(m.styles.panel.Render("Task not found. Refresh with r or return to the board."), 2)
	}
	blockers := m.blockersForTask(task.ID)

	const taskDetailLabelWidth = 13

	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}

	taskTags := m.tagsForTask(task.ID)
	tagLine := ""
	if len(taskTags) > 0 {
		tagNames := make([]string, len(taskTags))
		for i, t := range taskTags {
			tagNames[i] = t.Label
		}
		tagLine = strings.Join(tagNames, " · ")
	}

	rows := [][]string{
		{m.styles.kicker(fmt.Sprintf("Task · #%d", task.ID))},
		{labelCell("Title"), task.Title},
		{labelCell("Bucket"), task.BucketKey},
		{labelCell("Priority"), string(task.Priority)},
		{labelCell("Comments"), fmt.Sprintf("%d", m.commentCount(task.ID))},
		{labelCell("Tags"), tagLine},
		{m.styles.kickerCount("Blockers", len(blockers))},
	}
	if len(blockers) == 0 {
		rows = append(rows, []string{m.styles.hint.Render("No blockers. Press b to add one.")})
	} else {
		for _, blocker := range blockers {
			rows = append(rows, []string{m.renderTaskReference(blocker)})
		}
	}
	rows = append(rows, []string{m.styles.kicker("Description")})
	if strings.TrimSpace(task.Description) == "" {
		rows = append(rows, []string{m.styles.hint.Render("No description")})
	} else {
		rows = append(rows, []string{strings.TrimRight(task.Description, "\n")})
	}

	commentsCellText := m.renderTaskCommentsCell(task.ID)

	available := m.availableWidth()
	// Side-by-side layout needs: details(label+1+value+2 borders) + 2 spacer + comments(inner+2 borders).
	// Below this threshold, stack vertically and let each block use the full width.
	const minWideValueWidth = 24
	wideThreshold := taskDetailLabelWidth + 1 + minWideValueWidth + 2 + 2 + taskCommentsPanelWidth + 2

	var rendered string
	if available < wideThreshold {
		valueWidth := available - taskDetailLabelWidth - 1 - 2
		if valueWidth < 16 {
			valueWidth = 16
		}
		details := renderGridTable(rows, []int{taskDetailLabelWidth, valueWidth}, m.styles.border)
		commentsWidth := available - 2
		if commentsWidth < 36 {
			commentsWidth = 36
		}
		commentsBox := renderFixedBox(wrapLinesToWidth(strings.Split(commentsCellText, "\n"), commentsWidth), commentsWidth, m.styles.border)
		rendered = details + "\n\n" + commentsBox
	} else {
		valueWidth := available - (taskCommentsPanelWidth + 2) - 2 - (taskDetailLabelWidth + 1) - 2
		if valueWidth < minWideValueWidth {
			valueWidth = minWideValueWidth
		}
		if valueWidth > 120 {
			valueWidth = 120
		}
		details := renderGridTable(rows, []int{taskDetailLabelWidth, valueWidth}, m.styles.border)
		commentsBox := renderFixedBox(wrapLinesToWidth(strings.Split(commentsCellText, "\n"), taskCommentsPanelWidth), taskCommentsPanelWidth, m.styles.border)
		rendered = lipgloss.JoinHorizontal(lipgloss.Top, details, "  ", commentsBox)
	}

	return m.applyTaskViewScroll(rendered)
}

// taskViewportHeight returns how many lines of detail-view content can fit
// between the header and footer. Returns 0 when the terminal height is unknown
// or too small to bother scrolling, in which case the caller should render the
// full content (the terminal will scroll natively if needed).
func (m Model) taskViewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 5 // header(2) + leading blank(1) + footer(2)
	if m.status != "" {
		chrome++
	}
	h := m.height - chrome
	if h < 8 {
		return 0
	}
	return h
}

func taskViewPageStep(viewport int) int {
	step := viewport / 2
	if step < 4 {
		return 4
	}
	return step
}

// applyTaskViewScroll slices the rendered detail content to the available
// viewport based on m.taskViewScroll, appending an indicator when content is
// hidden above or below.
func (m Model) applyTaskViewScroll(content string) string {
	viewport := m.taskViewportHeight()
	lines := strings.Split(content, "\n")
	if viewport <= 0 || len(lines) <= viewport {
		return "\n" + indentBlock(content, 2)
	}

	// Reserve one line for the scroll indicator.
	visibleHeight := viewport - 1
	maxOffset := len(lines) - visibleHeight
	offset := m.taskViewScroll
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	visible := lines[offset : offset+visibleHeight]
	above := offset
	below := len(lines) - (offset + visibleHeight)
	if below < 0 {
		below = 0
	}
	hint := m.styles.hint.Render(fmt.Sprintf("▲ %d above · ▼ %d below  · j/k pgup/pgdn g/G", above, below))
	return "\n" + indentBlock(strings.Join(visible, "\n")+"\n"+hint, 2)
}

func (m Model) renderTaskReference(task domain.Task) string {
	meta := m.styles.hint.Render(fmt.Sprintf("%s · %s", task.BucketKey, task.Priority))
	return m.styles.hintAccent.Render(fmt.Sprintf("#%d", task.ID)) + " " + task.Title + "  " + meta
}

func (m Model) renderBlockerPicker() string {
	task, ok := m.taskByID(m.blockerPickerTaskID)
	if !ok {
		return "\n" + indentBlock(m.styles.panel.Render("Task not found. Press esc to return."), 2)
	}

	contentWidth := m.availableWidth() - 4
	lines := []string{
		m.styles.kicker(fmt.Sprintf("Blockers · #%d", task.ID)),
		m.styles.hint.Render("up/down: move · space: toggle · ctrl+s: save · esc: cancel"),
		"",
		m.styles.metaRow("Task", task.Title, metaRowLabelWidth),
		"",
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	candidates := m.blockerPickerCandidates()
	if len(candidates) == 0 {
		lines = append(lines, m.styles.hint.Render("No other tasks are available to block this task."))
		return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
	}

	dataRows := make([]string, 0, len(candidates))
	for index, candidate := range candidates {
		marker := normalMarker
		if m.blockerPickerCursor == index {
			marker = m.styles.marker.Render(selectionMarker)
		}
		check := m.styles.hint.Render("[ ]")
		if m.blockerPickerChecks[candidate.ID] {
			check = m.styles.hintAccent.Render("[x]")
		}
		meta := m.styles.hint.Render(fmt.Sprintf("%s · %s", candidate.BucketKey, candidate.Priority))
		dataRows = append(dataRows, fmt.Sprintf("%s %s #%d %s  %s", marker, check, candidate.ID, candidate.Title, meta))
	}
	lines = append(lines, m.sliceScrollRows(dataRows, m.blockerPickerScroll, m.blockerPickerViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
}

func (m Model) renderTaskCommentsCell(taskID int64) string {
	comments := m.commentsForTask(taskID)
	lines := []string{
		m.styles.kickerCount("Comments", len(comments)),
	}
	if len(comments) == 0 {
		lines = append(lines, "", m.styles.hint.Render("No comments yet."), m.styles.hint.Render("Press c to add one."))
	} else {
		for _, comment := range comments {
			lines = append(lines, "", m.renderCommentCard(comment))
		}
	}
	if m.isEmbeddedCommentInput() && m.taskID == taskID {
		lines = append(lines, "", m.renderCommentInput())
	}
	return indentBlock(strings.Join(lines, "\n"), 2)
}

func (m Model) renderCommentInput() string {
	lines := []string{
		m.styles.kicker("New comment"),
		m.styles.hint.Render("enter saves · alt+enter/shift+enter newline"),
	}
	if m.status != "" && m.status != "Comment body" {
		lines = append(lines, m.styles.statusBadge(m.status))
	}
	lines = append(lines, m.styles.commentInput.Width(m.commentInputWidth()).Render(m.input))
	return strings.Join(lines, "\n")
}

func (m Model) renderCommentCard(comment domain.Comment) string {
	header := m.styles.hintAccent.Render(comment.AuthorType)
	if strings.TrimSpace(comment.CreatedAt) != "" {
		header += m.styles.hint.Render(" · " + comment.CreatedAt)
	}
	body := strings.TrimSpace(comment.Body)
	if body == "" {
		body = m.styles.hint.Render("empty comment")
	}
	content := header + "\n" + body
	if len(comment.Tags) > 0 {
		badges := make([]string, len(comment.Tags))
		for i, tag := range comment.Tags {
			badges[i] = m.styles.badgeInfo.Render("#" + tag.Label)
		}
		content += "\n" + wrapBadges(badges, commentCardContentWidth)
	}
	return m.styles.commentCard.Render(content)
}

func (m Model) renderTaskForm(title string) string {
	lines := []string{
		m.styles.kicker(title),
		m.styles.hint.Render("ctrl+s saves. tab: switch field. ←/→ changes priority."),
		"",
		m.renderTaskFormLabel(taskFieldTitle, "Title"),
		m.styles.input.Width(m.taskFormWidth()).Render(m.taskTitle),
		"",
		m.renderTaskFormLabel(taskFieldDescription, "Description"),
		m.renderTaskDescriptionInput(),
		"",
		m.renderTaskFormLabel(taskFieldPriority, "Priority"),
		m.renderTaskPriorityInput(),
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
}

func (m Model) renderTaskDescriptionInput() string {
	width := m.taskFormWidth()
	// multilineInput: Padding(0,2) → 4 cols of padding subtracted from Width to
	// get the actual text area. Match lipgloss's wrap so we know exactly how
	// many wrapped lines the text will occupy, then autoscroll-to-end if it
	// exceeds taskDescriptionInputHeight (text always appends at the bottom,
	// so the user's "cursor" is the last wrapped line).
	innerWidth := width - 4
	if innerWidth < 8 {
		innerWidth = 8
	}
	wrapped := wrapLinesToWidth(strings.Split(m.taskDescription, "\n"), innerWidth)

	height := taskDescriptionInputHeight
	var content string
	if len(wrapped) > height {
		visible := wrapped[len(wrapped)-(height-1):]
		hidden := len(wrapped) - len(visible)
		content = m.styles.hint.Render(fmt.Sprintf("▲ %d more above", hidden)) + "\n" + strings.Join(visible, "\n")
	} else {
		content = strings.Join(wrapped, "\n")
	}
	return m.styles.multilineInput.Width(width).Render(content)
}

func (m Model) renderTaskPriorityInput() string {
	levels := []struct {
		key   string
		label string
	}{
		{"low", "low"},
		{"normal", "normal"},
		{"high", "high"},
	}
	var parts []string
	for _, lvl := range levels {
		if lvl.key == m.taskPriority {
			parts = append(parts, m.styles.hintAccent.Render("["+lvl.label+"]"))
		} else {
			parts = append(parts, m.styles.hint.Render(lvl.label))
		}
	}
	return m.styles.input.Width(m.taskFormWidth()).Render(strings.Join(parts, "  "))
}

func (m Model) renderTaskFormLabel(field taskFormField, label string) string {
	marker := " "
	if m.taskField == field {
		marker = ">"
	}
	return m.styles.hintAccent.Render(marker + " // " + strings.ToUpper(label))
}

func (m Model) renderLogs() string {
	if m.repos.ActivityLogs == nil {
		return "\n" + indentBlock(m.styles.panel.Render("Activity logging is not available for this project."), 2)
	}
	if len(m.logs) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No activity yet. Use the CLI, TUI, or MCP to interact with Omakiten."), 2)
	}
	if m.availableWidth() < 92 {
		return m.renderLogsCompact()
	}

	const (
		logOperationWidth = 35
		logProjectWidth   = 11
		logFixedWidth     = 34
	)
	contentWidth := m.availableWidth() - 4
	argsWidth := contentWidth - logFixedWidth - logOperationWidth - logProjectWidth

	limit := minInt(len(m.logs), 50)
	dataRows := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		log := m.logs[i]
		marker := normalMarker
		if i == m.logsSelected {
			marker = m.styles.marker.Render(selectionMarker)
		}

		timeStr := log.StartedAt
		if len(timeStr) > 12 {
			timeStr = timeStr[len(timeStr)-12:]
		}

		statusStyle := m.styles.success
		if log.Status == "error" {
			statusStyle = m.styles.error
		}
		status := statusStyle.Render(fmt.Sprintf("%-5s", log.Status))

		row := fmt.Sprintf("%s %-12s %-4s %-*s %-*s %s %-4d %s",
			marker, timeStr, log.Source, logOperationWidth, truncateText(log.Operation, logOperationWidth), logProjectWidth, truncateText(log.ProjectSlug, logProjectWidth),
			status, log.DurationMs, truncateText(log.ArgumentsJSON, argsWidth))
		dataRows = append(dataRows, row)
	}

	rows := []string{
		m.styles.kickerCount("Activity", limit),
		m.styles.info.Render(fmt.Sprintf("// TIME        SRC  %-*s %-*s STATUS  MS   ARGS", logOperationWidth, "OPERATION", logProjectWidth, "PROJECT")),
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsScroll, m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render("Only app service calls are logged. TUI refreshes and direct reads are not shown."))

	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

// sliceScrollRows clamps `scroll` into a valid range and returns the visible
// slice of single-line data rows plus up-to-2 indicator rows ("▲ N above" /
// "▼ N below") inserted only when content is hidden in that direction. Each
// data row is assumed to be exactly one physical line, so no height heuristic
// is needed. Used by table, logs, and any future list-style view.
func (m Model) sliceScrollRows(dataRows []string, scroll, viewport int) []string {
	if viewport <= 0 || len(dataRows) <= viewport {
		return dataRows
	}
	offset := scroll
	if offset < 0 {
		offset = 0
	}
	maxOffset := len(dataRows) - viewport
	if offset > maxOffset {
		offset = maxOffset
	}

	above := offset
	belowAvailable := len(dataRows) - offset
	visibleHeight := viewport
	if above > 0 {
		visibleHeight--
	}
	if belowAvailable-visibleHeight > 0 {
		visibleHeight--
	}
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	end := offset + visibleHeight
	if end > len(dataRows) {
		end = len(dataRows)
	}
	below := len(dataRows) - end

	out := make([]string, 0, visibleHeight+2)
	if above > 0 {
		out = append(out, m.styles.hint.Render(fmt.Sprintf("▲ %d above", above)))
	}
	out = append(out, dataRows[offset:end]...)
	if below > 0 {
		out = append(out, m.styles.hint.Render(fmt.Sprintf("▼ %d below", below)))
	}
	return out
}

func (m Model) renderLogsCompact() string {
	width := clampInt(m.availableWidth()-4, 32, 72)
	limit := minInt(len(m.logs), 50)
	dataRows := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		log := m.logs[i]
		marker := normalMarker
		if i == m.logsSelected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		timeStr := log.StartedAt
		if len(timeStr) > 8 {
			timeStr = timeStr[len(timeStr)-8:]
		}
		statusStyle := m.styles.success
		if log.Status == "error" {
			statusStyle = m.styles.error
		}
		prefix := fmt.Sprintf("%s %s %s ", marker, timeStr, statusStyle.Render(log.Status))
		budget := clampInt(width-lipgloss.Width(prefix), 8, width)
		dataRows = append(dataRows, prefix+truncateText(log.Operation, budget))
	}
	rows := []string{
		m.styles.kickerCount("Activity", limit),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.logsScroll, m.logsViewportRows())...)
	rows = append(rows, "", m.styles.hint.Render("r refresh · full arguments appear on wider terminals"))
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderTable() string {
	if len(m.tasks) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No tasks yet. Press n to create one."), 2)
	}
	if m.availableWidth() < 74 {
		return m.renderTableCompact()
	}
	const tableFixedWidth = 44
	contentWidth := m.availableWidth() - 4
	titleWidth := contentWidth - tableFixedWidth

	dataRows := make([]string, 0, len(m.tasks))
	for i, task := range m.tasks {
		marker := normalMarker
		if i == m.selected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		dataRows = append(dataRows, fmt.Sprintf("%s %-4d %-11s %-8s %-5d %-9d %s", marker, task.ID, task.BucketKey, task.Priority, m.dependencyCount(task.ID), m.commentCount(task.ID), truncateText(task.Title, titleWidth)))
	}

	rows := []string{
		m.styles.kickerCount("Tasks", len(m.tasks)),
		m.styles.info.Render("// ID   BUCKET      PRI      DEPS  COMMENTS  TITLE"),
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.tableScroll, m.tableViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderTableCompact() string {
	width := clampInt(m.availableWidth()-4, 32, 68)
	dataRows := make([]string, 0, len(m.tasks))
	for i, task := range m.tasks {
		marker := normalMarker
		if i == m.selected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		prefix := fmt.Sprintf("%s #%d %s %s ", marker, task.ID, task.BucketKey, task.Priority)
		budget := clampInt(width-lipgloss.Width(prefix), 8, width)
		dataRows = append(dataRows, prefix+truncateText(task.Title, budget))
	}
	rows := []string{
		m.styles.kickerCount("Tasks", len(m.tasks)),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.tableScroll, m.tableViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderGraph() string {
	if len(m.dependencies) == 0 {
		content := m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join([]string{
			m.styles.kickerCount("Dependency graph", 0),
			"",
			m.styles.hint.Render("No task dependencies yet."),
			m.styles.hint.Render("Use ") + m.styles.hintAccent.Render("okt depend add TASK -i BLOCKER") + m.styles.hint.Render(" to define blocked_by edges."),
		}, "\n"))
		return "\n" + indentBlock(content, 2)
	}

	lines := buildDAGLines(m.dependencies, m.tasks)
	sel := dagSelectableIndices(lines)

	var cursorLineIdx int = -1
	if len(sel) > 0 {
		cursor := clampInt(m.graphCursor, 0, len(sel)-1)
		cursorLineIdx = sel[cursor]
	}

	dataRows := make([]string, len(lines))
	for i, l := range lines {
		if i == cursorLineIdx {
			dataRows[i] = m.styles.hintAccent.Render(l.text)
		} else {
			dataRows[i] = l.text
		}
	}

	rows := []string{
		m.styles.kickerCount("Dependency graph", len(m.dependencies)),
		"",
	}
	rows = append(rows, m.sliceScrollRows(dataRows, m.graphScroll, m.graphViewportRows())...)
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderConfig() string {
	bucketKeys := make([]string, 0, len(m.workflow.Buckets))
	for _, bucket := range m.workflow.Buckets {
		bucketKeys = append(bucketKeys, bucket.Key)
	}
	sort.Strings(bucketKeys)

	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}
	leftRows := [][]string{
		{labelCell("Runtime"), ""},
		{labelCell("Workflow"), m.workflow.Key},
		{labelCell("Buckets"), strings.Join(bucketKeys, ", ")},
		{labelCell("Theme"), m.theme.Key},
		{labelCell("Totals"), ""},
		{labelCell("Tasks"), fmt.Sprintf("%d", len(m.tasks))},
		{labelCell("Comments"), fmt.Sprintf("%d", len(m.comments))},
		{labelCell("Context"), fmt.Sprintf("%d", len(m.entries))},
		{labelCell("Tags"), fmt.Sprintf("%d", len(m.tags))},
	}
	rightRows := [][]string{
		{labelCell("Tokens"), ""},
		{labelCell("Estimated"), fmt.Sprintf("%d", m.metrics.EstimatedTotal)},
		{labelCell("Max"), fmt.Sprintf("%d", m.metrics.MaxTokens)},
	}
	if m.metrics.Truncated {
		rightRows = append(rightRows, []string{m.styles.error.Render("[ERROR]"), m.styles.error.Render("budget exceeded")})
	}

	const (
		configLabelWidth = 13
		configValueWidth = 27
		configTableWidth = 1 + configLabelWidth + 1 + configValueWidth + 1 // 43
		configGap        = 2
	)
	widths := []int{configLabelWidth, configValueWidth}

	var header string
	switch {
	case m.availableWidth() >= configTableWidth*2+configGap:
		left := renderGridTable(leftRows, widths, m.styles.border)
		right := renderGridTable(rightRows, widths, m.styles.border)
		header = lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", configGap), right)
	case m.availableWidth() >= configTableWidth:
		left := renderGridTable(leftRows, widths, m.styles.border)
		right := renderGridTable(rightRows, widths, m.styles.border)
		header = left + "\n\n" + right
	default:
		valueW := clampInt(m.availableWidth()-configLabelWidth-3, 8, configValueWidth)
		narrowWidths := []int{configLabelWidth, valueW}
		all := append(append([][]string{}, leftRows...), rightRows...)
		header = renderGridTable(all, narrowWidths, m.styles.border)
	}

	// Entity lists share borders via renderRowGrid so the bottom of CONFIG
	// follows the same shared-junction grid pattern as the runtime/budget cells
	// above it.
	// 3-col: 1 + 28 + 1 + 28 + 1 + 28 + 1 = 88
	// 4-col: 1 + 28 + 1 + 28 + 1 + 28 + 1 + 28 + 1 = 117
	// 5-col: 1 + 28 + 1 + 28 + 1 + 28 + 1 + 28 + 1 + 28 + 1 = 146
	const (
		entityGridTotal3 = entityListWidth*3 + 4
		entityGridTotal4 = entityListWidth*4 + 5
		entityGridTotal5 = entityListWidth*5 + 6
	)
	colWidths4 := []int{entityListWidth, entityListWidth, entityListWidth, entityListWidth}
	colWidths5 := append(append([]int{}, colWidths4...), entityListWidth)
	var lists string
	switch {
	case m.availableWidth() >= entityGridTotal5:
		lists = renderRowGrid(
			[]string{
				m.renderEntityCell(entityKindLaw),
				m.renderEntityCell(entityKindPersona),
				m.renderEntityCell(entityKindSkill),
				m.renderEntityCell(entityKindTemplate),
				m.renderEntityCell(entityKindTag),
			},
			colWidths5,
			m.styles.border,
		)
	case m.availableWidth() >= entityGridTotal4:
		// Drop the secondary kind (Templates or Tags) the user is not focused on
		// to a row below — same fallback used for tags at the 3-col breakpoint.
		primary := []string{
			m.renderEntityCell(entityKindLaw),
			m.renderEntityCell(entityKindPersona),
			m.renderEntityCell(entityKindSkill),
		}
		var secondary entityKind
		switch m.entityKind {
		case entityKindTemplate:
			primary = append(primary, m.renderEntityCell(entityKindTemplate))
			secondary = entityKindTag
		default:
			primary = append(primary, m.renderEntityCell(entityKindTag))
			secondary = entityKindTemplate
		}
		lists = renderRowGrid(primary, colWidths4, m.styles.border)
		lists += "\n\n" + renderRowGrid(
			[]string{m.renderEntityCell(secondary)},
			[]int{clampInt(m.availableWidth()-2, 16, entityListWidth)},
			m.styles.border,
		)
	case m.availableWidth() >= entityGridTotal3:
		lists = renderRowGrid(
			[]string{
				m.renderEntityCell(entityKindLaw),
				m.renderEntityCell(entityKindPersona),
				m.renderEntityCell(entityKindSkill),
			},
			[]int{entityListWidth, entityListWidth, entityListWidth},
			m.styles.border,
		)
		if m.entityKind == entityKindTag || m.entityKind == entityKindTemplate {
			lists += "\n\n" + renderRowGrid(
				[]string{m.renderEntityCell(m.entityKind)},
				[]int{clampInt(m.availableWidth()-2, 16, entityListWidth)},
				m.styles.border,
			)
		}
	default:
		// Narrow terminals see a single focused column wrapped in its own grid
		// cell so the border treatment stays consistent with the wide layout.
		focusedText := m.styles.hint.Render(fmt.Sprintf("Focused config · %s · left/right switches section", m.entityKind.plural()))
		cellWidth := clampInt(m.availableWidth()-2, 16, entityListWidth)
		lists = focusedText + "\n\n" + renderRowGrid(
			[]string{m.renderEntityCell(m.entityKind)},
			[]int{cellWidth},
			m.styles.border,
		)
	}

	return "\n" + indentBlock(header+"\n\n"+lists, 2)
}

func (m Model) renderFooter() string {
	var text string
	switch {
	case m.isEmbeddedCommentInput():
		text = "enter save comment  alt+enter/shift+enter newline  esc cancel"
	case m.blockerPickerOpen:
		text = "up/down move  pgup/pgdn scroll  space toggle blocker  ctrl+s save  esc cancel"
	case m.mode != modeNormal:
		text = "enter save  esc cancel  ctrl+c quit"
	case m.taskScreen == taskScreenView:
		text = "j/k scroll  e edit  b blockers  c comment  m move  r refresh  esc board  ? help"
	case m.taskScreen == taskScreenCreate:
		text = "tab field  ←/→ priority  ctrl+s create  esc cancel"
	case m.taskScreen == taskScreenEdit:
		text = "tab field  ←/→ priority  ctrl+b blockers  ctrl+s save  esc view"
	case m.entityScreen == entityScreenView && m.deletePending:
		text = "d confirm delete  esc cancel  q quit"
	case m.entityScreen == entityScreenView:
		text = "j/k scroll  e edit in $EDITOR  d arm delete  p skills (persona)  r refresh  esc config"
	case m.entityScreen == entityScreenSkillPicker:
		text = "up/down move  pgup/pgdn scroll  space toggle  enter on '+': new skill  ctrl+s save  esc cancel"
	case m.entityScreen == entityScreenThemePicker:
		text = "up/down move  pgup/pgdn scroll  enter apply (hot-reload)  esc cancel"
	case m.entityScreen == entityScreenConfigPicker:
		text = "up/down move  pgup/pgdn scroll  enter select (restart required)  esc cancel"
	case m.moveMode:
		text = "left/right move task to lane  esc cancel  q quit"
	case m.view == 0:
		text = "left/right lanes  up/down tasks  pgup/pgdn scroll  enter open  n new  e edit  m move  ? help"
	case m.view == 3 && m.deletePending:
		text = "d confirm delete  esc cancel  left/right changes target"
	case m.view == 3 && m.entityKind == entityKindTag:
		text = "left/right section  up/down select  d arm delete (orphan)  D delete all orphans  ? help"
	case m.view == 3:
		text = "left/right section  up/down select  enter open  n new  e edit  d arm delete  t theme  c config  ? help"
	case m.view == 4:
		text = "left/right switch view  up/down select row  pgup/pgdn scroll  g/G top/bottom  r refresh  ? help"
	case m.view == 2:
		text = "left/right switch view  j/k move  pgup/pgdn scroll  g/G top/bottom  enter open  ? help"
	default:
		text = "tab switch view  up/down select  pgup/pgdn scroll  g/G top/bottom  enter open  n new  m move  ? help"
	}
	return "\n" + indentBlock(m.styles.footer.Render(text), 2)
}

func (m Model) selectedTask() (domain.Task, bool) {
	if m.taskScreen != taskScreenClosed && m.taskID > 0 {
		return m.taskByID(m.taskID)
	}
	if m.view == 0 {
		if len(m.workflow.Buckets) == 0 || m.colIdx >= len(m.workflow.Buckets) {
			return domain.Task{}, false
		}
		bucketTasks := m.tasksInCurrentBucket()
		if m.cardIdx < 0 || m.cardIdx >= len(bucketTasks) {
			return domain.Task{}, false
		}
		return bucketTasks[m.cardIdx], true
	}
	if m.selected < 0 || m.selected >= len(m.tasks) {
		return domain.Task{}, false
	}
	return m.tasks[m.selected], true
}

func (m Model) activeTask() (domain.Task, bool) {
	if m.taskID <= 0 {
		return domain.Task{}, false
	}
	return m.taskByID(m.taskID)
}

func (m Model) taskByID(taskID int64) (domain.Task, bool) {
	for _, task := range m.tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.Task{}, false
}

func (m Model) tasksByBucket() map[string][]domain.Task {
	tasksByBucket := map[string][]domain.Task{}
	for _, task := range m.tasks {
		tasksByBucket[task.BucketKey] = append(tasksByBucket[task.BucketKey], task)
	}
	return tasksByBucket
}

func (m Model) tasksInCurrentBucket() []domain.Task {
	if len(m.workflow.Buckets) == 0 || m.colIdx >= len(m.workflow.Buckets) {
		return nil
	}
	return m.tasksByBucket()[m.workflow.Buckets[m.colIdx].Key]
}

func (m *Model) syncSelectedFromBoard() {
	task, ok := m.selectedTask()
	if !ok {
		return
	}
	for i, candidate := range m.tasks {
		if candidate.ID == task.ID {
			m.selected = i
			return
		}
	}
}

func (m *Model) selectTaskByID(taskID int64) bool {
	for i, task := range m.tasks {
		if task.ID != taskID {
			continue
		}

		m.selected = i
		for colIdx, bucket := range m.workflow.Buckets {
			if bucket.Key != task.BucketKey {
				continue
			}

			cardIdx := 0
			for _, candidate := range m.tasks {
				if candidate.BucketKey != task.BucketKey {
					continue
				}
				if candidate.ID == taskID {
					m.colIdx = colIdx
					m.cardIdx = cardIdx
					return true
				}
				cardIdx++
			}
		}
		return true
	}
	return false
}

func (m *Model) clampSelection() {
	if m.selected >= len(m.tasks) {
		m.selected = len(m.tasks) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.colIdx >= len(m.workflow.Buckets) {
		m.colIdx = len(m.workflow.Buckets) - 1
	}
	if m.colIdx < 0 {
		m.colIdx = 0
	}
}

func (m *Model) clampCardIdx() {
	tasks := m.tasksInCurrentBucket()
	if len(tasks) == 0 {
		m.cardIdx = 0
		return
	}
	if m.cardIdx >= len(tasks) {
		m.cardIdx = len(tasks) - 1
	}
	if m.cardIdx < 0 {
		m.cardIdx = 0
	}
}

func (m Model) dependencyCount(taskID int64) int {
	count := 0
	for _, dependency := range m.dependencies {
		if dependency.TaskID == taskID {
			count++
		}
	}
	return count
}

func (m Model) blockersForTask(taskID int64) []domain.Task {
	blockers := make([]domain.Task, 0)
	for _, dependency := range m.dependencies {
		if dependency.TaskID != taskID {
			continue
		}
		if blocker, ok := m.taskByID(dependency.DependsOnTaskID); ok {
			blockers = append(blockers, blocker)
		}
	}
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].ID < blockers[j].ID })
	return blockers
}

func (m Model) commentCount(taskID int64) int {
	return len(m.commentsForTask(taskID))
}

func (m Model) tagsForTask(taskID int64) []domain.Tag {
	if m.taskTagsMap == nil {
		return nil
	}
	return m.taskTagsMap[taskID]
}

func (m Model) commentsForTask(taskID int64) []domain.Comment {
	comments := make([]domain.Comment, 0)
	for _, comment := range m.comments {
		if comment.TaskID == taskID {
			comments = append(comments, comment)
		}
	}
	return comments
}

func (m Model) taskIndicator(task domain.Task) lipgloss.Style {
	if m.dependencyCount(task.ID) > 0 {
		return m.styles.warning
	}
	switch task.Priority {
	case domain.PriorityHigh:
		return m.styles.error
	case domain.PriorityLow:
		return m.styles.muted
	default:
		return m.styles.success
	}
}

func truncateText(s string, max int) string {
	runes := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// wrapWords breaks s into lines where the first line is constrained to firstWidth
// and subsequent lines to restWidth. It tries to keep whole words.
func wrapWords(s string, firstWidth, restWidth int) []string {
	if s == "" {
		return []string{""}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		limit := firstWidth
		if len(lines) > 0 {
			limit = restWidth
		}
		if lipgloss.Width(current+" "+word) <= limit {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	lines = append(lines, current)
	return lines
}

func trimLastRune(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	return string(runes[:len(runes)-1])
}

func renderFixedBox(lines []string, width int, border lipgloss.Style) string {
	rows := []string{border.Render("┌" + strings.Repeat("─", width) + "┐")}
	for _, line := range lines {
		rows = append(rows, border.Render("│")+padStyledLine(line, width)+border.Render("│"))
	}
	rows = append(rows, border.Render("└"+strings.Repeat("─", width)+"┘"))
	return strings.Join(rows, "\n")
}

// renderRowGrid renders cells as a single horizontal row with shared borders
// using ┌┬┐ / └┴┘ junctions, so neighboring cells visually share a border —
// the omacon "delimited grid" pattern. Each cell's content is padded to
// widths[i] columns and rows are padded so all cells are the same height.
func renderRowGrid(cells []string, widths []int, border lipgloss.Style) string {
	n := len(cells)
	if n == 0 || n != len(widths) {
		return ""
	}
	cellLines := make([][]string, n)
	maxHeight := 0
	for i, cell := range cells {
		lines := wrapLinesToWidth(strings.Split(cell, "\n"), widths[i])
		cellLines[i] = lines
		if len(lines) > maxHeight {
			maxHeight = len(lines)
		}
	}
	for i := range cellLines {
		for len(cellLines[i]) < maxHeight {
			cellLines[i] = append(cellLines[i], "")
		}
	}

	var top, bot strings.Builder
	top.WriteString(border.Render("┌"))
	bot.WriteString(border.Render("└"))
	for i, w := range widths {
		rule := strings.Repeat("─", w)
		top.WriteString(border.Render(rule))
		bot.WriteString(border.Render(rule))
		if i < n-1 {
			top.WriteString(border.Render("┬"))
			bot.WriteString(border.Render("┴"))
		}
	}
	top.WriteString(border.Render("┐"))
	bot.WriteString(border.Render("┘"))

	rows := []string{top.String()}
	bar := border.Render("│")
	for r := 0; r < maxHeight; r++ {
		var row strings.Builder
		row.WriteString(bar)
		for i, w := range widths {
			row.WriteString(padStyledLine(cellLines[i][r], w))
			row.WriteString(bar)
		}
		rows = append(rows, row.String())
	}
	rows = append(rows, bot.String())
	return strings.Join(rows, "\n")
}

// renderGridTable renders rows in a multi-row, multi-column table where every
// cell is delimited by ─ and │ with shared junctions (┌┬┐ ├┼┤ └┴┘). Each row
// must have len(widths) cells; missing trailing cells render as empty. A row
// with a single cell when n>1 is treated as a spanned row that covers the full
// width, and the surrounding horizontal dividers omit the internal junction.
func renderGridTable(rows [][]string, widths []int, border lipgloss.Style) string {
	n := len(widths)
	if len(rows) == 0 || n == 0 {
		return ""
	}

	totalWidth := 0
	for _, w := range widths {
		totalWidth += w
	}
	totalWidth += n - 1

	spanned := make([]bool, len(rows))
	rowLines := make([][][]string, len(rows))
	rowHeights := make([]int, len(rows))
	for r, row := range rows {
		if n > 1 && len(row) == 1 {
			spanned[r] = true
			lines := wrapLinesToWidth(strings.Split(row[0], "\n"), totalWidth)
			rowLines[r] = [][]string{lines}
			rowHeights[r] = len(lines)
			continue
		}
		cells := make([][]string, n)
		h := 0
		for c := 0; c < n; c++ {
			text := ""
			if c < len(row) {
				text = row[c]
			}
			lines := wrapLinesToWidth(strings.Split(text, "\n"), widths[c])
			cells[c] = lines
			if len(lines) > h {
				h = len(lines)
			}
		}
		for c := 0; c < n; c++ {
			for len(cells[c]) < h {
				cells[c] = append(cells[c], "")
			}
		}
		rowLines[r] = cells
		rowHeights[r] = h
	}

	horizontal := func(left, right string, aboveSpanned, belowSpanned bool) string {
		var b strings.Builder
		b.WriteString(border.Render(left))
		for i, w := range widths {
			b.WriteString(border.Render(strings.Repeat("─", w)))
			if i < n-1 {
				var junc string
				switch {
				case aboveSpanned && belowSpanned:
					junc = "─"
				case aboveSpanned && !belowSpanned:
					junc = "┬"
				case !aboveSpanned && belowSpanned:
					junc = "┴"
				default:
					junc = "┼"
				}
				b.WriteString(border.Render(junc))
			}
		}
		b.WriteString(border.Render(right))
		return b.String()
	}

	bar := border.Render("│")
	var out strings.Builder
	out.WriteString(horizontal("┌", "┐", true, spanned[0]))
	for r, h := range rowHeights {
		for line := 0; line < h; line++ {
			out.WriteString("\n")
			out.WriteString(bar)
			if spanned[r] {
				out.WriteString(padStyledLine(rowLines[r][0][line], totalWidth))
			} else {
				for c, w := range widths {
					out.WriteString(padStyledLine(rowLines[r][c][line], w))
					if c < n-1 {
						out.WriteString(bar)
					}
				}
			}
			out.WriteString(bar)
		}
		if r < len(rows)-1 {
			out.WriteString("\n")
			out.WriteString(horizontal("├", "┤", spanned[r], spanned[r+1]))
		}
	}
	out.WriteString("\n")
	out.WriteString(horizontal("└", "┘", spanned[len(rows)-1], true))
	return out.String()
}

func wrapLinesToWidth(lines []string, width int) []string {
	if width <= 0 {
		width = 1
	}

	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if lipgloss.Width(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}

		parts := strings.Split(ansi.Wrap(line, width, " "), "\n")
		wrapped = append(wrapped, parts...)
	}

	if len(wrapped) == 0 {
		return []string{""}
	}
	return wrapped
}

func padStyledLine(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
}

func clampInt(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func indentBlock(block string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// kicker renders a section label in dev-editorial style. Structural labels use
// the secondary color so the primary accent stays reserved for active focus.
func (s styles) kicker(label string) string {
	return s.info.Render("// " + strings.ToUpper(label))
}

// kickerCount renders `// LABEL · N` — kicker with a trailing count.
func (s styles) kickerCount(label string, count int) string {
	return s.info.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(label), count))
}

// metaRow renders a definition-list row: `// LABEL` (kicker) + value, the label
// padded to labelWidth so values align across multiple rows.
func (s styles) metaRow(label, value string, labelWidth int) string {
	rendered := "// " + strings.ToUpper(label)
	pad := labelWidth - lipgloss.Width(rendered)
	if pad < 1 {
		pad = 1
	}
	return s.info.Render(rendered) + strings.Repeat(" ", pad) + value
}

// metaRowWrap is metaRow with word-wrapping. Long values break onto
// continuation lines that are indented to the value gutter so the visual
// alignment is preserved. contentWidth is the total cell width available.
func (s styles) metaRowWrap(label, value string, labelWidth, contentWidth int) string {
	rendered := "// " + strings.ToUpper(label)
	pad := labelWidth - lipgloss.Width(rendered)
	if pad < 1 {
		pad = 1
	}
	gutter := lipgloss.Width(rendered) + pad
	valueWidth := contentWidth - gutter
	if valueWidth < 1 || contentWidth <= 0 {
		return s.info.Render(rendered) + strings.Repeat(" ", pad) + value
	}
	wrapped := wrapWords(value, valueWidth, valueWidth)
	indent := strings.Repeat(" ", gutter)
	var b strings.Builder
	for i, line := range wrapped {
		if i == 0 {
			b.WriteString(s.info.Render(rendered))
			b.WriteString(strings.Repeat(" ", pad))
		} else {
			b.WriteString("\n")
			b.WriteString(indent)
		}
		b.WriteString(line)
	}
	return b.String()
}

// statusBadge renders a status message as `[INFO] msg` or `[ERROR] msg` based
// on a content heuristic. Replaces italic-on-secondary status rendering.
func (s styles) statusBadge(msg string) string {
	if msg == "" {
		return ""
	}
	level := "INFO"
	tagStyle := s.info
	lower := strings.ToLower(msg)
	for _, needle := range []string{"confirm", "pending"} {
		if strings.Contains(lower, needle) {
			level = "WARN"
			tagStyle = s.warning
			break
		}
	}
	for _, needle := range []string{"error", "fail", "not found", "required", "missing", "invalid", "exceeded"} {
		if strings.Contains(lower, needle) {
			level = "ERROR"
			tagStyle = s.error
			break
		}
	}
	return tagStyle.Render("["+level+"]") + " " + s.muted.Render(msg)
}

type styles struct {
	title          lipgloss.Style
	nav            lipgloss.Style
	activeNav      lipgloss.Style
	panel          lipgloss.Style
	commentCard    lipgloss.Style
	commentInput   lipgloss.Style
	border         lipgloss.Style
	kanbanColumn   lipgloss.Style
	card           lipgloss.Style
	cardSelected   lipgloss.Style
	entityCard     lipgloss.Style
	entityCardSelected lipgloss.Style
	marker         lipgloss.Style
	separator      lipgloss.Style
	empty          lipgloss.Style
	input          lipgloss.Style
	multilineInput lipgloss.Style
	footer         lipgloss.Style
	hint           lipgloss.Style
	hintAccent     lipgloss.Style
	hintBox        lipgloss.Style
	muted          lipgloss.Style
	info           lipgloss.Style
	success        lipgloss.Style
	warning        lipgloss.Style
	error          lipgloss.Style

	badgeHigh        lipgloss.Style
	badgeNormal      lipgloss.Style
	badgeLow         lipgloss.Style
	badgeBlocker     lipgloss.Style
	badgeComment     lipgloss.Style
	badgeInfo        lipgloss.Style
	badgeScope       lipgloss.Style
	badgeFix         lipgloss.Style
	badgeTokenGreen  lipgloss.Style
	badgeTokenYellow lipgloss.Style
	badgeTokenRed    lipgloss.Style
}

func newStyles(theme config.Theme) styles {
	color := func(key, fallback string) lipgloss.Color {
		if value := theme.Colors[key]; value != "" {
			return lipgloss.Color(value)
		}
		return lipgloss.Color(fallback)
	}

	border := color("border", "#494543")
	foreground := color("foreground", "#E5E2E1")
	primary := color("primary", "#39FF14")
	secondary := color("secondary", "#8FAE9A")
	success := color("success", "#86D27A")
	warning := color("warning", "#FFB347")
	errorColor := color("error", "#FF5544")
	// badgeFg is the foreground used on filled-pill badges (dark text on a
	// bright background). Themable via the `badge_fg` color so dark-themed
	// palettes can override it.
	badgeFg := color("badge_fg", "#1A1A1A")

	return styles{
		title:          lipgloss.NewStyle().Bold(true).Foreground(primary),
		nav:            lipgloss.NewStyle().Foreground(secondary),
		activeNav:      lipgloss.NewStyle().Foreground(primary).Bold(true),
		panel:          lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		commentCard:    lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(taskCommentsPanelWidth - 8),
		commentInput:   lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(taskCommentsPanelWidth - 8).Height(commentInputHeight),
		border:         lipgloss.NewStyle().Foreground(border),
		kanbanColumn:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Width(columnWidth).Padding(0, 0),
		card:           lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
		cardSelected:   lipgloss.NewStyle().Foreground(foreground).Bold(true).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(cardBoxWidth),
		entityCard:     lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
		entityCardSelected: lipgloss.NewStyle().Foreground(foreground).Bold(true).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(cardBoxWidth),
		marker:         lipgloss.NewStyle().Foreground(primary).Bold(true),
		separator:      lipgloss.NewStyle().Foreground(border),
		empty:          lipgloss.NewStyle().Foreground(border).Width(columnWidth).Align(lipgloss.Center),
		input:          lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 2),
		multilineInput: lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 2).Width(taskFormInputWidth).Height(taskDescriptionInputHeight),
		footer:         lipgloss.NewStyle().Foreground(border),
		hint:           lipgloss.NewStyle().Foreground(border),
		hintAccent:     lipgloss.NewStyle().Foreground(primary).Bold(true),
		hintBox:        lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2).Width(60),
		muted:          lipgloss.NewStyle().Foreground(border),
		info:           lipgloss.NewStyle().Foreground(secondary),
		success:        lipgloss.NewStyle().Foreground(success),
		warning:        lipgloss.NewStyle().Foreground(warning),
		error:          lipgloss.NewStyle().Foreground(errorColor),

		badgeHigh:        lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeNormal:      lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeLow:         lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeBlocker:     lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeComment:     lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		badgeInfo:        lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeScope:       lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		badgeFix:         lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenGreen:  lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenYellow: lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenRed:    lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),
	}
}
