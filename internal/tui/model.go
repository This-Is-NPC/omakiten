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
	selectionMarker            = "▌"
	normalMarker               = " "
	cardBoxWidth               = 26
	cardContentWidth           = 24 // cardBoxWidth - horizontal padding(2); text fits here
)

type Repositories struct {
	Tasks        app.TaskRepository
	Comments     app.CommentRepository
	Dependencies app.DependencyRepository
	Entries      app.ContextEntryRepository
	Config       app.ConfigRepository
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

	tasks        []domain.Task
	workflow     domain.Workflow
	dependencies []domain.TaskDependency
	comments     []domain.Comment
	laws         []domain.Law
	skills       []domain.Skill
	personas     []domain.Persona
	entries      []domain.ContextEntry
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
		entityCursors: map[entityKind]int{entityKindLaw: 0, entityKindPersona: 0, entityKindSkill: 0},
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
			case "?", "esc", "q":
				m.helpOpen = false
				m.helpAll = false
			}
			return m, nil
		}
		if msg.String() == "?" && m.mode == modeNormal {
			m.helpOpen = true
			m.helpAll = false
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
	return clampInt(m.availableWidth()-8, 32, taskFormInputWidth)
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
		}
	case "up", "k":
		if m.cardIdx > 0 {
			m.cardIdx--
			m.syncSelectedFromBoard()
		}
	case "down", "j":
		bucketTasks := m.tasksInCurrentBucket()
		if m.cardIdx < len(bucketTasks)-1 {
			m.cardIdx++
			m.syncSelectedFromBoard()
		}
	}
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
		}
	case "down", "j":
		if m.selected < len(m.tasks)-1 {
			m.selected++
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

func (m *Model) handleLogsKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "left", "h":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
	case "right", "l":
		m.view = (m.view + 1) % len(viewNames)
	case "up", "k":
		if m.logsSelected > 0 {
			m.logsSelected--
		}
	case "down", "j":
		if m.logsSelected < len(m.logs)-1 {
			m.logsSelected++
		}
	}
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
	switch msg.String() {
	case "ctrl+c", "q":
		return *m, tea.Quit
	case "esc":
		m.closeBlockerPicker("Cancelled")
	case "up", "k":
		if m.blockerPickerCursor > 0 {
			m.blockerPickerCursor--
		}
	case "down", "j":
		if m.blockerPickerCursor < len(candidates)-1 {
			m.blockerPickerCursor++
		}
	case " ", "space":
		if m.blockerPickerCursor >= 0 && m.blockerPickerCursor < len(candidates) {
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
}

func (m *Model) closeBlockerPicker(status string) {
	m.blockerPickerOpen = false
	m.blockerPickerTaskID = 0
	m.blockerPickerCursor = 0
	m.blockerPickerChecks = nil
	m.status = status
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
		_, err = app.NewCommentService(m.repos.Comments).Add(m.ctx, m.project, task.ID, input, "human")
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
	if m.repos.Editor != nil {
		bundle, err := m.repos.Editor.Load()
		if err != nil {
			return err
		}
		skills = enrichSkillsFromBundle(skills, bundle)
		laws = enrichLawsFromBundle(laws, bundle)
		personas = enrichPersonasFromBundle(personas, bundle)
	}
	entries, err := m.repos.Entries.ListContextEntries(m.ctx, m.project.ID)
	if err != nil {
		return err
	}
	settings, err := m.repos.Config.ContextSettings(m.ctx)
	if err != nil {
		return err
	}

	m.tasks = tasks
	m.workflow = workflow
	m.dependencies = dependencies
	m.comments = comments
	m.laws = laws
	m.skills = skills
	m.personas = personas
	m.entries = entries
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
	if m.taskScreen != taskScreenClosed || m.entityScreen != entityScreenClosed {
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

func (m Model) renderBoard() string {
	if len(m.workflow.Buckets) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No workflow buckets. Add buckets in the active workflow config."), 2)
	}

	tasksByBucket := m.tasksByBucket()
	cells := make([]string, 0, len(m.workflow.Buckets))
	totalTasks := 0
	for i, bucket := range m.workflow.Buckets {
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if i == m.colIdx {
			selectedIdx = m.cardIdx
		}
		cellContent := m.renderKanbanCell(bucket, bucketTasks, i == m.colIdx, selectedIdx)
		col := m.styles.kanbanColumn.Render(cellContent)
		cells = append(cells, col)
		totalTasks += len(bucketTasks)
	}
	if len(cells) > 0 && len(cells)*columnWidth+len(cells)-1 > m.availableWidth() {
		current := clampInt(m.colIdx, 0, len(m.workflow.Buckets)-1)
		bucket := m.workflow.Buckets[current]
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if len(bucketTasks) > 0 {
			selectedIdx = clampInt(m.cardIdx, 0, len(bucketTasks)-1)
		}
		board := m.styles.kanbanColumn.Render(m.renderKanbanCell(bucket, bucketTasks, true, selectedIdx))
		hint := m.styles.hint.Render(fmt.Sprintf("Column %d/%d · left/right switches lane", current+1, len(m.workflow.Buckets)))
		var sb strings.Builder
		sb.WriteString("\n")
		sb.WriteString(indentBlock(board+"\n"+hint, 2))
		if totalTasks == 0 {
			sb.WriteString("\n\n")
			sb.WriteString(indentBlock(m.renderEmptyBoardHint(), 2))
		}
		return sb.String()
	}

	// Join columns side-by-side with a narrow spacer between them.
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

func (m Model) renderKanbanCell(bucket domain.Bucket, tasks []domain.Task, focused bool, selectedIdx int) string {
	headerStyle := m.styles.hintAccent
	if !focused {
		headerStyle = m.styles.muted
	}
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(bucket.Name), len(tasks))
	lines := []string{headerStyle.Render(headerText), ""}

	if len(tasks) == 0 {
		lines = append(lines, m.styles.empty.Render(centerText("empty", columnWidth)))
	} else {
		for i, task := range tasks {
			lines = append(lines, m.renderCard(task, focused && i == selectedIdx))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderCard(task domain.Task, selected bool) string {
	marker := normalMarker
	if selected {
		marker = m.styles.marker.Render(selectionMarker)
	}
	prefix := fmt.Sprintf("%s #%d ", marker, task.ID)
	prefixWidth := lipgloss.Width(prefix)

	firstWidth := cardContentWidth - prefixWidth
	restWidth := cardContentWidth - prefixWidth
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

	// Badges line (truncated to fit card width)
	if badgeLine := m.renderTaskBadges(task, cardContentWidth); badgeLine != "" {
		lines = append(lines, badgeLine)
	}

	style := m.styles.card
	if selected {
		style = m.styles.cardSelected
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderTaskBadges builds a line of colored badges for a task: priority,
// blocker count, and comment count. Each badge is rendered as a filled pill
// using Lipgloss background colors. Badges are dropped from the right when
// the total width would exceed maxWidth.
func (m Model) renderTaskBadges(task domain.Task, maxWidth int) string {
	var badges []string
	totalWidth := 0

	tryAdd := func(badge string, width int) bool {
		sep := 0
		if totalWidth > 0 {
			sep = 1 // space between badges
		}
		if totalWidth+sep+width > maxWidth {
			return false
		}
		totalWidth += sep + width
		badges = append(badges, badge)
		return true
	}

	// Priority badge (always shown)
	var priorityBadge string
	switch task.Priority {
	case domain.PriorityHigh:
		priorityBadge = m.styles.badgeHigh.Render("HIGH")
	case domain.PriorityLow:
		priorityBadge = m.styles.badgeLow.Render("LOW")
	default:
		priorityBadge = m.styles.badgeNormal.Render("NORM")
	}
	tryAdd(priorityBadge, lipgloss.Width(priorityBadge))

	// Blockers badge
	if deps := m.dependencyCount(task.ID); deps > 0 {
		badge := m.styles.badgeBlocker.Render(fmt.Sprintf("%d BLK", deps))
		tryAdd(badge, lipgloss.Width(badge))
	}

	// Comments badge
	if cmts := m.commentCount(task.ID); cmts > 0 {
		badge := m.styles.badgeComment.Render(fmt.Sprintf("%d CMT", cmts))
		tryAdd(badge, lipgloss.Width(badge))
	}

	return strings.Join(badges, " ")
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
			{"← ↑ ↓ → · h j k l", "navigate lanes and tasks"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"c", "add comment"},
			{"m", "move task between lanes"},
		}},
		{"Task list", []binding{
			{"↑ ↓ · j k", "select task"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"m", "move by bucket key"},
		}},
		{"Task view", []binding{
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
			{"↑ ↓ · j k", "move"},
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
			{"e", "edit (opens $EDITOR)"},
			{"d · d", "arm delete, then confirm"},
			{"p", "skill picker (persona)"},
			{"esc", "back, or cancel pending delete"},
		}},
		{"Skill picker", []binding{
			{"↑ ↓", "move"},
			{"space", "toggle"},
			{"enter on '+ create new'", "scaffold new skill"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Logs", []binding{
			{"← →", "switch view"},
			{"↑ ↓", "select row"},
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
	return "\n" + indentBlock(strings.Join(lines, "\n"), 2)
}

func (m Model) renderHelpFooter() string {
	return indentBlock(m.styles.footer.Render("a all/current · ?/esc/q close help"), 2)
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
	case m.view == 1 || m.view == 2:
		return []string{"Task list"}
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

	detailLines := []string{
		m.styles.kicker(fmt.Sprintf("Task · #%d", task.ID)),
		"",
		m.styles.metaRow("Title", task.Title, 14),
		m.styles.metaRow("Bucket", task.BucketKey, 14),
		m.styles.metaRow("Priority", string(task.Priority), 14),
		m.styles.metaRow("Comments", fmt.Sprintf("%d", m.commentCount(task.ID)), 14),
		"",
		m.styles.kickerCount("Blockers", len(blockers)),
	}
	if len(blockers) == 0 {
		detailLines = append(detailLines, m.styles.hint.Render("No blockers. Press b to add one."))
	} else {
		for _, blocker := range blockers {
			detailLines = append(detailLines, m.renderTaskReference(blocker))
		}
	}
	detailLines = append(detailLines,
		"",
		m.styles.kicker("Description"),
	)
	if strings.TrimSpace(task.Description) == "" {
		detailLines = append(detailLines, m.styles.hint.Render("No description"))
	} else {
		detailLines = append(detailLines, task.Description)
	}

	if m.availableWidth() < taskDetailsPanelWidth+taskCommentsPanelWidth+3 {
		panelWidth := clampInt(m.availableWidth()-4, 36, 72)
		detailsBox := renderFixedBox(wrapLinesToWidth(detailLines, panelWidth), panelWidth, m.styles.border)
		commentsBox := renderFixedBox(wrapLinesToWidth(strings.Split(m.renderTaskCommentsCell(task.ID), "\n"), panelWidth), panelWidth, m.styles.border)
		return "\n" + indentBlock(detailsBox+"\n\n"+commentsBox, 2)
	}

	detailsCell := indentBlock(strings.Join(detailLines, "\n"), 2)
	commentsCell := m.renderTaskCommentsCell(task.ID)
	grid := renderRowGrid(
		[]string{detailsCell, commentsCell},
		[]int{taskDetailsPanelWidth, taskCommentsPanelWidth},
		m.styles.border,
	)
	return "\n" + indentBlock(grid, 2)
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

	lines := []string{
		m.styles.kicker(fmt.Sprintf("Blockers · #%d", task.ID)),
		m.styles.hint.Render("up/down: move · space: toggle · ctrl+s: save · esc: cancel"),
		"",
		m.styles.metaRow("Task", task.Title, 10),
		"",
	}
	candidates := m.blockerPickerCandidates()
	if len(candidates) == 0 {
		lines = append(lines, m.styles.hint.Render("No other tasks are available to block this task."))
	} else {
		for index, candidate := range candidates {
			marker := " "
			if m.blockerPickerCursor == index {
				marker = ">"
			}
			check := m.styles.hint.Render("[ ]")
			if m.blockerPickerChecks[candidate.ID] {
				check = m.styles.hintAccent.Render("[x]")
			}
			meta := m.styles.hint.Render(fmt.Sprintf("%s · %s", candidate.BucketKey, candidate.Priority))
			row := fmt.Sprintf("%s %s #%d %s  %s", marker, check, candidate.ID, candidate.Title, meta)
			lines = append(lines, row)
		}
	}
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
	return m.styles.commentCard.Render(header + "\n" + body)
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
	return m.styles.multilineInput.Width(m.taskFormWidth()).Render(m.taskDescription)
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
	rows := []string{
		m.styles.kicker(fmt.Sprintf("Activity · %d", minInt(len(m.logs), 50))),
		m.styles.info.Render(fmt.Sprintf("// TIME        SRC  %-*s %-*s STATUS  MS   ARGS", logOperationWidth, "OPERATION", logProjectWidth, "PROJECT")),
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}

	for i, log := range m.logs {
		if i >= 50 {
			break
		}
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
		rows = append(rows, row)
	}

	rows = append(rows, "", m.styles.hint.Render("Only app service calls are logged. TUI refreshes and direct reads are not shown."))

	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderLogsCompact() string {
	width := clampInt(m.availableWidth()-4, 32, 72)
	rows := []string{
		m.styles.kicker(fmt.Sprintf("Activity · showing %d", minInt(len(m.logs), 50))),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}
	for i, log := range m.logs {
		if i >= 50 {
			break
		}
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
		rows = append(rows, prefix+truncateText(log.Operation, budget))
	}
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
	rows := []string{
		m.styles.kicker(fmt.Sprintf("Tasks · %d", len(m.tasks))),
		m.styles.info.Render("// ID   BUCKET      PRI      DEPS  COMMENTS  TITLE"),
		m.styles.separator.Render(strings.Repeat("─", contentWidth)),
	}
	for i, task := range m.tasks {
		marker := normalMarker
		if i == m.selected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		row := fmt.Sprintf("%s %-4d %-11s %-8s %-5d %-9d %s", marker, task.ID, task.BucketKey, task.Priority, m.dependencyCount(task.ID), m.commentCount(task.ID), truncateText(task.Title, titleWidth))
		rows = append(rows, row)
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderTableCompact() string {
	width := clampInt(m.availableWidth()-4, 32, 68)
	rows := []string{
		m.styles.kicker(fmt.Sprintf("Tasks · %d", len(m.tasks))),
		m.styles.separator.Render(strings.Repeat("─", width)),
	}
	for i, task := range m.tasks {
		marker := normalMarker
		if i == m.selected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		prefix := fmt.Sprintf("%s #%d %s %s ", marker, task.ID, task.BucketKey, task.Priority)
		budget := clampInt(width-lipgloss.Width(prefix), 8, width)
		rows = append(rows, prefix+truncateText(task.Title, budget))
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderGraph() string {
	if len(m.dependencies) == 0 {
		content := m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join([]string{
			m.styles.kicker("Dependency graph · 0 edges"),
			"",
			m.styles.hint.Render("No task dependencies yet."),
			m.styles.hint.Render("Use ") + m.styles.hintAccent.Render("okt depend add TASK -i BLOCKER") + m.styles.hint.Render(" to define blocked_by edges."),
		}, "\n"))
		return "\n" + indentBlock(content, 2)
	}
	lines := []string{m.styles.kicker(fmt.Sprintf("Dependency graph · %d edges", len(m.dependencies))), m.styles.separator.Render(strings.Repeat("─", 44))}
	for _, dependency := range m.dependencies {
		lines = append(lines, fmt.Sprintf("#%d %s #%d", dependency.TaskID, m.styles.hint.Render("blocked_by"), dependency.DependsOnTaskID))
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
}

func (m Model) renderConfig() string {
	bucketKeys := make([]string, 0, len(m.workflow.Buckets))
	for _, bucket := range m.workflow.Buckets {
		bucketKeys = append(bucketKeys, bucket.Key)
	}
	sort.Strings(bucketKeys)

	left := []string{
		m.styles.kicker("Runtime"),
		m.styles.metaRow("Workflow", m.workflow.Key, 14),
		m.styles.metaRow("Buckets", strings.Join(bucketKeys, ", "), 14),
		m.styles.metaRow("Theme", m.theme.Key, 14),
		"",
		m.styles.kicker("Totals"),
		m.styles.metaRow("Tasks", fmt.Sprintf("%d", len(m.tasks)), 14),
		m.styles.metaRow("Comments", fmt.Sprintf("%d", len(m.comments)), 14),
		m.styles.metaRow("Context", fmt.Sprintf("%d", len(m.entries)), 14),
	}
	right := []string{
		m.styles.kicker("Token budget"),
		m.styles.metaRow("Estimated", fmt.Sprintf("%d", m.metrics.EstimatedTotal), 14),
		m.styles.metaRow("Max", fmt.Sprintf("%d", m.metrics.MaxTokens), 14),
	}
	if m.metrics.Truncated {
		right = append(right, m.styles.error.Render("[ERROR] budget exceeded"))
	}

	const configCellWidth = 40
	var header string
	if m.availableWidth() >= configCellWidth*2+3 {
		header = renderRowGrid(
			[]string{strings.Join(left, "\n"), strings.Join(right, "\n")},
			[]int{configCellWidth, configCellWidth},
			m.styles.border,
		)
	} else {
		width := clampInt(m.availableWidth()-4, 32, configCellWidth)
		summary := append(append([]string{}, left...), "")
		summary = append(summary, right...)
		header = renderFixedBox(wrapLinesToWidth(summary, width), width, m.styles.border)
	}

	var lists string
	if m.availableWidth() >= entityListWidth*3+3 {
		lists = renderRowGrid(
			[]string{
				m.renderEntityCell(entityKindLaw),
				m.renderEntityCell(entityKindPersona),
				m.renderEntityCell(entityKindSkill),
			},
			[]int{entityListWidth, entityListWidth, entityListWidth},
			m.styles.border,
		)
	} else {
		focused := []string{
			m.styles.hint.Render(fmt.Sprintf("Focused config · %s · left/right switches section", m.entityKind.plural())),
			"",
		}
		focused = append(focused, strings.Split(m.renderEntityCell(m.entityKind), "\n")...)
		lists = renderFixedBox(wrapLinesToWidth(focused, entityListWidth), entityListWidth, m.styles.border)
	}

	return "\n" + indentBlock(header+"\n\n"+lists, 2)
}

func (m Model) renderFooter() string {
	var text string
	switch {
	case m.isEmbeddedCommentInput():
		text = "enter save comment  alt+enter/shift+enter newline  esc cancel"
	case m.blockerPickerOpen:
		text = "up/down move  space toggle blocker  ctrl+s save  esc cancel"
	case m.mode != modeNormal:
		text = "enter save  esc cancel  ctrl+c quit"
	case m.taskScreen == taskScreenView:
		text = "e edit  b blockers  c comment  m move  r refresh  esc board  ? help"
	case m.taskScreen == taskScreenCreate:
		text = "tab field  ←/→ priority  ctrl+s create  esc cancel"
	case m.taskScreen == taskScreenEdit:
		text = "tab field  ←/→ priority  ctrl+b blockers  ctrl+s save  esc view"
	case m.entityScreen == entityScreenView && m.deletePending:
		text = "d confirm delete  esc cancel  q quit"
	case m.entityScreen == entityScreenView:
		text = "e edit in $EDITOR  d arm delete  p skills (persona)  r refresh  esc config"
	case m.entityScreen == entityScreenSkillPicker:
		text = "up/down move  space toggle  enter on '+': new skill  ctrl+s save  esc cancel"
	case m.moveMode:
		text = "left/right move task to lane  esc cancel  q quit"
	case m.view == 0:
		text = "left/right lanes  up/down tasks  enter open  n new  e edit  c comment  m move  ? help"
	case m.view == 3 && m.deletePending:
		text = "d confirm delete  esc cancel  left/right changes target"
	case m.view == 3:
		text = "left/right section  up/down select  enter open  n new  e edit  d arm delete  ? help"
	case m.view == 4:
		text = "left/right switch view  up/down select row  r refresh  ? help"
	default:
		text = "tab switch view  up/down select  enter open  n new  e edit  m move  ? help"
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

func centerText(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	left := (width - visible) / 2
	right := width - visible - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
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
	columnTitle    lipgloss.Style
	card           lipgloss.Style
	cardSelected   lipgloss.Style
	kanbanColumn   lipgloss.Style
	entityRow      lipgloss.Style
	entitySelected lipgloss.Style
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
	badgeHigh      lipgloss.Style
	badgeNormal    lipgloss.Style
	badgeLow       lipgloss.Style
	badgeBlocker   lipgloss.Style
	badgeComment   lipgloss.Style
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

	return styles{
		title:          lipgloss.NewStyle().Bold(true).Foreground(primary),
		nav:            lipgloss.NewStyle().Foreground(secondary),
		activeNav:      lipgloss.NewStyle().Foreground(primary).Bold(true),
		panel:          lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		commentCard:    lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(taskCommentsPanelWidth - 8),
		commentInput:   lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(taskCommentsPanelWidth - 8).Height(commentInputHeight),
		border:         lipgloss.NewStyle().Foreground(border),
		columnTitle:    lipgloss.NewStyle().Bold(true).Foreground(secondary),
		kanbanColumn:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Width(columnWidth).Padding(0, 0),
		card:           lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
		cardSelected:   lipgloss.NewStyle().Foreground(foreground).Bold(true).Border(lipgloss.RoundedBorder()).BorderForeground(primary).Padding(0, 1).Width(cardBoxWidth),
		entityRow:      lipgloss.NewStyle().Foreground(foreground).Width(entityListWidth),
		entitySelected: lipgloss.NewStyle().Foreground(foreground).Bold(true).Width(entityListWidth),
		marker:         lipgloss.NewStyle().Foreground(primary).Bold(true),
		separator:      lipgloss.NewStyle().Foreground(border),
		empty:          lipgloss.NewStyle().Foreground(border).Width(columnWidth - 4).Align(lipgloss.Center),
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
		badgeHigh:      lipgloss.NewStyle().Background(errorColor).Foreground(lipgloss.Color("#1A1A1A")).Padding(0, 1).Bold(true),
		badgeNormal:    lipgloss.NewStyle().Background(success).Foreground(lipgloss.Color("#1A1A1A")).Padding(0, 1).Bold(true),
		badgeLow:       lipgloss.NewStyle().Background(secondary).Foreground(lipgloss.Color("#1A1A1A")).Padding(0, 1).Bold(true),
		badgeBlocker:   lipgloss.NewStyle().Background(warning).Foreground(lipgloss.Color("#1A1A1A")).Padding(0, 1).Bold(true),
		badgeComment:   lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
	}
}
