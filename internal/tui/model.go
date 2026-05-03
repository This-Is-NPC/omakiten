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

	width  int
	height int
	view   int
	mode   inputMode
	input  string
	status string

	tasks        []domain.Task
	workflow     domain.Workflow
	dependencies []domain.TaskDependency
	comments     []domain.Comment
	laws         []domain.Law
	entries      []domain.ContextEntry
	metrics      domain.TokenMetrics
	selected     int
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

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab", "right", "l":
			m.view = (m.view + 1) % len(viewNames)
		case "shift+tab", "left", "h":
			m.view = (m.view + len(viewNames) - 1) % len(viewNames)
		case "1":
			m.view = 0
		case "2":
			m.view = 1
		case "3":
			m.view = 2
		case "4":
			m.view = 3
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.tasks)-1 {
				m.selected++
			}
		case "n":
			m.mode = modeNewTask
			m.input = ""
			m.status = "New task title"
		case "e":
			if task, ok := m.selectedTask(); ok {
				m.mode = modeEditTask
				m.input = task.Title
				m.status = "Edit task title"
			}
		case "c":
			if _, ok := m.selectedTask(); ok {
				m.mode = modeComment
				m.input = ""
				m.status = "Comment body"
			}
		case "m":
			if _, ok := m.selectedTask(); ok {
				m.mode = modeMove
				m.input = ""
				m.status = "Target bucket key"
			}
		case "r":
			if err := m.refresh(); err != nil {
				m.status = err.Error()
			} else {
				m.status = "Refreshed"
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	parts := []string{m.renderHeader()}
	if m.status != "" {
		parts = append(parts, m.styles.status.Render(m.status))
	}
	if m.mode != modeNormal {
		parts = append(parts, m.renderInput())
	}

	switch m.view {
	case 0:
		parts = append(parts, m.renderBoard())
	case 1:
		parts = append(parts, m.renderTable())
	case 2:
		parts = append(parts, m.renderGraph())
	case 3:
		parts = append(parts, m.renderConfig())
	}

	parts = append(parts, m.styles.help.Render("tab switch | j/k select | n new | e edit | c comment | m move | r refresh | q quit"))
	return strings.Join(parts, "\n")
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
	if m.selected >= len(m.tasks) {
		m.selected = len(m.tasks) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
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
	tabs := make([]string, 0, len(viewNames))
	for i, name := range viewNames {
		style := m.styles.tab
		if i == m.view {
			style = m.styles.activeTab
		}
		tabs = append(tabs, style.Render(fmt.Sprintf("%d %s", i+1, name)))
	}
	title := m.styles.title.Render(fmt.Sprintf("Omakiten | %s", m.project.Slug))
	return title + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m Model) renderInput() string {
	return m.styles.input.Render(fmt.Sprintf("%s: %s", m.status, m.input))
}

func (m Model) renderBoard() string {
	if len(m.workflow.Buckets) == 0 {
		return m.styles.panel.Render("No workflow buckets")
	}
	tasksByBucket := map[string][]domain.Task{}
	for _, task := range m.tasks {
		tasksByBucket[task.BucketKey] = append(tasksByBucket[task.BucketKey], task)
	}

	columns := make([]string, 0, len(m.workflow.Buckets))
	for _, bucket := range m.workflow.Buckets {
		lines := []string{m.styles.columnTitle.Render(bucket.Name)}
		for _, task := range tasksByBucket[bucket.Key] {
			line := fmt.Sprintf("#%d %s", task.ID, task.Title)
			if m.isSelected(task.ID) {
				line = m.styles.selected.Render("> " + line)
			} else {
				line = "  " + line
			}
			lines = append(lines, line)
		}
		columns = append(columns, m.styles.column.Render(strings.Join(lines, "\n")))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, columns...)
}

func (m Model) renderTable() string {
	if len(m.tasks) == 0 {
		return m.styles.panel.Render("No tasks")
	}
	rows := []string{m.styles.columnTitle.Render("ID   Bucket     Pri     Deps  Title")}
	for i, task := range m.tasks {
		prefix := " "
		if i == m.selected {
			prefix = ">"
		}
		row := fmt.Sprintf("%s %-4d %-10s %-7s %-5d %s", prefix, task.ID, task.BucketKey, task.Priority, m.dependencyCount(task.ID), task.Title)
		if i == m.selected {
			row = m.styles.selected.Render(row)
		}
		rows = append(rows, row)
	}
	return m.styles.panel.Render(strings.Join(rows, "\n"))
}

func (m Model) renderGraph() string {
	if len(m.dependencies) == 0 {
		return m.styles.panel.Render("No task dependencies")
	}
	lines := []string{m.styles.columnTitle.Render("Task dependency graph")}
	for _, dependency := range m.dependencies {
		lines = append(lines, fmt.Sprintf("#%d blocked_by #%d", dependency.TaskID, dependency.DependsOnTaskID))
	}
	return m.styles.panel.Render(strings.Join(lines, "\n"))
}

func (m Model) renderConfig() string {
	lawLines := make([]string, 0, len(m.laws))
	for _, law := range m.laws {
		lawLines = append(lawLines, fmt.Sprintf("- %s [%s]", law.Key, law.Severity))
	}
	if len(lawLines) == 0 {
		lawLines = append(lawLines, "- none")
	}

	bucketKeys := make([]string, 0, len(m.workflow.Buckets))
	for _, bucket := range m.workflow.Buckets {
		bucketKeys = append(bucketKeys, bucket.Key)
	}
	sort.Strings(bucketKeys)

	lines := []string{
		m.styles.columnTitle.Render("Configuration"),
		fmt.Sprintf("Workflow: %s", m.workflow.Key),
		fmt.Sprintf("Buckets: %s", strings.Join(bucketKeys, ", ")),
		fmt.Sprintf("Theme: %s", m.theme.Key),
		fmt.Sprintf("Tasks: %d", len(m.tasks)),
		fmt.Sprintf("Comments: %d", len(m.comments)),
		fmt.Sprintf("Context entries: %d", len(m.entries)),
		fmt.Sprintf("Tokens: %d / %d", m.metrics.EstimatedTotal, m.metrics.MaxTokens),
		"Laws:",
		strings.Join(lawLines, "\n"),
	}
	if m.metrics.Truncated {
		lines = append(lines, m.styles.error.Render("Token budget exceeded"))
	}
	return m.styles.panel.Render(strings.Join(lines, "\n"))
}

func (m Model) selectedTask() (domain.Task, bool) {
	if m.selected < 0 || m.selected >= len(m.tasks) {
		return domain.Task{}, false
	}
	return m.tasks[m.selected], true
}

func (m Model) isSelected(taskID int64) bool {
	task, ok := m.selectedTask()
	return ok && task.ID == taskID
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

type styles struct {
	title       lipgloss.Style
	tab         lipgloss.Style
	activeTab   lipgloss.Style
	panel       lipgloss.Style
	column      lipgloss.Style
	columnTitle lipgloss.Style
	selected    lipgloss.Style
	status      lipgloss.Style
	input       lipgloss.Style
	help        lipgloss.Style
	error       lipgloss.Style
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
	highlight := color("highlight", "#363A4F")
	errorColor := color("error", "#ED8796")

	return styles{
		title:       lipgloss.NewStyle().Bold(true).Foreground(primary).MarginBottom(1),
		tab:         lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		activeTab:   lipgloss.NewStyle().Foreground(background).Background(primary).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1),
		panel:       lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1, 2).MarginTop(1),
		column:      lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(1, 2).MarginRight(1).Width(28),
		columnTitle: lipgloss.NewStyle().Bold(true).Foreground(secondary),
		selected:    lipgloss.NewStyle().Background(highlight).Foreground(foreground),
		status:      lipgloss.NewStyle().Foreground(secondary).MarginTop(1),
		input:       lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1),
		help:        lipgloss.NewStyle().Foreground(border).MarginTop(1),
		error:       lipgloss.NewStyle().Foreground(errorColor),
	}
}
