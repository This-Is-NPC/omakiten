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
	columnWidth     = 28
	selectionMarker = "▌"
	normalMarker    = " "
)

type Repositories struct {
	Tasks        app.TaskRepository
	Comments     app.CommentRepository
	Dependencies app.DependencyRepository
	Entries      app.ContextEntryRepository
	Config       app.ConfigRepository
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
	detail   bool
	moveMode bool

	tasks        []domain.Task
	workflow     domain.Workflow
	dependencies []domain.TaskDependency
	comments     []domain.Comment
	laws         []domain.Law
	entries      []domain.ContextEntry
	metrics      domain.TokenMetrics
	selected     int
	colIdx       int
	cardIdx      int
}

type inputMode int

const (
	modeNormal inputMode = iota
	modeNewTask
	modeEditTask
	modeComment
	modeMove
)

var viewNames = []string{"Board", "Table", "Graph", "Config"}

func NewModel(ctx context.Context, project domain.ProjectContext, repos Repositories, theme config.Theme, counter token.Counter) (Model, error) {
	if counter == nil {
		counter = token.ApproxCounter{}
	}
	model := Model{ctx: ctx, project: project, repos: repos, theme: theme, styles: newStyles(theme), counter: counter}
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
	case tea.KeyMsg:
		if m.mode != modeNormal {
			return m.updateInput(msg)
		}

		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		if m.handleCommonKey(msg) {
			return m, nil
		}
		if m.view == 0 {
			m.handleBoardKey(msg)
			return m, nil
		}
		m.handleListKey(msg)
	}
	return m, nil
}

func (m Model) View() string {
	if m.mode != modeNormal {
		return strings.Join([]string{m.renderHeader(), m.renderInput(), m.renderCurrentView(), m.renderFooter()}, "\n")
	}

	parts := []string{m.renderHeader()}
	if m.status != "" {
		parts = append(parts, m.styles.status.Render("  "+m.status))
	}
	parts = append(parts, m.renderCurrentView(), m.renderFooter())
	return strings.Join(parts, "\n")
}

func (m *Model) handleCommonKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc":
		if m.detail || m.moveMode {
			m.detail = false
			m.moveMode = false
			m.status = "Closed"
			return true
		}
	case "tab":
		m.view = (m.view + 1) % len(viewNames)
		m.detail = false
		m.moveMode = false
		return true
	case "shift+tab":
		m.view = (m.view + len(viewNames) - 1) % len(viewNames)
		m.detail = false
		m.moveMode = false
		return true
	case "1", "2", "3", "4":
		m.view = int(msg.String()[0] - '1')
		m.detail = false
		m.moveMode = false
		return true
	case "n":
		m.beginInput(modeNewTask, "New task title", "")
		return true
	case "e":
		if task, ok := m.selectedTask(); ok {
			m.beginInput(modeEditTask, "Edit task title", task.Title)
		}
		return true
	case "c":
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
		if _, ok := m.selectedTask(); ok {
			m.detail = !m.detail
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
		if _, ok := m.selectedTask(); ok {
			m.detail = !m.detail
		}
	case "m":
		if _, ok := m.selectedTask(); ok {
			m.beginInput(modeMove, "Target bucket key", "")
		}
	}
}

func (m *Model) beginInput(mode inputMode, status, input string) {
	m.mode = mode
	m.input = input
	m.status = status
	m.detail = false
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

	var err error
	switch m.mode {
	case modeNewTask:
		_, err = app.NewTaskService(m.repos.Tasks).Add(m.ctx, m.project, input, "", "backlog")
	case modeEditTask:
		task, ok := m.selectedTask()
		if !ok {
			err = domain.NewError(domain.ErrTaskNotFound, "no selected task", nil)
			break
		}
		_, err = app.NewTaskService(m.repos.Tasks).Edit(m.ctx, m.project, task.ID, domain.TaskUpdate{Title: &input})
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
		_, err = app.NewTaskService(m.repos.Tasks).Move(m.ctx, m.project, task.ID, input)
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
	m.entries = entries
	m.metrics = m.computeMetrics(settings.MaxTokens)
	m.clampSelection()
	m.clampCardIdx()
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
	for _, comment := range m.comments {
		total += m.counter.Count(comment.Body)
	}
	return domain.TokenMetrics{EstimatedTotal: total, MaxTokens: maxTokens, Truncated: maxTokens > 0 && total > maxTokens}
}

func (m Model) renderHeader() string {
	var sb strings.Builder
	sb.WriteString("\n  Project: ")
	sb.WriteString(m.styles.title.Render(m.project.Slug))
	sb.WriteString("  ")
	sb.WriteString(m.styles.hint.Render("· local checkpoint"))
	sb.WriteString("\n\n  ")

	items := make([]string, 0, len(viewNames))
	for i, name := range viewNames {
		style := m.styles.nav
		if i == m.view {
			style = m.styles.activeNav
		}
		items = append(items, style.Render(fmt.Sprintf("%d %s", i+1, name)))
	}
	sb.WriteString(strings.Join(items, "  "))
	return sb.String()
}

func (m Model) renderInput() string {
	return indentBlock(m.styles.input.Render(fmt.Sprintf("%s: %s", m.status, m.input)), 2)
}

func (m Model) renderCurrentView() string {
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
	columns := make([]string, 0, len(m.workflow.Buckets))
	totalTasks := 0
	for i, bucket := range m.workflow.Buckets {
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if i == m.colIdx {
			selectedIdx = m.cardIdx
		}
		columns = append(columns, m.renderKanbanColumn(bucket, bucketTasks, i == m.colIdx, selectedIdx))
		totalTasks += len(bucketTasks)
	}

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(lipgloss.JoinHorizontal(lipgloss.Top, columns...), 2))
	if totalTasks == 0 {
		sb.WriteString("\n\n")
		sb.WriteString(indentBlock(m.renderEmptyBoardHint(), 2))
	}
	if m.detail {
		if task, ok := m.selectedTask(); ok {
			sb.WriteString("\n\n")
			sb.WriteString(indentBlock(m.renderDetailPanel(task), 2))
		}
	}
	return sb.String()
}

func (m Model) renderKanbanColumn(bucket domain.Bucket, tasks []domain.Task, focused bool, selectedIdx int) string {
	header := truncateText(bucket.Name, columnWidth-7)
	lines := []string{
		m.styles.columnTitle.Render(fmt.Sprintf("%s (%d)", header, len(tasks))),
		m.styles.separator.Render(strings.Repeat("─", columnWidth)),
	}

	if len(tasks) == 0 {
		lines = append(lines, m.styles.empty.Render(centerText("empty", columnWidth)))
	} else {
		for i, task := range tasks {
			lines = append(lines, m.renderCard(task, focused && i == selectedIdx))
		}
	}

	borderStyle := m.styles.border
	if focused {
		borderStyle = m.styles.focusBorder
	}
	return renderFixedBox(lines, columnWidth, borderStyle)
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

func (m Model) renderEmptyBoardHint() string {
	lines := []string{
		m.styles.hintAccent.Render("No tasks yet."),
		"",
		m.styles.hint.Render("Create one with ") + m.styles.hintAccent.Render("n") + m.styles.hint.Render(" or from the CLI:"),
		m.styles.hint.Render("  okt add -t \"Implement the next slice\""),
		"",
		m.styles.hintAccent.Render("m") + m.styles.hint.Render(" move  ") + m.styles.hintAccent.Render("enter") + m.styles.hint.Render(" detail  ") + m.styles.hintAccent.Render("c") + m.styles.hint.Render(" comment"),
	}
	return m.styles.hintBox.Render(strings.Join(lines, "\n"))
}

func (m Model) renderDetailPanel(task domain.Task) string {
	lines := []string{
		"Title:    " + task.Title,
		"Bucket:   " + task.BucketKey,
		"Priority: " + string(task.Priority),
		fmt.Sprintf("Deps:     %d", m.dependencyCount(task.ID)),
		fmt.Sprintf("Comments: %d", m.commentCount(task.ID)),
	}
	if strings.TrimSpace(task.Description) != "" {
		lines = append(lines, "", "Description:", task.Description)
	}
	return m.styles.detail.Render(strings.Join(lines, "\n"))
}

func (m Model) renderTable() string {
	if len(m.tasks) == 0 {
		return "\n" + indentBlock(m.styles.panel.Render("No tasks"), 2)
	}
	rows := []string{
		m.styles.columnTitle.Render("ID   Bucket      Pri      Deps  Comments  Title"),
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
	content := strings.Join(rows, "\n")
	if m.detail {
		if task, ok := m.selectedTask(); ok {
			content += "\n\n" + m.renderDetailPanel(task)
		}
	}
	return "\n" + indentBlock(m.styles.panel.Render(content), 2)
}

func (m Model) renderGraph() string {
	if len(m.dependencies) == 0 {
		content := m.styles.hintBox.Render(m.styles.hint.Render("No task dependencies yet.") + "\n" + m.styles.hint.Render("Use ") + m.styles.hintAccent.Render("okt depend add TASK -i BLOCKER") + m.styles.hint.Render(" to define blocked_by edges."))
		return "\n" + indentBlock(content, 2)
	}
	lines := []string{m.styles.columnTitle.Render("Task dependency graph"), m.styles.separator.Render(strings.Repeat("─", 44))}
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

	lawLines := make([]string, 0, len(m.laws))
	for _, law := range m.laws {
		lawLines = append(lawLines, fmt.Sprintf("  %s [%s]", law.Key, law.Severity))
	}
	if len(lawLines) == 0 {
		lawLines = append(lawLines, "  none")
	}

	left := []string{
		m.styles.columnTitle.Render("Runtime"),
		fmt.Sprintf("Workflow: %s", m.workflow.Key),
		fmt.Sprintf("Buckets:  %s", strings.Join(bucketKeys, ", ")),
		fmt.Sprintf("Theme:    %s", m.theme.Key),
		"",
		m.styles.columnTitle.Render("Totals"),
		fmt.Sprintf("Tasks:    %d", len(m.tasks)),
		fmt.Sprintf("Comments: %d", len(m.comments)),
		fmt.Sprintf("Context:  %d", len(m.entries)),
	}
	right := []string{
		m.styles.columnTitle.Render("Token budget"),
		fmt.Sprintf("Estimated: %d", m.metrics.EstimatedTotal),
		fmt.Sprintf("Max:       %d", m.metrics.MaxTokens),
	}
	if m.metrics.Truncated {
		right = append(right, m.styles.error.Render("Budget exceeded"))
	}
	right = append(right, "", m.styles.columnTitle.Render("Laws"))
	right = append(right, lawLines...)

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.styles.configColumn.Render(strings.Join(left, "\n")),
		m.styles.configColumn.Render(strings.Join(right, "\n")),
	)
	return "\n" + indentBlock(content, 2)
}

func (m Model) renderFooter() string {
	var text string
	switch {
	case m.mode != modeNormal:
		text = "enter: save  esc: cancel  ctrl+c: quit"
	case m.detail:
		text = "esc: close detail  tab: switch view  q: quit"
	case m.moveMode:
		text = "left/right: move task to column  esc: cancel  q: quit"
	case m.view == 0:
		text = "left/right: columns  up/down: cards  enter: detail  m: move  n/e/c: change  q: quit"
	default:
		text = "tab: switch view  up/down: select  enter: detail  m: move  n/e/c: change  q: quit"
	}
	return "\n" + indentBlock(m.styles.footer.Render(text), 2)
}

func (m Model) selectedTask() (domain.Task, bool) {
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
	count := 0
	for _, comment := range m.comments {
		if comment.TaskID == taskID {
			count++
		}
	}
	return count
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
	rows := []string{border.Render("╭" + strings.Repeat("─", width) + "╮")}
	for _, line := range lines {
		rows = append(rows, border.Render("│")+padStyledLine(line, width)+border.Render("│"))
	}
	rows = append(rows, border.Render("╰"+strings.Repeat("─", width)+"╯"))
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

type styles struct {
	title         lipgloss.Style
	nav           lipgloss.Style
	activeNav     lipgloss.Style
	panel         lipgloss.Style
	column        lipgloss.Style
	focusedColumn lipgloss.Style
	border        lipgloss.Style
	focusBorder   lipgloss.Style
	columnTitle   lipgloss.Style
	card          lipgloss.Style
	marker        lipgloss.Style
	separator     lipgloss.Style
	empty         lipgloss.Style
	status        lipgloss.Style
	input         lipgloss.Style
	footer        lipgloss.Style
	hint          lipgloss.Style
	hintAccent    lipgloss.Style
	hintBox       lipgloss.Style
	detail        lipgloss.Style
	configColumn  lipgloss.Style
	muted         lipgloss.Style
	success       lipgloss.Style
	warning       lipgloss.Style
	error         lipgloss.Style
}

func newStyles(theme config.Theme) styles {
	color := func(key, fallback string) lipgloss.Color {
		if value := theme.Colors[key]; value != "" {
			return lipgloss.Color(value)
		}
		return lipgloss.Color(fallback)
	}

	border := color("border", "#494D64")
	foreground := color("foreground", "#CAD3F5")
	background := color("background", "#24273A")
	primary := color("primary", "#8AADF4")
	secondary := color("secondary", "#C6A0F6")
	success := color("success", "#A6DA95")
	warning := color("warning", "#EED49F")
	errorColor := color("error", "#ED8796")

	return styles{
		title:         lipgloss.NewStyle().Bold(true).Foreground(primary),
		nav:           lipgloss.NewStyle().Foreground(foreground),
		activeNav:     lipgloss.NewStyle().Foreground(background).Background(primary).Bold(true).Padding(0, 1),
		panel:         lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 2),
		column:        lipgloss.NewStyle().Foreground(foreground),
		focusedColumn: lipgloss.NewStyle().Foreground(foreground),
		border:        lipgloss.NewStyle().Foreground(border),
		focusBorder:   lipgloss.NewStyle().Foreground(primary),
		columnTitle:   lipgloss.NewStyle().Bold(true).Foreground(foreground),
		card:          lipgloss.NewStyle().Width(columnWidth),
		marker:        lipgloss.NewStyle().Foreground(primary).Bold(true),
		separator:     lipgloss.NewStyle().Foreground(border),
		empty:         lipgloss.NewStyle().Foreground(border).Italic(true).Width(columnWidth - 4).Align(lipgloss.Center),
		status:        lipgloss.NewStyle().Foreground(secondary).Italic(true),
		input:         lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 2),
		footer:        lipgloss.NewStyle().Foreground(border).Italic(true),
		hint:          lipgloss.NewStyle().Foreground(border).Italic(true),
		hintAccent:    lipgloss.NewStyle().Foreground(primary).Bold(true),
		hintBox:       lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2).Width(60),
		detail:        lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.RoundedBorder()).BorderForeground(primary).Padding(0, 2).Width(54),
		configColumn:  lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1, 2).MarginRight(1).Width(40),
		muted:         lipgloss.NewStyle().Foreground(border),
		success:       lipgloss.NewStyle().Foreground(success),
		warning:       lipgloss.NewStyle().Foreground(warning),
		error:         lipgloss.NewStyle().Foreground(errorColor),
	}
}
