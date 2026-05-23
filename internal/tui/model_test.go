package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/activity"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)


func TestModelSwitchesViews(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	blocker, err := store.CreateTask(ctx, project.ID, "Blocker", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(blocker) error = %v", err)
	}
	blocked, err := store.CreateTask(ctx, project.ID, "Blocked", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(blocked) error = %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, blocked.ID, blocker.ID); err != nil {
		t.Fatalf("AddTaskDependency() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	longTitle := "Investigate viewport usage for the TUI table without truncating the task title"
	if _, err := store.CreateTask(ctx, project.ID, longTitle, "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if cmd := model.Init(); cmd == nil {
		t.Fatal("Init() command = nil, want realtime refresh tick")
	}
	if len(model.tasks) != 0 {
		t.Fatalf("initial tasks len = %d, want 0", len(model.tasks))
	}
	if _, err := store.CreateTask(ctx, project.ID, "External task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Existing task", "First line\nSecond line", domain.Priority(2), "backlog", nil, store.Snapshot())
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	plain := stripANSI(view)
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
		if !strings.Contains(plain, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelAddsMultilineCommentInsideTaskCommentsPanel(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Existing task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	plainView := stripANSI(view)
	for _, want := range []string{"// ACTIVITY · 1", "human", comments[0].CreatedAt, "First line", "Second line", "Third line"} {
		if !strings.Contains(plainView, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelCreatesTaskFromDedicatedScreen(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	descriptionInput := got.renderTaskDescriptionField(got.taskFormWidth())
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
	if got.taskPriority != domain.Priority(3) {
		t.Fatalf("taskPriority = %d, want high (id 3)", got.taskPriority)
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
	if task.Title != title || task.Description != description || task.Priority != domain.Priority(3) || task.BucketKey != "backlog" {
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
		if !strings.Contains(stripANSI(view), want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestModelEditsTaskAndReturnsToView(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Old title", "Old description", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

// TestOpenTaskEditCalibratesDescriptionTextarea locks the fix for the
// "field empties on first keystroke" bug. Pre-fix, openTaskEdit loaded
// the description into a textarea that still carried the bubbles
// package-default geometry (Width=40, Height=6 after Prompt/LineNumbers
// reservations resolve). Subsequent Update(msg) calls — typing,
// arrow-key navigation — wrapped against that stale 40-col width even
// though the render path sized a per-frame copy to ~68 cols. The
// resulting yOffset desync visually emptied the field for users
// running terminals where the form's inner width and the bubbles
// default diverge.
//
// The fix calls multilineform.Resize on the persistent textarea so
// every Update operates on the same wrap width Render uses. This test
// asserts the persistent geometry after openTaskEdit matches what
// renderTaskDescriptionField will pass downstream — pre-fix Width()
// would still report the bubbles default and Height() would still be
// the default viewport rows.
func TestOpenTaskEditCalibratesDescriptionTextarea(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task with body", "Existing description across\nmultiple lines.", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(WindowSizeMsg) returned %T, want Model", updated)
	}

	got = pressKey(t, got, tea.KeyEnter) // enter task view
	got = pressRune(t, got, 'e')         // open edit form
	if got.taskScreen != taskScreenEdit {
		t.Fatalf("taskScreen = %v, want %v", got.taskScreen, taskScreenEdit)
	}

	// Persistent textarea must be sized to the form's actual geometry.
	// renderTaskDescriptionField passes taskFormWidth and
	// taskDescriptionInputHeight down to multilineform.Render, which
	// derives the inner width by subtracting the formMultiline
	// horizontal padding (4 cols). The persistent model has to mirror
	// that or Update operates on a different wrap.
	wantInnerWidth := got.taskFormWidth() - got.styles.formMultiline.GetHorizontalPadding()
	if w := got.taskDescriptionInput.Width(); w != wantInnerWidth {
		t.Fatalf("taskDescriptionInput.Width() = %d, want %d (taskFormWidth %d minus padding %d) — Resize at openTaskEdit not applied", w, wantInnerWidth, got.taskFormWidth(), got.styles.formMultiline.GetHorizontalPadding())
	}
	if h := got.taskDescriptionInput.Height(); h != taskDescriptionInputHeight {
		t.Fatalf("taskDescriptionInput.Height() = %d, want %d — Resize at openTaskEdit not applied", h, taskDescriptionInputHeight)
	}
}

func TestModelSetsTaskBlockersFromPicker(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	blocker, err := store.CreateTask(ctx, project.ID, "Design dependency", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(blocker) error = %v", err)
	}
	blocked, err := store.CreateTask(ctx, project.ID, "Implement feature", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(blocked) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Pinned", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev", store.Snapshot()); err != nil {
		t.Fatalf("MoveTask(setup) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].BucketKey != "dev" {
		t.Fatalf("tasks after blocked move = %#v, want unchanged in dev", tasks)
	}
}

func TestModelTaskViewWrapsLongPropertyTextWithoutBreakingGrid(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	longTitle := "Atualizar a documentação do projeto com orientações operacionais muito detalhadas para evitar quebra visual"
	longDescription := "Revisar a documentação existente, completar pontos faltantes e alinhar as instruções ao comportamento atual do projeto, incluindo casos de borda com textos extensos para validação de layout."
	if _, err := store.CreateTask(ctx, project.ID, longTitle, longDescription, domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	// Scan forward from the form column's top border to the first
	// closing └┘ row — that bounds the form column box. The detail
	// view now stacks a sub-tasks pane below the form, so the older
	// "last └┘ in the output" probe wandered into a different box
	// and asserted against blank separator rows in between.
	for i := start + 1; i < len(lines); i++ {
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Backlog task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask(backlog) error = %v", err)
	}
	devTask, err := store.CreateTask(ctx, project.ID, "Dev task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(dev) error = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, devTask.ID, "dev", store.Snapshot()); err != nil {
		t.Fatalf("MoveTask(dev) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, multiBucketBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, multiBucketBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), ActivityLogs: store, Metrics: app.NewMetricsService(store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	if got.sub != subPlans {
		t.Fatalf("after third '/': sub = %d, want subPlans", got.sub)
	}
	got = pressStringKey(t, got, "/")
	if got.sub != subBoard {
		t.Fatalf("after fourth '/': sub = %d, want subBoard (wrap-around)", got.sub)
	}
	got = pressStringKey(t, got, ",")
	if got.sub != subPlans {
		t.Fatalf("after ',' from board: sub = %d, want subPlans (wrap-around)", got.sub)
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), ActivityLogs: store, Metrics: app.NewMetricsService(store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), ActivityLogs: store, Metrics: app.NewMetricsService(store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

// TestModelDeletesTaskFromTaskViewWithDoubleD covers the new arm-then-confirm
// `d`/`d` shortcut. Delete only fires from inside the task view with the
// form column focused — the user has to commit to the task first, mirroring
// the destructive-action gating the user asked for.
func TestModelDeletesTaskFromTaskViewWithDoubleD(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Doomed", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter) // open task view
	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v, want taskScreenView (must enter the task before deleting)", got.taskScreen)
	}

	armed := pressRune(t, got, 'd')
	if armed.taskDeletePendingID != task.ID {
		t.Fatalf("taskDeletePendingID = %d, want %d", armed.taskDeletePendingID, task.ID)
	}
	if !strings.Contains(armed.status, "Confirm delete task") {
		t.Fatalf("status = %q, want a confirm-delete prompt", armed.status)
	}

	confirmed := pressRune(t, armed, 'd')
	if confirmed.taskDeletePendingID != 0 {
		t.Fatalf("taskDeletePendingID = %d, want cleared after confirm", confirmed.taskDeletePendingID)
	}
	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{IncludeArchived: true}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("ListTasks() = %d tasks, want 0 (delete must hard-remove the row)", len(tasks))
	}
}

// TestModelBlocksTaskEditOnPressWhenBucketForbids covers the pre-check
// gate: pressing `e` on a task whose current bucket forbids edit must
// surface the policy hint immediately and refuse to open the form. The
// service still re-runs the policy on save, but the user should never
// type into a modal that is doomed to fail.
//
func TestModelBlocksTaskEditOnPressWhenBucketForbids(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	// Default policy: edit allowed only on the workflow's first bucket.
	// The fixture has backlog → dev; placing the task in dev means edit
	// is forbidden under the canonical default.
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Locked", "", domain.Priority(2), "dev", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), Events: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	// Cursor starts in the first column (backlog) which is empty; move
	// right to land on the dev column where the task lives.
	got := pressKey(t, model, tea.KeyRight)
	got = pressKey(t, got, tea.KeyEnter)
	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v, want taskScreenView", got.taskScreen)
	}
	got = pressRune(t, got, 'e')
	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v after blocked edit; want taskScreen unchanged (edit must not open)", got.taskScreen)
	}
	if !strings.Contains(got.status, "policy:") || !strings.Contains(got.status, "task.edit") {
		t.Fatalf("status = %q, want a policy hint mentioning task.edit", got.status)
	}
}

// TestModelBlocksTaskDeleteArmWhenBucketForbids mirrors the edit gate for
// the destructive `d` arm. The first press in a forbidden bucket should
// surface the policy hint and skip the arm — the user should not see a
// "Confirm delete..." prompt for an action that cannot succeed.
//
func TestModelBlocksTaskDeleteArmWhenBucketForbids(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	// Default delete policy is "false everywhere" — every bucket forbids
	// delete unless explicitly opted in. backlog suffices for the fixture.
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Locked", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), Events: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'd')
	if got.taskDeletePendingID != 0 {
		t.Fatalf("taskDeletePendingID = %d, want 0 (forbidden delete must not arm)", got.taskDeletePendingID)
	}
	if !strings.Contains(got.status, "policy:") || !strings.Contains(got.status, "task.delete") {
		t.Fatalf("status = %q, want a policy hint mentioning task.delete", got.status)
	}
}

// TestModelBoardDoesNotArmDeleteOnD locks down the rule that destructive
// actions are not reachable from the board — pressing `d` on a card must
// be a no-op so an accidental keystroke cannot wipe a row the user has
// not committed to.
func TestModelBoardDoesNotArmDeleteOnD(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Survives", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	got := pressRune(t, model, 'd')
	if got.taskDeletePendingID != 0 {
		t.Fatalf("board-level `d` must not arm delete; taskDeletePendingID = %d", got.taskDeletePendingID)
	}
	got = pressRune(t, got, 'd')
	tasks, _ := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, store.Snapshot())
	if len(tasks) != 1 {
		t.Fatalf("ListTasks() = %d, want 1 (board `d` must not delete)", len(tasks))
	}
}

// TestModelCancelsArmedTaskDeleteOnNavigation ensures any non-`d` keystroke
// inside the task view disarms a pending delete prompt so the second `d`
// cannot fire after the user moved focus or pressed something unrelated.
func TestModelCancelsArmedTaskDeleteOnNavigation(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Survives", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), Events: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	armed := pressRune(t, got, 'd')
	if armed.taskDeletePendingID == 0 {
		t.Fatalf("expected armed pending after first d")
	}
	// `r` triggers a refresh — a non-`d` key that should disarm the prompt
	// without otherwise touching the task.
	disarmed := pressRune(t, armed, 'r')
	if disarmed.taskDeletePendingID != 0 {
		t.Fatalf("taskDeletePendingID = %d, want cleared by `r` navigation", disarmed.taskDeletePendingID)
	}
	tasks, _ := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, store.Snapshot())
	if len(tasks) != 1 {
		t.Fatalf("ListTasks() = %d, want 1 (delete must not have fired)", len(tasks))
	}
}

// TestModelDeletesCommentFromCommentScreen covers the `d`/`d` shortcut
// inside the dedicated comment screen — the user must enter the comment
// (Enter on a focused activity card) before any destructive verb is
// reachable. Comment delete enforces the bucket permissions.comment.delete
// policy via CommentService.Remove.
func TestModelDeletesCommentFromCommentScreen(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	comment, err := store.AddComment(ctx, project.ID, task.ID, "Comment to remove", "human", nil)
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter) // open task view
	got = pressKey(t, got, tea.KeyTab)      // focus activity column
	if got.taskFocus != taskFocusActivity {
		t.Fatalf("taskFocus = %v, want activity", got.taskFocus)
	}
	// Activity feed is chronological: events[0] is the task.created system
	// event, events[1] is the comment we just added. Advance past the
	// system event so Enter lands on the comment.
	got = pressStringKey(t, got, "J")
	if got.activityCursor != 1 {
		t.Fatalf("activityCursor = %d, want 1 (comment row)", got.activityCursor)
	}
	got = pressKey(t, got, tea.KeyEnter)
	if !got.commentScreenOpen || got.commentScreenID != comment.ID {
		t.Fatalf("commentScreenOpen = %v, commentScreenID = %d, want true / %d", got.commentScreenOpen, got.commentScreenID, comment.ID)
	}

	armed := pressRune(t, got, 'd')
	if armed.commentDeletePendingID != comment.ID {
		t.Fatalf("commentDeletePendingID = %d, want %d", armed.commentDeletePendingID, comment.ID)
	}

	confirmed := pressRune(t, armed, 'd')
	if confirmed.commentDeletePendingID != 0 {
		t.Fatalf("commentDeletePendingID = %d, want cleared after confirm", confirmed.commentDeletePendingID)
	}
	if confirmed.commentScreenOpen {
		t.Fatalf("commentScreenOpen = true, want auto-closed after delete")
	}
	remaining, err := store.ListComments(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("ListComments() = %d, want 0 (comment should be hard-deleted)", len(remaining))
	}
}

// TestModelEditsCommentFromCommentScreen covers the `e` shortcut from the
// dedicated comment screen: open a pre-filled modal seeded with the
// existing body, rewrite it, save through CommentService.Edit
// (workflow-aware so bucket policy is enforced).
func TestModelEditsCommentFromCommentScreen(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	comment, err := store.AddComment(ctx, project.ID, task.ID, "Original body", "human", nil)
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)
	// Skip past the chronologically-first task.created system event so
	// Enter on the activity card opens the comment we want to edit.
	got = pressStringKey(t, got, "J")
	got = pressKey(t, got, tea.KeyEnter)
	if !got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = false, want true after entering the comment")
	}
	got = pressRune(t, got, 'e')
	if !got.commentScreenEditing {
		t.Fatalf("commentScreenEditing = false, want true after pressing 'e'")
	}
	if got.commentEditID != comment.ID {
		t.Fatalf("commentEditID = %d, want %d", got.commentEditID, comment.ID)
	}
	if !got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = false, want the dedicated overlay to stay open while editing")
	}
	if got.isEmbeddedCommentInput() {
		t.Fatalf("isEmbeddedCommentInput() = true, want false — edit lives in the dedicated overlay now")
	}
	if got.commentInput.Value() != "Original body" {
		t.Fatalf("commentInput.Value() = %q, want pre-filled with original body", got.commentInput.Value())
	}

	// Erase the original body via repeated backspace so we test caret
	// editing instead of a blunt SetValue. bubbles' textarea handles
	// rune-aware backspace at the cursor, mirroring real-terminal UX.
	got = pressBackspace(t, got, len("Original body"))
	got = sendText(t, got, "Rewritten body")
	got = pressKey(t, got, tea.KeyCtrlS)

	if got.commentScreenEditing {
		t.Fatalf("commentScreenEditing = true after ctrl+s, want false (back to read view)")
	}
	if got.commentEditID != 0 {
		t.Fatalf("commentEditID = %d, want cleared after save", got.commentEditID)
	}
	if !got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = false after save, want true (still in read view)")
	}
	comments, err := store.ListComments(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "Rewritten body" {
		t.Fatalf("ListComments() = %+v, want one comment with body %q", comments, "Rewritten body")
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

// tuiTestBundle loads the default 2-bucket workflow used by most TUI
// tests. testdata/default_workflow.yaml carries strict defaults plus a
// backlog opt-in so editing is allowed only on the first bucket.
// Skills/Personas/Laws are wired in Go because config.Bundle marks
// those fields `yaml:"-"`.
func tuiTestBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "default_workflow.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}}
	return bundle
}

// tuiPermissiveBundle loads the all-allow fixture for tests that exercise
// the success path of policy-gated keybindings.
func tuiPermissiveBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "permissive.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}}
	return bundle
}

// multiBucketBundle loads the 4-bucket fixture used by board-rendering
// tests that assert the horizontal sliding window. Defaults permissive
// because these tests care about geometry, not policy.
func multiBucketBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "multi_bucket.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}}
	return bundle
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

// TestPlansSubTabRendersRollups exercises the new Tasks › plans list
// view: refresh() must call PlanService.ListRollups, the renderer must
// emit one row per plan with slug/status/done-total/percent, and the
// kicker count must reflect the number of rollups. Seeds two plans —
// one with a closed and an open task across two waves so DoneCount,
// TotalCount, and ActiveWaveName all take non-zero defaults.
func TestPlansSubTabRendersRollups(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	planA, err := store.CreatePlan(ctx, project.ID, "rollout-a", "Rollout A", "")
	if err != nil {
		t.Fatalf("CreatePlan(A) error = %v", err)
	}
	if _, err := store.CreatePlan(ctx, project.ID, "rollout-b", "Rollout B", ""); err != nil {
		t.Fatalf("CreatePlan(B) error = %v", err)
	}
	wave1, err := store.AddPlanWave(ctx, project.ID, planA.ID, "Wave 1", 1)
	if err != nil {
		t.Fatalf("AddPlanWave(1) error = %v", err)
	}
	if _, err := store.AddPlanWave(ctx, project.ID, planA.ID, "Wave 2", 2); err != nil {
		t.Fatalf("AddPlanWave(2) error = %v", err)
	}
	taskOpen, err := store.CreateTask(ctx, project.ID, "open", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask(open) error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, taskOpen.ID, planA.ID, wave1.ID); err != nil {
		t.Fatalf("AssignTaskToPlan(open) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	// Cycle board → table → graph → plans.
	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	if got.sub != subPlans {
		t.Fatalf("third '/': sub = %d, want subPlans", got.sub)
	}
	if len(got.plans) != 2 {
		t.Fatalf("len(plans) = %d, want 2", len(got.plans))
	}

	view := ansi.Strip(got.View())
	if !strings.Contains(view, "// PLANS") {
		t.Fatalf("plans view missing kicker\n%s", view)
	}
	if !strings.Contains(view, "rollout-a") || !strings.Contains(view, "rollout-b") {
		t.Fatalf("plans view missing plan rows\n%s", view)
	}
	if !strings.Contains(view, "Wave 1") {
		t.Fatalf("plans view missing active wave name\n%s", view)
	}

	// j moves planCursor; k restores it.
	advanced := pressRune(t, got, 'j')
	if advanced.planCursor != 1 {
		t.Fatalf("after 'j': planCursor = %d, want 1", advanced.planCursor)
	}
	back := pressRune(t, advanced, 'k')
	if back.planCursor != 0 {
		t.Fatalf("after 'k': planCursor = %d, want 0", back.planCursor)
	}
}

// TestPlansSubTabEnterOpensNetwork covers the list → network transition:
// enter on the cursored plan loads PlanService.Show, flips
// planNetworkOpen, and the rendered view shows column headers (// W1,
// // W2), per-wave done/total counts, the @assigned_to marker, and the
// header progress badge. h moves the wave cursor; esc returns to the
// list view.
func TestPlansSubTabEnterOpensNetwork(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	w1, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Foundation", 1)
	if err != nil {
		t.Fatalf("AddPlanWave(1) error = %v", err)
	}
	w2, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Migration", 2)
	if err != nil {
		t.Fatalf("AddPlanWave(2) error = %v", err)
	}
	tOpen, err := store.CreateTask(ctx, project.ID, "foundation-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask(open) error = %v", err)
	}
	tGated, err := store.CreateTask(ctx, project.ID, "migration-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask(gated) error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, tOpen.ID, plan.ID, w1.ID); err != nil {
		t.Fatalf("AssignTaskToPlan(open) error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, tGated.ID, plan.ID, w2.ID); err != nil {
		t.Fatalf("AssignTaskToPlan(gated) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	if got.sub != subPlans {
		t.Fatalf("third '/': sub = %d, want subPlans", got.sub)
	}

	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("after enter: planNetworkOpen = false, want true")
	}
	if len(opened.planNetworkShow.Waves) != 2 {
		t.Fatalf("planNetworkShow.Waves = %d, want 2", len(opened.planNetworkShow.Waves))
	}

	view := ansi.Strip(opened.View())
	if !strings.Contains(view, "// PLAN · rollout") {
		t.Fatalf("network header missing\n%s", view)
	}
	if !strings.Contains(view, "W1") || !strings.Contains(view, "W2") {
		t.Fatalf("network missing wave headers\n%s", view)
	}
	if !strings.Contains(view, "‹active›") {
		t.Fatalf("network missing active wave tag\n%s", view)
	}
	if !strings.Contains(view, "foundation-task") || !strings.Contains(view, "migration-task") {
		t.Fatalf("network missing task titles\n%s", view)
	}

	// j advances the linear cursor one row; k walks it back. The
	// rails+filaments outline collapses the multi-axis cursor of the
	// old column view into a single index into the flat row list.
	startCursor := opened.planNetworkCursor
	advanced := pressRune(t, opened, 'j')
	if advanced.planNetworkCursor != startCursor+1 {
		t.Fatalf("after 'j': planNetworkCursor = %d, want %d", advanced.planNetworkCursor, startCursor+1)
	}
	back := pressRune(t, advanced, 'k')
	if back.planNetworkCursor != startCursor {
		t.Fatalf("after 'k': planNetworkCursor = %d, want %d", back.planNetworkCursor, startCursor)
	}

	closed := pressKey(t, opened, tea.KeyEsc)
	if closed.planNetworkOpen {
		t.Fatalf("after esc: planNetworkOpen = true, want false")
	}
}

// TestPlansSubTabNetworkClaimsNextTask exercises the `c` binding inside
// the network view: it must open the assignee text input for the
// focused task without touching the bucket, accept a typed assignee,
// stamp tasks.assigned_to on submit, and reload the projection so the
// in-progress badge surfaces — all while leaving the bucket guard
// (omakase self-branch comment for backlog → dev) authoritative.
func TestPlansSubTabNetworkAssignOpensInputAndStampsAssignee(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, multiBucketBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Foundation", 1)
	if err != nil {
		t.Fatalf("AddPlanWave() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "foundation-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("network did not open")
	}
	// Cursor sits on the wave header by default; step once so the
	// task row is focused before triggering the assign modal.
	opened = pressRune(t, opened, 'j')

	editing := pressRune(t, opened, 'c')
	if editing.mode != modePlanAssign {
		t.Fatalf("after 'c' mode = %v, want modePlanAssign (text input open)", editing.mode)
	}
	if editing.planAssignTaskID != task.ID {
		t.Fatalf("planAssignTaskID = %d, want %d (cursor row task)", editing.planAssignTaskID, task.ID)
	}

	typed := editing
	for _, r := range "alice" {
		typed = pressRune(t, typed, r)
	}
	submitted := pressKey(t, typed, tea.KeyEnter)
	if submitted.mode != modeNormal {
		t.Fatalf("after submit mode = %v, want modeNormal", submitted.mode)
	}

	tasksFilter, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, snap)
	if err != nil {
		t.Fatalf("ListTasks after assign: %v", err)
	}
	if len(tasksFilter) != 1 {
		t.Fatalf("ListTasks returned %d tasks, want 1", len(tasksFilter))
	}
	if tasksFilter[0].BucketKey != "backlog" {
		t.Fatalf("bucket after assign = %q, want backlog (assign must NOT move the task)", tasksFilter[0].BucketKey)
	}

	view := ansi.Strip(submitted.View())
	if !strings.Contains(view, "@alice") {
		t.Fatalf("network view missing @alice marker\n%s", view)
	}
	// Task stays in the first bucket (backlog) — assign no longer moves
	// it, so the badge must read `assigned` (claimed, not started), NOT
	// `in-progress` (which is reserved for tasks already past the first
	// bucket).
	if !strings.Contains(view, "assigned") {
		t.Fatalf("network view missing `assigned` badge after assign\n%s", view)
	}
	if strings.Contains(view, "in-progress") {
		t.Fatalf("network view should NOT show `in-progress` for a backlog-staying assignment\n%s", view)
	}
}

// TestPlansSubTabNetworkClaimReportsEmpty covers the no-claimable
// branch: a plan with no active wave (all tasks already in the final
// bucket) leaves the projection untouched and surfaces a status
// message naming the plan.
func TestPlansSubTabNetworkClaimReportsEmpty(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	if _, err := store.CreatePlan(ctx, project.ID, "empty", "Empty Plan", ""); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("network did not open")
	}

	claimed := pressRune(t, opened, 'c')
	if !strings.Contains(claimed.status, "no task selected") {
		t.Fatalf("status after 'c' on empty plan = %q, want no-task-selected message", claimed.status)
	}
	if claimed.mode == modePlanAssign {
		t.Fatalf("modePlanAssign opened on empty plan; the input must not engage when there is no task to assign")
	}
}

// TestPlansSubTabNetworkRendersBlockerMarkers proves PlanShow's
// in-plan dependency edges surface as inline "← #N" markers on the
// dependent task's line. Out-of-plan edges must not leak through.
func TestPlansSubTabNetworkRendersBlockerMarkers(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Foundation", 1)
	if err != nil {
		t.Fatalf("AddPlanWave() error = %v", err)
	}
	blocker, err := store.CreateTask(ctx, project.ID, "blocker-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask blocker: %v", err)
	}
	dependent, err := store.CreateTask(ctx, project.ID, "dependent-task", "", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask dependent: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, blocker.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan blocker: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, dependent.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan dependent: %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, dependent.ID, blocker.ID); err != nil {
		t.Fatalf("AddTaskDependency: %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("network did not open")
	}

	view := ansi.Strip(opened.View())
	// Intra-wave blocker → dependent edges now surface as a rail
	// terminal (├─ or └─) on the dependent's line directly under the
	// blocker, rather than the prior "← #N" inline annotation. The
	// rail tree is the source of truth for the parent-child edge;
	// duplicating the marker on the dependent's line would be noise.
	blockerStr := "#" + strconv.FormatInt(blocker.ID, 10)
	dependentStr := "#" + strconv.FormatInt(dependent.ID, 10)
	blockerIdx := strings.Index(view, blockerStr)
	dependentIdx := strings.Index(view, dependentStr)
	if blockerIdx < 0 || dependentIdx < 0 {
		t.Fatalf("network missing blocker/dependent ids\n%s", view)
	}
	if blockerIdx >= dependentIdx {
		t.Fatalf("dependent %s should render below blocker %s\n%s", dependentStr, blockerStr, view)
	}
	lineStart := strings.LastIndex(view[:dependentIdx], "\n") + 1
	depLine := view[lineStart:dependentIdx]
	if !strings.Contains(depLine, "└─") && !strings.Contains(depLine, "├─") {
		t.Fatalf("dependent line missing rail glyph (└─ or ├─): %q", depLine)
	}
}

// TestPlansSubTabNetworkScrollsVertically proves the rails+filaments
// outline scrolls its linear row list when the cursor walks past the
// viewport. The old multi-column view used h/l for horizontal slide;
// the new design uses a single vertical scroll offset (planNetworkScroll)
// and `j` keeps the cursor in view by advancing it.
func TestPlansSubTabNetworkScrollsVertically(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "narrow", "Narrow", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for i, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		if _, err := store.AddPlanWave(ctx, project.ID, plan.ID, name, i+1); err != nil {
			t.Fatalf("AddPlanWave %s: %v", name, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.height = 80
	model.width = 80
	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("network did not open")
	}

	// j walks the cursor through the flat row list — including across
	// wave headers, so 6 j presses on a 4-wave plan with no tasks
	// advances the cursor by 3 (capped at the last header). The
	// old multi-axis cursor used h/l to swap waves; the new design
	// folds wave navigation into the same vertical j/k motion.
	cursor := opened
	for i := 0; i < 6; i++ {
		cursor = pressRune(t, cursor, 'j')
	}
	rows := cursor.planNetworkBuildRows()
	if len(rows) == 0 {
		t.Fatalf("plan network produced no rows")
	}
	if cursor.planNetworkCursor != len(rows)-1 {
		t.Fatalf("planNetworkCursor = %d, want %d (last row)", cursor.planNetworkCursor, len(rows)-1)
	}
}

// TestPlansSubTabNetworkRendersDirectionalMarkers proves the outline
// surfaces intra-wave dep relationships through the rail tree (├─/└─)
// and still emits the "Dependencies:" footer + next-claimable hint so
// a reviewer can audit the full edge set without leaving the view.
func TestPlansSubTabNetworkRendersDirectionalMarkers(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	snap := store.Snapshot()
	plan, err := store.CreatePlan(ctx, project.ID, "edges", "Edges", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 1)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	a, _ := store.CreateTask(ctx, project.ID, "alpha", "", domain.Priority(2), "backlog", nil, snap)
	b, _ := store.CreateTask(ctx, project.ID, "bravo", "", domain.Priority(2), "backlog", nil, snap)
	for _, tid := range []int64{a.ID, b.ID} {
		if err := store.AssignTaskToPlan(ctx, project.ID, tid, plan.ID, wave.ID); err != nil {
			t.Fatalf("AssignTaskToPlan: %v", err)
		}
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, b.ID, a.ID); err != nil {
		t.Fatalf("AddTaskDependency: %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.height = 40
	model.width = 160
	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	view := ansi.Strip(opened.View())

	// New design: intra-wave blocker → dependent surfaces as a rail
	// terminal (└─ when there is only one child) on the dependent's
	// line, not as a "← #N" / "→ #N" text marker.
	bravoStr := "#" + strconv.FormatInt(b.ID, 10)
	bravoIdx := strings.Index(view, bravoStr)
	if bravoIdx < 0 {
		t.Fatalf("network missing bravo id %s\n%s", bravoStr, view)
	}
	lineStart := strings.LastIndex(view[:bravoIdx], "\n") + 1
	depLine := view[lineStart:bravoIdx]
	if !strings.Contains(depLine, "└─") && !strings.Contains(depLine, "├─") {
		t.Fatalf("dependent line missing rail glyph: %q", depLine)
	}
	if !strings.Contains(view, "Dependencies:") {
		t.Fatalf("missing deps footer\n%s", view)
	}
	if !strings.Contains(view, "▶ next claimable:") {
		t.Fatalf("missing next-claimable indicator\n%s", view)
	}
}

// TestPlansSubTabNetworkRendersCriticalPath proves the rails outline
// surfaces every task line and the critical-path chain renders with
// rail glyphs (└─/├─) linking blockers to dependents in DFS order.
// Seeds A → B → C plus an isolated D; D must render but stay rail-less
// because it is not connected to any blocker chain.
func TestPlansSubTabNetworkRendersCriticalPath(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "critical", "Critical", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "Foundation", 1)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	a, _ := store.CreateTask(ctx, project.ID, "alpha", "", domain.Priority(2), "backlog", nil, snap)
	b, _ := store.CreateTask(ctx, project.ID, "bravo", "", domain.Priority(2), "backlog", nil, snap)
	c, _ := store.CreateTask(ctx, project.ID, "charlie", "", domain.Priority(2), "backlog", nil, snap)
	d, _ := store.CreateTask(ctx, project.ID, "delta", "", domain.Priority(2), "backlog", nil, snap)
	for _, tid := range []int64{a.ID, b.ID, c.ID, d.ID} {
		if err := store.AssignTaskToPlan(ctx, project.ID, tid, plan.ID, wave.ID); err != nil {
			t.Fatalf("AssignTaskToPlan #%d: %v", tid, err)
		}
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, b.ID, a.ID); err != nil {
		t.Fatalf("dep B→A: %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, c.ID, b.ID); err != nil {
		t.Fatalf("dep C→B: %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)

	view := ansi.Strip(opened.View())
	// Each task surfaces as a single outline row with its id + title.
	for _, title := range []string{"alpha", "bravo", "charlie", "delta"} {
		if !strings.Contains(view, title) {
			t.Fatalf("task %q missing from outline\n%s", title, view)
		}
	}
	// The blocker chain renders as rail glyphs: bravo and charlie
	// inherit └─ / ├─ prefixes; isolated delta does not.
	railed := strings.Count(view, "└─") + strings.Count(view, "├─")
	if railed < 2 {
		t.Fatalf("chain should produce at least 2 rail glyphs (bravo + charlie), got %d\n%s", railed, view)
	}
	bravoLine := planTestLineFor(t, view, "bravo")
	if !strings.Contains(bravoLine, "└─") && !strings.Contains(bravoLine, "├─") {
		t.Fatalf("bravo missing rail prefix: %q", bravoLine)
	}
	deltaLine := planTestLineFor(t, view, "delta")
	if strings.Contains(deltaLine, "└─") || strings.Contains(deltaLine, "├─") {
		t.Fatalf("isolated delta should not carry a rail glyph: %q", deltaLine)
	}
}

// planTestLineFor returns the single rendered line containing `needle`
// from the stripped view. Test helpers extracted so the rail / chevron
// assertions stay one-liners at the callsite.
func planTestLineFor(t *testing.T, view, needle string) string {
	t.Helper()
	idx := strings.Index(view, needle)
	if idx < 0 {
		t.Fatalf("needle %q not found in view\n%s", needle, view)
	}
	start := strings.LastIndex(view[:idx], "\n") + 1
	end := strings.Index(view[idx:], "\n")
	if end < 0 {
		return view[start:]
	}
	return view[start : idx+end]
}

// TestPlansSubTabNetworkEditsGoalBody covers the in-TUI goal_body
// editor: pressing `e` inside the network view opens a multi-line
// textarea pre-filled with the current goal_body, ctrl+s persists the
// edit via PlanService.UpdateGoalBody, and the model returns to the
// network view with the new body reflected in planNetworkShow. Plans
// content is sqlite-only — no $EDITOR shell-out, no tempfile.
func TestPlansSubTabNetworkEditsGoalBody(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "original goal")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	if !opened.planNetworkOpen {
		t.Fatalf("network did not open")
	}

	editing := pressRune(t, opened, 'e')
	if editing.mode != modePlanGoal {
		t.Fatalf("after 'e': mode = %d, want modePlanGoal", editing.mode)
	}
	if editing.planGoalEditingID != plan.ID {
		t.Fatalf("planGoalEditingID = %d, want %d", editing.planGoalEditingID, plan.ID)
	}
	if got := editing.commentInput.Value(); got != "original goal" {
		t.Fatalf("textarea prefill = %q, want %q", got, "original goal")
	}

	editing.commentInput.SetValue("rewritten goal body")
	saved, _ := editing.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	savedModel := saved.(Model)
	if savedModel.mode != modeNormal {
		t.Fatalf("after ctrl+s: mode = %d, want modeNormal", savedModel.mode)
	}
	if savedModel.planGoalEditingID != 0 {
		t.Fatalf("planGoalEditingID after save = %d, want 0", savedModel.planGoalEditingID)
	}
	if savedModel.planNetworkShow.Plan.GoalBody != "rewritten goal body" {
		t.Fatalf("planNetworkShow.Plan.GoalBody = %q, want %q",
			savedModel.planNetworkShow.Plan.GoalBody, "rewritten goal body")
	}

	stored, err := store.GetPlanBySlug(ctx, project.ID, "rollout")
	if err != nil {
		t.Fatalf("GetPlanBySlug() error = %v", err)
	}
	if stored.GoalBody != "rewritten goal body" {
		t.Fatalf("sqlite goal_body = %q, want %q", stored.GoalBody, "rewritten goal body")
	}
}

// TestPlansSubTabNetworkGoalEditorCancels confirms esc aborts the
// goal_body edit without touching sqlite — the editor never gets
// confused into accidentally clearing the body when the user backs out.
func TestPlansSubTabNetworkGoalEditorCancels(t *testing.T) {
	ctx := activity.WithAgent(context.Background(), "tui", "tui", "human", "")
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	plan, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "keep me")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	opened := pressKey(t, got, tea.KeyEnter)
	editing := pressRune(t, opened, 'e')
	editing.commentInput.SetValue("typo I want to throw away")
	cancelled := pressKey(t, editing, tea.KeyEsc)
	if cancelled.mode != modeNormal {
		t.Fatalf("after esc: mode = %d, want modeNormal", cancelled.mode)
	}

	stored, err := store.GetPlanBySlug(ctx, project.ID, "rollout")
	if err != nil {
		t.Fatalf("GetPlanBySlug() error = %v", err)
	}
	if stored.GoalBody != "keep me" {
		t.Fatalf("sqlite goal_body after cancel = %q, want unchanged %q", stored.GoalBody, "keep me")
	}
	_ = plan
}

// TestPlansSubTabEmptyState confirms the empty-state hint renders when
// the project has no plans yet — covers the early-return branch in
// renderPlans so the panel never collapses to a blank surface.
func TestPlansSubTabEmptyState(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	if got.sub != subPlans {
		t.Fatalf("third '/': sub = %d, want subPlans", got.sub)
	}
	view := ansi.Strip(got.View())
	if !strings.Contains(view, "No plans yet") {
		t.Fatalf("plans view missing empty-state hint\n%s", view)
	}
}
