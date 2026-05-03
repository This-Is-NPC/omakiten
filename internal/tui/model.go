package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
)

type Repositories struct {
	Tasks        app.TaskRepository
	Comments     app.CommentRepository
	Dependencies app.DependencyRepository
	Entries      app.ContextEntryRepository
	Config       app.ConfigRepository
	Editor       *app.BundleEditor
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

	taskScreen      taskScreenMode
	taskID          int64
	taskTitle       string
	taskDescription string
	taskField       taskFormField

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
)

var viewNames = []string{"BOARD", "TABLE", "GRAPH", "CONFIG"}

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
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case editorFinishedMsg:
		m.handleEditorFinished(msg)
		return m, nil
	case tea.KeyMsg:
		if m.helpOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "?", "esc", "q":
				m.helpOpen = false
			}
			return m, nil
		}
		if msg.String() == "?" && m.mode == modeNormal {
			m.helpOpen = true
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
			return m, nil
		}
		switch m.view {
		case 0:
			m.handleBoardKey(msg)
		case 3:
			if cmd := m.handleConfigKey(msg); cmd != nil {
				return m, cmd
			}
		default:
			m.handleListKey(msg)
		}
	}
	return m, nil
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
	case "1", "2", "3", "4":
		m.view = int(msg.String()[0] - '1')
		m.moveMode = false
		return true
	case "n":
		if m.view == 3 {
			return false
		}
		m.openTaskCreate()
		return true
	case "e":
		if m.view == 3 {
			return false
		}
		if task, ok := m.selectedTask(); ok {
			m.openTaskEdit(task)
		}
		return true
	case "c":
		if m.view == 3 {
			return false
		}
		if _, ok := m.selectedTask(); ok {
			m.beginInput(modeComment, "Comment body", "")
		}
		return true
	case "r":
		if err := m.refresh(); err != nil {
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

func (m *Model) updateTaskScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case isTaskFormNewline(msg):
		m.insertTaskFormNewline()
	case msg.String() == "backspace" || msg.String() == "ctrl+h":
		m.deleteTaskFormRune()
	default:
		if len(msg.Runes) > 0 {
			m.appendTaskFormText(string(msg.Runes))
		} else if msg.String() == " " {
			m.appendTaskFormText(" ")
		}
	}
	return *m, nil
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
	m.taskField = taskFieldTitle
	m.status = "Editing task"
	m.moveMode = false
}

func (m *Model) closeTaskScreen(status string) {
	m.taskScreen = taskScreenClosed
	m.taskID = 0
	m.taskTitle = ""
	m.taskDescription = ""
	m.taskField = taskFieldTitle
	m.status = status
	m.moveMode = false
}

func (m *Model) toggleTaskField() {
	if m.taskField == taskFieldTitle {
		m.taskField = taskFieldDescription
	} else {
		m.taskField = taskFieldTitle
	}
}

func (m *Model) appendTaskFormText(text string) {
	if m.taskField == taskFieldTitle {
		m.taskTitle += text
		return
	}
	m.taskDescription += text
}

func (m *Model) insertTaskFormNewline() {
	if m.taskField == taskFieldTitle {
		m.taskField = taskFieldDescription
		return
	}
	m.taskDescription += "\n"
}

func (m *Model) deleteTaskFormRune() {
	if m.taskField == taskFieldTitle {
		m.taskTitle = trimLastRune(m.taskTitle)
		return
	}
	m.taskDescription = trimLastRune(m.taskDescription)
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
		task, err = app.NewTaskService(m.repos.Tasks).Add(m.ctx, m.project, title, description, "backlog")
	case taskScreenEdit:
		current, ok := m.activeTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		task, err = app.NewTaskService(m.repos.Tasks).Edit(m.ctx, m.project, current.ID, domain.TaskUpdate{Title: &title, Description: &description})
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
	default:
		return ""
	}
}

func (m Model) renderBoard() string {
	if len(m.workflow.Buckets) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No workflow buckets"), 2)
	}

	tasksByBucket := m.tasksByBucket()
	cells := make([]string, 0, len(m.workflow.Buckets))
	widths := make([]int, 0, len(m.workflow.Buckets))
	totalTasks := 0
	for i, bucket := range m.workflow.Buckets {
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if i == m.colIdx {
			selectedIdx = m.cardIdx
		}
		cells = append(cells, m.renderKanbanCell(bucket, bucketTasks, i == m.colIdx, selectedIdx))
		widths = append(widths, columnWidth)
		totalTasks += len(bucketTasks)
	}

	grid := renderRowGrid(cells, widths, m.styles.border)

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(grid, 2))
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
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(truncateText(bucket.Name, columnWidth-10)), len(tasks))
	lines := []string{
		headerStyle.Render(headerText),
		m.styles.separator.Render(strings.Repeat("─", columnWidth)),
	}

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
	indicator := m.taskIndicator(task).Render("●")
	prefix := fmt.Sprintf("%s %s #%d ", marker, indicator, task.ID)
	titleBudget := columnWidth - lipgloss.Width(prefix)
	title := truncateText(task.Title, titleBudget)
	line := prefix + title
	return m.styles.card.Render(line)
}

func (m Model) renderHelp() string {
	type binding struct{ key, desc string }
	type group struct {
		title    string
		bindings []binding
	}
	groups := []group{
		{"Global", []binding{
			{"?", "toggle this overlay"},
			{"q · ctrl+c", "quit"},
			{"tab · shift+tab", "cycle views"},
			{"1 · 2 · 3 · 4", "jump to view"},
			{"r", "refresh"},
		}},
		{"Board", []binding{
			{"← ↑ ↓ → · h j k l", "navigate columns and cards"},
			{"enter", "open task"},
			{"n", "new task"},
			{"e", "edit task"},
			{"c", "add comment"},
			{"m", "toggle move mode"},
		}},
		{"Task view", []binding{
			{"e", "edit"},
			{"c", "add comment"},
			{"m", "move"},
			{"esc", "back to board"},
		}},
		{"Task form", []binding{
			{"tab", "switch field"},
			{"enter · alt+enter · shift+enter", "newline in description"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
		{"Config", []binding{
			{"← →", "switch entity kind"},
			{"↑ ↓", "select entity"},
			{"enter", "open detail"},
			{"n · e · d", "new · edit · delete"},
			{"p", "skill picker (persona)"},
		}},
		{"Entity view", []binding{
			{"e", "edit (opens $EDITOR)"},
			{"d", "delete"},
			{"p", "skill picker (persona)"},
			{"esc", "back"},
		}},
		{"Skill picker", []binding{
			{"↑ ↓", "move"},
			{"space", "toggle"},
			{"enter on '+ create new'", "scaffold new skill"},
			{"ctrl+s", "save"},
			{"esc", "cancel"},
		}},
	}

	const keyW = 36
	var lines []string
	lines = append(lines, m.styles.kicker("Keybindings"), "")
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
	return indentBlock(m.styles.footer.Render("? · esc · q   close help"), 2)
}

func (m Model) renderEmptyBoardHint() string {
	lines := []string{
		m.styles.hintAccent.Render("No tasks yet."),
		"",
		m.styles.hint.Render("Create one with ") + m.styles.hintAccent.Render("n") + m.styles.hint.Render(" or from the CLI:"),
		m.styles.hint.Render("  okt add -t \"Implement the next slice\""),
		"",
		m.styles.hintAccent.Render("m") + m.styles.hint.Render(" move  ") + m.styles.hintAccent.Render("enter") + m.styles.hint.Render(" open  ") + m.styles.hintAccent.Render("c") + m.styles.hint.Render(" comment"),
	}
	return m.styles.hintBox.Render(strings.Join(lines, "\n"))
}

func (m Model) renderTaskScreen() string {
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
		return "\n" + indentBlock(m.styles.panel.Render("Task not found"), 2)
	}

	detailLines := []string{
		m.styles.kicker(fmt.Sprintf("Task · #%d", task.ID)),
		"",
		m.styles.metaRow("Title", task.Title, 14),
		m.styles.metaRow("Bucket", task.BucketKey, 14),
		m.styles.metaRow("Priority", string(task.Priority), 14),
		m.styles.metaRow("Blockers", fmt.Sprintf("%d", m.dependencyCount(task.ID)), 14),
		m.styles.metaRow("Comments", fmt.Sprintf("%d", m.commentCount(task.ID)), 14),
		"",
		m.styles.kicker("Description"),
	}
	if strings.TrimSpace(task.Description) == "" {
		detailLines = append(detailLines, m.styles.hint.Render("No description"))
	} else {
		detailLines = append(detailLines, task.Description)
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
	lines = append(lines, m.styles.commentInput.Render(m.input))
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
		m.styles.hint.Render("ctrl+s saves. enter/alt+enter/shift+enter adds lines in description."),
		"",
		m.renderTaskFormLabel(taskFieldTitle, "Title"),
		m.styles.input.Width(taskFormInputWidth).Render(m.taskTitle),
		"",
		m.renderTaskFormLabel(taskFieldDescription, "Description"),
		m.renderTaskDescriptionInput(),
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(lines, "\n")), 2)
}

func (m Model) renderTaskDescriptionInput() string {
	return m.styles.multilineInput.Render(m.taskDescription)
}

func (m Model) renderTaskFormLabel(field taskFormField, label string) string {
	marker := " "
	if m.taskField == field {
		marker = ">"
	}
	return m.styles.hintAccent.Render(marker+" // "+strings.ToUpper(label))
}

func (m Model) renderTable() string {
	if len(m.tasks) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No tasks"), 2)
	}
	rows := []string{
		m.styles.hintAccent.Render("// ID   BUCKET      PRI      DEPS  COMMENTS  TITLE"),
		m.styles.separator.Render(strings.Repeat("─", 70)),
	}
	for i, task := range m.tasks {
		marker := normalMarker
		if i == m.selected {
			marker = m.styles.marker.Render(selectionMarker)
		}
		row := fmt.Sprintf("%s %-4d %-11s %-8s %-5d %-9d %s", marker, task.ID, task.BucketKey, task.Priority, m.dependencyCount(task.ID), m.commentCount(task.ID), truncateText(task.Title, 32))
		rows = append(rows, row)
	}
	return "\n" + indentBlock(m.styles.panel.Render(strings.Join(rows, "\n")), 2)
}

func (m Model) renderGraph() string {
	if len(m.dependencies) == 0 {
		content := m.styles.hintBox.Render(m.styles.hint.Render("No task dependencies yet.") + "\n" + m.styles.hint.Render("Use ") + m.styles.hintAccent.Render("okt depend add TASK -i BLOCKER") + m.styles.hint.Render(" to define blocked_by edges."))
		return "\n" + indentBlock(content, 2)
	}
	lines := []string{m.styles.kicker("Task dependency graph"), m.styles.separator.Render(strings.Repeat("─", 44))}
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
	header := renderRowGrid(
		[]string{strings.Join(left, "\n"), strings.Join(right, "\n")},
		[]int{configCellWidth, configCellWidth},
		m.styles.border,
	)

	lists := renderRowGrid(
		[]string{
			m.renderEntityCell(entityKindLaw),
			m.renderEntityCell(entityKindPersona),
			m.renderEntityCell(entityKindSkill),
		},
		[]int{entityListWidth, entityListWidth, entityListWidth},
		m.styles.border,
	)

	return "\n" + indentBlock(header+"\n\n"+lists, 2)
}

func (m Model) renderFooter() string {
	var text string
	switch {
	case m.isEmbeddedCommentInput():
		text = "enter: add comment  alt+enter/shift+enter: newline  esc: cancel"
	case m.mode != modeNormal:
		text = "enter: save  esc: cancel  ctrl+c: quit"
	case m.taskScreen == taskScreenView:
		text = "e: edit  c: comment  m: move  r: refresh  esc: board  q: quit"
	case m.taskScreen == taskScreenCreate:
		text = "tab: switch field  enter/alt+enter: newline  ctrl+s: create  esc: cancel"
	case m.taskScreen == taskScreenEdit:
		text = "tab: switch field  enter/alt+enter: newline  ctrl+s: save  esc: view"
	case m.entityScreen == entityScreenView:
		text = "e: edit (opens $EDITOR)  d: delete  p: skill picker (persona)  r: refresh  esc: config  q: quit"
	case m.entityScreen == entityScreenSkillPicker:
		text = "up/down: move  space: toggle  enter on '+': new skill  ctrl+s: save  esc: cancel"
	case m.moveMode:
		text = "left/right: move task to column  esc: cancel  q: quit"
	case m.view == 0:
		text = "left/right: columns  up/down: cards  enter: open  m: move  n/e/c: change  ?: help  q: quit"
	case m.view == 3:
		text = "left/right: section  up/down: select  enter: open  n: new  e: edit  d: delete  ?: help  q: quit"
	default:
		text = "tab: switch view  up/down: select  enter: open  m: move  n/e/c: change  ?: help  q: quit"
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
	if max <= 0 || len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
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
		lines := strings.Split(cell, "\n")
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

func padStyledLine(line string, width int) string {
	visible := lipgloss.Width(line)
	if visible >= width {
		return line
	}
	return line + strings.Repeat(" ", width-visible)
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

// kicker renders a section label in dev-editorial style: `// LABEL` in the
// accent color. Label is uppercased; pass it in any case.
func (s styles) kicker(label string) string {
	return s.hintAccent.Render("// " + strings.ToUpper(label))
}

// kickerCount renders `// LABEL · N` — kicker with a trailing count.
func (s styles) kickerCount(label string, count int) string {
	return s.hintAccent.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(label), count))
}

// metaRow renders a definition-list row: `// LABEL` (kicker) + value, the label
// padded to labelWidth so values align across multiple rows.
func (s styles) metaRow(label, value string, labelWidth int) string {
	rendered := "// " + strings.ToUpper(label)
	pad := labelWidth - lipgloss.Width(rendered)
	if pad < 1 {
		pad = 1
	}
	return s.hintAccent.Render(rendered) + strings.Repeat(" ", pad) + value
}

// statusBadge renders a status message as `[INFO] msg` or `[ERROR] msg` based
// on a content heuristic. Replaces italic-on-secondary status rendering.
func (s styles) statusBadge(msg string) string {
	if msg == "" {
		return ""
	}
	level := "INFO"
	tagStyle := s.hintAccent
	lower := strings.ToLower(msg)
	for _, needle := range []string{"error", "fail", "not found", "required", "cancel", "missing", "nothing", "invalid", "exceeded"} {
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
	success        lipgloss.Style
	warning        lipgloss.Style
	error          lipgloss.Style
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
	success := color("success", "#39FF14")
	warning := color("warning", "#FFB347")
	errorColor := color("error", "#FF5544")

	return styles{
		title:          lipgloss.NewStyle().Bold(true).Foreground(primary),
		nav:            lipgloss.NewStyle().Foreground(foreground),
		activeNav:      lipgloss.NewStyle().Foreground(primary).Bold(true),
		panel:          lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		commentCard:    lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(taskCommentsPanelWidth - 8),
		commentInput:   lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(taskCommentsPanelWidth - 8).Height(commentInputHeight),
		border:         lipgloss.NewStyle().Foreground(border),
		columnTitle:    lipgloss.NewStyle().Bold(true).Foreground(foreground),
		card:           lipgloss.NewStyle().Width(columnWidth),
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
		success:        lipgloss.NewStyle().Foreground(success),
		warning:        lipgloss.NewStyle().Foreground(warning),
		error:          lipgloss.NewStyle().Foreground(errorColor),
	}
}
