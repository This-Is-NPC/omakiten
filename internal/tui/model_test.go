package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

func TestModelSwitchesViews(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got := updated.(Model)
	if got.top != topStats || got.sub != subStatsGeneral {
		t.Fatalf("(top, sub) = (%d, %d), want (topStats, subStatsGeneral)", got.top, got.sub)
	}
}

func TestModelTableAndGraphShowCounts(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	blocker, err := store.CreateTask(ctx, project.ID, "Blocker", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(blocker) error = %v", err)
	}
	blocked, err := store.CreateTask(ctx, project.ID, "Blocked", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(blocked) error = %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, blocked.ID, blocker.ID); err != nil {
		t.Fatalf("AddTaskDependency() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	table := ansi.Strip(pressStringKey(t, model, "/").View())
	if !strings.Contains(table, "// TASKS · 2") {
		t.Fatalf("table missing task count\n%s", table)
	}

	graphModel := pressStringKey(t, pressStringKey(t, model, "/"), "/")
	graph := ansi.Strip(graphModel.View())
	if !strings.Contains(graph, "// DEPENDENCY GRAPH · 1") {
		t.Fatalf("graph missing dependency count\n%s", graph)
	}
}

func TestModelTablesUseWideTerminalSpace(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	longTitle := "Investigate viewport usage for the TUI table without truncating the task title"
	if _, err := store.CreateTask(ctx, project.ID, longTitle, "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	argsJSON := `{"root":"/home/howl/Projects/person/omakiten","view":"logs","expanded":true}`
	logID, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceCLI,
		Entrypoint:    "init",
		Operation:     "app.ProjectService.Init",
		ProjectID:     project.ID,
		ProjectSlug:   project.Slug,
		ArgumentsJSON: argsJSON,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog() error = %v", err)
	}
	if err := store.FinishActivityLog(ctx, logID, "ok", 12, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 180
	// The Stats › Logs view now stacks two narrow summary grid tables on
	// top of the wide activity panel, so the *first* ┌...┐ line belongs
	// to the summary, not the panel. Walk every match and take the widest
	// — that is the panel itself.
	panelWidth := func(view string) int {
		widest := 0
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "┌") && strings.Contains(line, "┐") {
				if w := lipgloss.Width(line); w > widest {
					widest = w
				}
			}
		}
		return widest
	}

	tableModel := pressStringKey(t, model, "/")
	table := ansi.Strip(tableModel.View())
	if !strings.Contains(table, longTitle) {
		t.Fatalf("wide table truncated task title\n%s", table)
	}

	logsModel := pressStringKey(t, pressRune(t, model, '2'), "/")
	logs := ansi.Strip(logsModel.View())
	if !strings.Contains(logs, argsJSON) {
		t.Fatalf("wide logs truncated arguments\n%s", logs)
	}
	if !strings.Contains(logs, "// ACTIVITY · 1") {
		t.Fatalf("wide logs missing table-style section label\n%s", logs)
	}
	if tablePanelWidth, logsPanelWidth := panelWidth(table), panelWidth(logs); tablePanelWidth == 0 || logsPanelWidth == 0 || tablePanelWidth != logsPanelWidth {
		t.Fatalf("table/log panel widths = %d/%d, want matching non-zero widths\nTABLE:\n%s\nLOGS:\n%s", tablePanelWidth, logsPanelWidth, table, logs)
	}
}

func TestModelLoadsActivityLogsWhenOpeningLogsView(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	logID, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceCLI,
		Entrypoint:    "add",
		Operation:     "app.TaskService.Add",
		ProjectID:     project.ID,
		ProjectSlug:   project.Slug,
		ArgumentsJSON: `{"title":"From CLI"}`,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog() error = %v", err)
	}
	if err := store.FinishActivityLog(ctx, logID, "ok", 12, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressRune(t, model, '2')
	got = pressRune(t, got, '/')
	if got.top != topStats || got.sub != subStatsLogs {
		t.Fatalf("(top, sub) = (%d, %d), want (topStats, subStatsLogs)", got.top, got.sub)
	}
	view := ansi.Strip(got.View())
	for _, want := range []string{"app.TaskService.Add", "project", `{"title":"From CLI"}`, "ok"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelRefreshKeyUpdatesActivityLogs(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressRune(t, model, '2')
	got = pressRune(t, got, '/')
	if strings.Contains(ansi.Strip(got.View()), "app.CommentService.Add") {
		t.Fatalf("logs view unexpectedly contains new log before refresh\n%s", ansi.Strip(got.View()))
	}
	logID, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceMCP,
		Entrypoint:    "tools/call",
		Operation:     "app.CommentService.Add",
		ProjectID:     project.ID,
		ProjectSlug:   project.Slug,
		ArgumentsJSON: `{"task_id":1}`,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog() error = %v", err)
	}
	if err := store.FinishActivityLog(ctx, logID, "ok", 7, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}

	got = pressRune(t, got, 'r')
	view := ansi.Strip(got.View())
	for _, want := range []string{"app.CommentService.Add", "mcp", `{"task_id":1}`} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q after refresh\n%s", want, view)
		}
	}
}

func TestModelRealtimeTickRefreshesBoardTasks(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if cmd := model.Init(); cmd == nil {
		t.Fatal("Init() command = nil, want realtime refresh tick")
	}
	if len(model.tasks) != 0 {
		t.Fatalf("initial tasks len = %d, want 0", len(model.tasks))
	}
	if _, err := store.CreateTask(ctx, project.ID, "External task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	updated, cmd := model.Update(refreshTickMsg{})
	if cmd == nil {
		t.Fatal("Update(refreshTickMsg) command = nil, want next realtime tick")
	}
	got := updated.(Model)
	if len(got.tasks) != 1 || got.tasks[0].Title != "External task" {
		t.Fatalf("tasks after realtime tick = %#v, want external task", got.tasks)
	}
	if !strings.Contains(ansi.Strip(got.View()), "External task") {
		t.Fatalf("board view missing external task\n%s", ansi.Strip(got.View()))
	}
}

func TestModelOpensExistingTaskScreen(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Existing task", "First line\nSecond line", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	humanComment, err := store.AddComment(ctx, project.ID, task.ID, "Looks good to me.", "human", nil)
	if err != nil {
		t.Fatalf("AddComment(human) error = %v", err)
	}
	agentComment, err := store.AddComment(ctx, project.ID, task.ID, "I can take the next step.", "agent", nil)
	if err != nil {
		t.Fatalf("AddComment(agent) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenView)
	}
	if got.taskID != task.ID {
		t.Fatalf("taskID = %d, want %d", got.taskID, task.ID)
	}
	view := got.View()
	for _, hidden := range []string{"01 // BOARD", "02 // TABLE", "03 // GRAPH", "04 // CONFIG"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("View() contains hidden task-screen tab %q\n%s", hidden, view)
		}
	}
	for _, want := range []string{
		"▸ TASK · #",
		"// TITLE",
		"Existing task",
		"// DESCRIPTION",
		"First line",
		"Second line",
		"// ACTIVITY · 2",
		"human",
		humanComment.CreatedAt,
		"Looks good to me.",
		"agent",
		agentComment.CreatedAt,
		"I can take the next step.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelAddsMultilineCommentInsideTaskCommentsPanel(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Existing task", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'c')
	if !got.isEmbeddedCommentInput() {
		t.Fatalf("isEmbeddedCommentInput() = false, want true")
	}
	view := got.View()
	for _, want := range []string{"// ACTIVITY · 0", "// NEW COMMENT", "enter saves", "alt+enter/shift+enter", "newline"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "Comment body:") {
		t.Fatalf("View() contains global comment input\n%s", view)
	}

	got = sendText(t, got, "First line")
	got = pressAltKey(t, got, tea.KeyEnter)
	got = sendText(t, got, "Second line")
	got = pressStringKey(t, got, "shift+enter")
	got = sendText(t, got, "Third line")
	got = pressKey(t, got, tea.KeyEnter)

	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	comments, err := store.ListComments(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("ListComments() len = %d, want 1", len(comments))
	}
	wantBody := "First line\nSecond line\nThird line"
	if comments[0].Body != wantBody {
		t.Fatalf("comment body = %q, want %q", comments[0].Body, wantBody)
	}
	view = got.View()
	for _, want := range []string{"// ACTIVITY · 1", "human", comments[0].CreatedAt, "First line", "Second line", "Third line"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelCreatesTaskFromDedicatedScreen(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := sendText(t, pressRune(t, model, 'n'), "Created from TUI")
	if got.taskScreen != taskScreenCreate {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenCreate)
	}
	count, err := store.TaskCount(ctx, project.ID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("TaskCount() = %d, want 0 before save", count)
	}

	got = pressKey(t, got, tea.KeyTab)
	descriptionInput := got.renderTaskDescriptionInput()
	if lipgloss.Width(descriptionInput) < taskFormInputWidth || lipgloss.Height(descriptionInput) < taskDescriptionInputHeight {
		t.Fatalf("description input size = %dx%d, want at least %dx%d", lipgloss.Width(descriptionInput), lipgloss.Height(descriptionInput), taskFormInputWidth, taskDescriptionInputHeight)
	}
	got = sendText(t, got, "First line")
	got = pressAltKey(t, got, tea.KeyEnter)
	got = sendText(t, got, "Second line")
	got = pressKey(t, got, tea.KeyTab)
	if got.taskField != taskFieldPriority {
		t.Fatalf("taskField = %v, want priority", got.taskField)
	}
	got = pressKey(t, got, tea.KeyRight)
	if got.taskPriority != string(domain.PriorityHigh) {
		t.Fatalf("taskPriority = %q, want high", got.taskPriority)
	}
	got = pressKey(t, got, tea.KeyCtrlS)

	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenView)
	}
	if got.mode != modeNormal {
		t.Fatalf("mode = %v, want %v", got.mode, modeNormal)
	}
	if got.selected != 0 || got.colIdx != 0 || got.cardIdx != 0 {
		t.Fatalf("selection = selected %d col %d card %d, want 0/0/0", got.selected, got.colIdx, got.cardIdx)
	}
	task, ok := got.selectedTask()
	if !ok {
		t.Fatalf("selectedTask() ok = false, want true")
	}
	title := "Created from TUI"
	description := "First line\nSecond line"
	if task.Title != title || task.Description != description || task.Priority != domain.PriorityHigh || task.BucketKey != "backlog" {
		t.Fatalf("selected task = %#v, want title %q description %q priority high in backlog", task, title, description)
	}
	count, err = store.TaskCount(ctx, project.ID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("TaskCount() = %d, want 1", count)
	}

	view := got.View()
	for _, want := range []string{
		"// TITLE",
		title,
		"// BUCKET",
		"backlog",
		"// PRIORITY",
		"high",
		"// BLOCKERS",
		"// COMMENTS",
		"// DESCRIPTION",
		"First line",
		"Second line",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelEditsTaskAndReturnsToView(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Old title", "Old description", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'e')
	if got.taskScreen != taskScreenEdit {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenEdit)
	}
	got = pressBackspace(t, got, len("Old title"))
	got = sendText(t, got, "New title")
	got = pressKey(t, got, tea.KeyTab)
	got = pressBackspace(t, got, len("Old description"))
	got = sendText(t, got, "Line one")
	got = pressKey(t, got, tea.KeyEnter)
	got = sendText(t, got, "Line two")
	got = pressKey(t, got, tea.KeyCtrlS)

	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenView)
	}
	task, ok := got.selectedTask()
	if !ok {
		t.Fatalf("selectedTask() ok = false, want true")
	}
	if task.Title != "New title" || task.Description != "Line one\nLine two" {
		t.Fatalf("selected task = %#v, want edited title and multiline description", task)
	}
}

func TestModelSetsTaskBlockersFromPicker(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	blocker, err := store.CreateTask(ctx, project.ID, "Design dependency", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(blocker) error = %v", err)
	}
	blocked, err := store.CreateTask(ctx, project.ID, "Implement feature", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(blocked) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyDown)
	got = pressKey(t, got, tea.KeyEnter)
	if got.taskID != blocked.ID || got.taskScreen != taskScreenView {
		t.Fatalf("task screen = %v taskID = %d, want blocked task #%d", got.taskScreen, got.taskID, blocked.ID)
	}
	got = pressRune(t, got, 'e')
	if got.taskScreen != taskScreenEdit {
		t.Fatalf("taskScreen = %v, want edit", got.taskScreen)
	}
	got = pressKey(t, got, tea.KeyCtrlB)
	if !got.blockerPickerOpen {
		t.Fatalf("blockerPickerOpen = false, want true")
	}
	if !strings.Contains(got.View(), "Design dependency") {
		t.Fatalf("blocker picker view missing candidate\n%s", got.View())
	}
	got = pressKey(t, got, tea.KeySpace)
	got = pressKey(t, got, tea.KeyCtrlS)

	if got.blockerPickerOpen {
		t.Fatalf("blockerPickerOpen = true, want false after save")
	}
	deps, err := store.ListTaskDependencies(ctx, project.ID, blocked.ID)
	if err != nil {
		t.Fatalf("ListTaskDependencies() error = %v", err)
	}
	if len(deps) != 1 || deps[0].TaskID != blocked.ID || deps[0].DependsOnTaskID != blocker.ID {
		t.Fatalf("dependencies = %#v, want blocked depends on blocker", deps)
	}
	if got.dependencyCount(blocked.ID) != 1 {
		t.Fatalf("dependencyCount() = %d, want 1", got.dependencyCount(blocked.ID))
	}
	got = pressKey(t, got, tea.KeyEsc)
	view := got.View()
	for _, want := range []string{"// BLOCKERS · 1", "Design dependency", "backlog · normal"} {
		if !strings.Contains(view, want) {
			t.Fatalf("task view missing blocker detail %q\n%s", want, view)
		}
	}
}

func TestModelBoardMoveSurfacesWorkflowBlock(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Pinned", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask(setup) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressStringKey(t, model, "right")
	if got.colIdx != 1 {
		t.Fatalf("colIdx = %d, want 1 (dev column)", got.colIdx)
	}
	got = pressRune(t, got, 'm')
	if !got.moveMode {
		t.Fatalf("moveMode = false, want true")
	}
	got = pressStringKey(t, got, "left")

	if got.colIdx != 1 {
		t.Fatalf("colIdx after blocked move = %d, want 1 (task should not move visually)", got.colIdx)
	}
	if got.moveMode {
		t.Fatalf("moveMode = true, want false (clears after blocked attempt)")
	}
	if !strings.Contains(got.status, "transition not allowed") {
		t.Fatalf("status = %q, want it to surface workflow_invalid_transition", got.status)
	}

	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].BucketKey != "dev" {
		t.Fatalf("tasks after blocked move = %#v, want unchanged in dev", tasks)
	}
}

func TestModelTaskViewWrapsLongPropertyTextWithoutBreakingGrid(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	longTitle := "Atualizar a documentação do projeto com orientações operacionais muito detalhadas para evitar quebra visual"
	longDescription := "Revisar a documentação existente, completar pontos faltantes e alinhar as instruções ao comportamento atual do projeto, incluindo casos de borda com textos extensos para validação de layout."
	if _, err := store.CreateTask(ctx, project.ID, longTitle, longDescription, "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	view := got.View()
	plain := ansi.Strip(view)
	lines := strings.Split(plain, "\n")

	start := -1
	end := -1
	for i, line := range lines {
		if strings.Contains(line, "┌") && strings.Contains(line, "┐") {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("task view grid top border not found\n%s", plain)
	}
	for i := len(lines) - 1; i > start; i-- {
		if strings.Contains(lines[i], "└") && strings.Contains(lines[i], "┘") {
			end = i
			break
		}
	}
	if end == -1 {
		t.Fatalf("task view grid bottom border not found\n%s", plain)
	}

	wantWidth := lipgloss.Width(lines[start])
	for i := start; i <= end; i++ {
		if gotWidth := lipgloss.Width(lines[i]); gotWidth != wantWidth {
			t.Fatalf("grid row %d width = %d, want %d\n%s", i, gotWidth, wantWidth, plain)
		}
	}

	for _, want := range []string{"// TITLE", "// DESCRIPTION", "comportamento", "projeto"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("View() missing %q\n%s", want, plain)
		}
	}
}

func TestModelBoardCollapsesToFocusedColumnWhenNarrow(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Backlog task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask(backlog) error = %v", err)
	}
	devTask, err := store.CreateTask(ctx, project.ID, "Dev task", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(dev) error = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, devTask.ID, "dev"); err != nil {
		t.Fatalf("MoveTask(dev) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 40

	view := ansi.Strip(model.View())
	for _, want := range []string{"lanes 1–1 / 2", "BACKLOG", "Backlog task"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow board missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "DEVELOPMENT") {
		t.Fatalf("narrow board rendered non-focused column\n%s", view)
	}

	got := pressStringKey(t, model, "right")
	view = ansi.Strip(got.View())
	for _, want := range []string{"lanes 2–2 / 2", "DEVELOPMENT", "Dev task"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow board after right missing %q\n%s", want, view)
		}
	}
}

func TestModelBoardShowsMultipleColumnsWhenTheyFit(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, multiBucketBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	// Width that fits 2 of 4 buckets side-by-side. Workflow has Backlog,
	// Development, Review, Done (4 buckets). The narrow path used to drop
	// to a single column; now it should show as many as fit.
	model.width = 80

	view := ansi.Strip(model.View())
	visibleHeaders := 0
	for _, name := range []string{"BACKLOG", "DEVELOPMENT", "REVIEW", "DONE"} {
		if strings.Contains(view, name) {
			visibleHeaders++
		}
	}
	if visibleHeaders < 2 {
		t.Fatalf("expected ≥2 board columns visible, got %d:\n%s", visibleHeaders, view)
	}
	if !strings.Contains(view, "lanes") {
		t.Fatalf("expected lanes scroll hint when not all columns fit:\n%s", view)
	}
}

func TestModelBoardLaneNavigationWrapsAround(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, multiBucketBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	n := len(model.workflow.Buckets)
	if n < 2 {
		t.Fatalf("multiBucketBundle should produce >=2 buckets, got %d", n)
	}

	// Right past the last lane wraps to the first.
	got := model
	for i := 0; i < n; i++ {
		got = pressStringKey(t, got, "right")
	}
	if got.colIdx != 0 {
		t.Fatalf("after %d rights colIdx = %d, want 0 (wrap)", n, got.colIdx)
	}

	// Left from the first lane wraps to the last.
	got = pressStringKey(t, got, "left")
	if got.colIdx != n-1 {
		t.Fatalf("left from first colIdx = %d, want %d (wrap)", got.colIdx, n-1)
	}
}

// TestModelSettingsLawsRendersOwnColumnWhenNarrow replaces the T1
// horizontal-grid sliding test (`TestModelConfigUsesFocusedSectionWhenNarrow`).
// The T2 split makes each entity kind its own Settings sub, so a
// narrow terminal no longer needs a slide-and-hidden-list hint — the
// active sub renders its single column at full width.
func TestModelSettingsLawsRendersOwnColumnWhenNarrow(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 50
	// Settings/general first, then advance to Settings/laws.
	got := pressRune(t, model, '3')
	got = pressStringKey(t, got, "/")

	view := ansi.Strip(got.View())
	if !strings.Contains(view, "// LAWS") {
		t.Fatalf("Settings › Laws column header missing on narrow terminal:\n%s", view)
	}
	for _, leaked := range []string{"// PERSONAS", "// SKILLS", "// TEMPLATES", "// TAGS"} {
		if strings.Contains(view, leaked) {
			t.Fatalf("Settings › Laws should not co-render sibling kind %q:\n%s", leaked, view)
		}
	}
}

func TestModelHelpDefaultsToCurrentContext(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressStringKey(t, model, "?")
	view := ansi.Strip(got.View())
	for _, want := range []string{"KEYBINDINGS · CURRENT CONTEXT", "// GLOBAL", "// TASKS · BOARD LENS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("context help missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "// SKILL PICKER") {
		t.Fatalf("context help rendered unrelated skill picker group\n%s", view)
	}

	got = pressRune(t, got, 'a')
	view = ansi.Strip(got.View())
	for _, want := range []string{"KEYBINDINGS · ALL CONTEXTS", "// SKILL PICKER"} {
		if !strings.Contains(view, want) {
			t.Fatalf("all help missing %q\n%s", want, view)
		}
	}
}

func TestModelCancelsTaskCreateWithoutPersisting(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := sendText(t, pressRune(t, model, 'n'), "Draft title")
	got = pressKey(t, got, tea.KeyEsc)
	if got.taskScreen != taskScreenClosed {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenClosed)
	}
	count, err := store.TaskCount(ctx, project.ID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("TaskCount() = %d, want 0", count)
	}
}

// TestNavHeaderRendersTopAndSubKickers locks in the T1 navigation refactor:
// the per-project header renders the three top zones as `01 // TASKS`,
// `02 // STATS`, `03 // SETTINGS`, and surfaces a sub-menu strip when the
// active top has more than one sub. The strip is suppressed on Settings
// (single sub).
func TestNavHeaderRendersTopAndSubKickers(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store), ActivityLogs: store, Metrics: app.NewMetricsService(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 200

	tasksHeader := ansi.Strip(model.View())
	for _, want := range []string{"01 // TASKS", "02 // STATS", "03 // SETTINGS", "// board", "// table", "// graph"} {
		if !strings.Contains(tasksHeader, want) {
			t.Fatalf("tasks header missing %q\n%s", want, tasksHeader)
		}
	}

	statsModel := pressRune(t, model, '2')
	statsHeader := ansi.Strip(statsModel.View())
	for _, want := range []string{"01 // TASKS", "02 // STATS", "03 // SETTINGS", "// general", "// logs"} {
		if !strings.Contains(statsHeader, want) {
			t.Fatalf("stats header missing %q\n%s", want, statsHeader)
		}
	}
	if strings.Contains(statsHeader, "// board") || strings.Contains(statsHeader, "// graph") {
		t.Fatalf("stats header leaked tasks subs:\n%s", statsHeader)
	}

	settingsModel := pressRune(t, model, '3')
	settingsHeader := ansi.Strip(settingsModel.View())
	for _, want := range []string{"01 // TASKS", "02 // STATS", "03 // SETTINGS", "// general", "// laws", "// personas", "// skills", "// templates", "// tags"} {
		if !strings.Contains(settingsHeader, want) {
			t.Fatalf("settings header missing %q\n%s", want, settingsHeader)
		}
	}
	for _, leaked := range []string{"// board", "// table", "// graph"} {
		if strings.Contains(settingsHeader, leaked) {
			t.Fatalf("settings header leaked tasks subs %q:\n%s", leaked, settingsHeader)
		}
	}
}

// TestSubCycleBindings exercises the comma/slash sub-cycle bindings inside
// the Tasks zone (board → table → graph and wrap-around) and confirms the
// no-op behavior on Settings (single sub).
func TestSubCycleBindings(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressStringKey(t, model, "/")
	if got.top != topTasks || got.sub != subTable {
		t.Fatalf("after first '/': (top, sub) = (%d, %d), want (topTasks, subTable)", got.top, got.sub)
	}
	got = pressStringKey(t, got, "/")
	if got.sub != subGraph {
		t.Fatalf("after second '/': sub = %d, want subGraph", got.sub)
	}
	got = pressStringKey(t, got, "/")
	if got.sub != subBoard {
		t.Fatalf("after third '/': sub = %d, want subBoard (wrap-around)", got.sub)
	}
	got = pressStringKey(t, got, ",")
	if got.sub != subGraph {
		t.Fatalf("after ',' from board: sub = %d, want subGraph (wrap-around)", got.sub)
	}

	got = pressRune(t, model, '3')
	if got.top != topSettings || got.sub != subSettingsGeneral {
		t.Fatalf("after '3': (top, sub) = (%d, %d), want (topSettings, subSettingsGeneral)", got.top, got.sub)
	}
	got = pressStringKey(t, got, "/")
	if got.top != topSettings || got.sub != subSettingsLaws {
		t.Fatalf("'/' on Settings/general should advance to Settings/laws: (top, sub) = (%d, %d)", got.top, got.sub)
	}
}

// TestCtrlOPopsBackStack covers AC2: every intentional zone/sub
// navigation pushes the current (top, sub) onto the back-stack, and
// `ctrl+o` pops the stack to restore the previous view. Empty-stack
// presses are silent no-ops (no status flash, no nav change).
func TestCtrlOPopsBackStack(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store), ActivityLogs: store, Metrics: app.NewMetricsService(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	// Empty stack — ctrl+o is silently dropped, no nav change.
	got := pressStringKey(t, model, "ctrl+o")
	if got.top != topTasks || got.sub != subBoard {
		t.Fatalf("ctrl+o on empty stack mutated nav: (top, sub) = (%d, %d)", got.top, got.sub)
	}

	// Tasks/board → Stats/general → Settings/general, then ctrl+o twice.
	got = pressRune(t, model, '2')
	if got.top != topStats || got.sub != subStatsGeneral {
		t.Fatalf("'2' should jump to Stats/general: (top, sub) = (%d, %d)", got.top, got.sub)
	}
	got = pressRune(t, got, '3')
	if got.top != topSettings || got.sub != subSettingsGeneral {
		t.Fatalf("'3' should jump to Settings/general: (top, sub) = (%d, %d)", got.top, got.sub)
	}
	got = pressStringKey(t, got, "ctrl+o")
	if got.top != topStats || got.sub != subStatsGeneral {
		t.Fatalf("ctrl+o should restore Stats/general: (top, sub) = (%d, %d)", got.top, got.sub)
	}
	got = pressStringKey(t, got, "ctrl+o")
	if got.top != topTasks || got.sub != subBoard {
		t.Fatalf("ctrl+o should restore Tasks/board: (top, sub) = (%d, %d)", got.top, got.sub)
	}
	if len(got.viewHistory) != 0 {
		t.Fatalf("viewHistory should be empty after popping every entry, got %d", len(got.viewHistory))
	}
}

// TestHomeTileEmbeddedInTopStrip covers AC3: the per-project header
// surfaces `00 // HOME` to the left of the three top zones, separated
// by the faded `│` divider. Keeps the home affordance visible without
// growing the chrome to a third row.
func TestHomeTileEmbeddedInTopStrip(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Workflow: app.NewWorkflowServiceFromStore(store), ActivityLogs: store, Metrics: app.NewMetricsService(store)}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 200
	view := ansi.Strip(model.View())
	for _, want := range []string{"00 // HOME", "│", "01 // TASKS", "02 // STATS", "03 // SETTINGS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("top strip missing %q:\n%s", want, view)
		}
	}
	homeIdx := strings.Index(view, "00 // HOME")
	tasksIdx := strings.Index(view, "01 // TASKS")
	if homeIdx < 0 || tasksIdx < 0 || homeIdx >= tasksIdx {
		t.Fatalf("HOME tile must render before TASKS in the strip; homeIdx=%d, tasksIdx=%d", homeIdx, tasksIdx)
	}
}

func pressRune(t *testing.T, model Model, r rune) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return got
}

func pressKey(t *testing.T, model Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyMsg{Type: key})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return got
}

func pressAltKey(t *testing.T, model Model, key tea.KeyType) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyMsg{Type: key, Alt: true})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return got
}

func pressStringKey(t *testing.T, model Model, key string) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return got
}

func sendText(t *testing.T, model Model, text string) Model {
	t.Helper()
	got := model
	for _, r := range text {
		got = pressRune(t, got, r)
	}
	return got
}

func pressBackspace(t *testing.T, model Model, count int) Model {
	t.Helper()
	got := model
	for range count {
		got = pressKey(t, got, tea.KeyBackspace)
	}
	return got
}

func tuiTestBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []config.Skill{{Slug: "go", Name: "Go"}},
		Personas: []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}},
		Laws:     []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}

// multiBucketBundle returns a bundle with the same wiring as tuiTestBundle
// but a 4-bucket workflow so tests can exercise the horizontal sliding
// window in renderBoard at narrower widths.
func multiBucketBundle() config.Bundle {
	b := tuiTestBundle()
	b.Workflows[0].Buckets = []config.Bucket{
		{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
		{ID: 2, Key: "dev", Name: "Development", Position: 2},
		{ID: 3, Key: "review", Name: "Review", Position: 3},
		{ID: 4, Key: "done", Name: "Done", Position: 4},
	}
	b.Workflows[0].Transitions = []config.Transition{
		{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 4},
	}
	return b
}

func tuiTestTheme() config.Theme {
	return config.Theme{
		Version: 1,
		Key:     "catppuccin",
		Name:    "Catppuccin",
		Colors: map[string]string{
			"background": "#24273A",
			"foreground": "#CAD3F5",
			"primary":    "#8AADF4",
			"secondary":  "#C6A0F6",
			"border":     "#494D64",
			"highlight":  "#363A4F",
			"error":      "#ED8796",
		},
	}
}
