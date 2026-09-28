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
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/plannetwork"
	"omakiten/internal/tui/screens/taskdetail"
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	table := ansi.Strip(pressStringKey(t, model, "/").View())
	if !strings.Contains(table, "TASKS · 2") {
		t.Fatalf("table missing task count\n%s", table)
	}

	graphModel := pressStringKey(t, pressStringKey(t, model, "/"), "/")
	graph := ansi.Strip(graphModel.View())
	if !strings.Contains(graph, "DEPENDENCY GRAPH · 1") {
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
	finishActivityLog(t, store, ctx, logID, 12)

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 180
	assertWideModelViews(t, model, longTitle)
}

func finishActivityLog(t *testing.T, store *snapstore.Store, ctx context.Context, id int64, duration int) {
	t.Helper()
	if err := store.FinishActivityLog(ctx, id, "ok", duration, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}
}

func widestPanelWidth(view string) int {
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

func assertWideModelViews(t *testing.T, model Model, longTitle string) {
	t.Helper()
	table := ansi.Strip(pressStringKey(t, model, "/").View())
	if !strings.Contains(table, longTitle) {
		t.Fatalf("wide table truncated task title\n%s", table)
	}
	logs := ansi.Strip(pressStringKey(t, pressRune(t, model, '2'), "/").View())
	for _, want := range []string{"app.ProjectService.Init", "ACTIVITY · 2"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("wide logs missing %q\n%s", want, logs)
		}
	}
	for _, header := range []string{"TIME", "TYPE", "ENTITY", "WHO", "DETAIL"} {
		if !strings.Contains(logs, header) {
			t.Fatalf("wide logs missing 5-column header %q\n%s", header, logs)
		}
	}
	tableWidth, logsWidth := widestPanelWidth(table), widestPanelWidth(logs)
	if tableWidth == 0 || logsWidth == 0 || tableWidth != logsWidth {
		t.Fatalf("table/log panel widths = %d/%d, want matching non-zero widths\nTABLE:\n%s\nLOGS:\n%s", tableWidth, logsWidth, table, logs)
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressRune(t, model, '2')
	got = pressRune(t, got, '/')
	if got.top != topStats || got.sub != subStatsLogs {
		t.Fatalf("(top, sub) = (%d, %d), want (topStats, subStatsLogs)", got.top, got.sub)
	}
	view := ansi.Strip(got.View())
	// SummarizeEvent renders the tool_call row as
	// `<source>/<tool_name> [status] <duration>ms`, so the operation
	// + source + status all surface inside the DETAIL column. The
	// TYPE column carries EventDef.Display from the YAML-loaded
	// registry (Phase 3 #355).
	for _, want := range []string{"app.TaskService.Add", "CLI tool call", "ok"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	// The Logs inspector renders the new event_row through
	// SummarizeEvent, which renders tool_call rows as
	// `<source>/<tool_name> [status] <ms>ms`. The TYPE column
	// carries the event_type (mcp.tool_call), the WHO column the
	// source (mcp).
	for _, want := range []string{"app.CommentService.Add", "mcp"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

	got, _ := driveRealtimeTick(t, model)
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	if !got.inTaskDetail() {
		t.Fatal("task detail did not open")
	}
	if got.taskDetailScreen.Payload().Task.ID != task.ID {
		t.Fatalf("taskID = %d, want %d", got.taskDetailScreen.Payload().Task.ID, task.ID)
	}
	view := got.View()
	for _, hidden := range []string{"01 // BOARD", "02 // TABLE", "03 // GRAPH", "04 // CONFIG"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("View() contains hidden task-screen tab %q\n%s", hidden, view)
		}
	}
	plain := stripANSI(view)
	// The details zone holds a window of its own since the screenlayout
	// migration (#2425), so its tail is REACHABLE from that zone rather than
	// unconditionally on the first frame. Paging it down brings the description
	// into the same view as everything else asserted here.
	plain += "\n" + taskDetailZoneSweep(t, got)
	for _, want := range []string{
		"▸ TASK · #",
		"TITLE",
		"Existing task",
		"DESCRIPTION",
		"First line",
		"Second line",
		"ACTIVITY · 2",
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'c')
	if got.taskDetailScreen.State().Mode != taskdetail.ModeComment {
		t.Fatalf("detail mode = %v, want comment", got.taskDetailScreen.State().Mode)
	}
	view := got.View()
	for _, want := range []string{"ACTIVITY · 0", "NEW COMMENT", "enter saves", "alt+enter/shift+enter", "newline"} {
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
	assertMultilineCommentView(t, got, comments[0].CreatedAt)
}

func assertMultilineCommentView(t *testing.T, model Model, createdAt string) {
	t.Helper()
	view := model.View()
	plainView := stripANSI(view)
	for _, want := range []string{"ACTIVITY · 1", "human", createdAt, "First line", "Second line", "Third line"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := sendText(t, pressRune(t, model, 'n'), "Created from TUI")
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.TaskForm {
		t.Fatalf("screenStack = %v, want task form", got.screenStack)
	}
	count, err := store.TaskCount(ctx, project.ID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("TaskCount() = %d, want 0 before save", count)
	}

	got = pressKey(t, got, tea.KeyTab)
	got = sendText(t, got, "First line")
	got = pressAltKey(t, got, tea.KeyEnter)
	got = sendText(t, got, "Second line")
	got = pressKey(t, got, tea.KeyTab)
	got = pressKey(t, got, tea.KeyRight)
	if got.taskFormScreen.Values().Priority != "3" {
		t.Fatalf("task priority = %q, want high (id 3)", got.taskFormScreen.Values().Priority)
	}
	got = pressKey(t, got, tea.KeyCtrlS)
	assertCreatedTask(t, got, store, ctx, project.ID)
}

func assertCreatedTask(t *testing.T, model Model, store *snapstore.Store, ctx context.Context, projectID int64) {
	t.Helper()
	if !model.inTaskDetail() || model.mode != modeNormal || model.boardScreen.Column() != 0 || model.boardScreen.Card() != 0 {
		t.Fatalf("created task model did not return to the expected detail selection")
	}
	task, ok := model.selectedTask()
	if !ok {
		t.Fatalf("selectedTask() ok = false, want true")
	}
	if task.Title != "Created from TUI" || task.Description != "First line\nSecond line" || task.Priority != domain.Priority(3) || task.BucketKey != "backlog" {
		t.Fatalf("selected task = %#v, want created task in backlog", task)
	}
	count, err := store.TaskCount(ctx, projectID)
	if err != nil {
		t.Fatalf("TaskCount() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("TaskCount() = %d, want 1", count)
	}
	view := model.View()
	painted := stripANSI(view) + "\n" + taskDetailZoneSweep(t, model)
	for _, want := range []string{"TITLE", "Created from TUI", "BUCKET", "backlog", "PRIORITY", "high", "BLOCKERS", "COMMENTS", "DESCRIPTION", "First line", "Second line"} {
		if !strings.Contains(painted, want) {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'e')
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.TaskForm {
		t.Fatalf("screenStack = %v, want task form", got.screenStack)
	}
	got = pressBackspace(t, got, len("Old title"))
	got = sendText(t, got, "New title")
	got = pressKey(t, got, tea.KeyTab)
	got = pressBackspace(t, got, len("Old description"))
	got = sendText(t, got, "Line one")
	got = pressKey(t, got, tea.KeyEnter)
	got = sendText(t, got, "Line two")
	got = pressKey(t, got, tea.KeyCtrlS)

	if !got.inTaskDetail() {
		t.Fatal("task detail did not remain open after edit")
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

	model := newBlockerPickerModel(t, ctx, project, store)

	got := pressKey(t, model, tea.KeyDown)
	got = pressKey(t, got, tea.KeyEnter)
	if !got.inTaskDetail() || got.taskDetailScreen.Payload().Task.ID != blocked.ID {
		t.Fatalf("task detail = %v taskID = %d, want blocked task #%d", got.screenStack, got.taskDetailScreen.Payload().Task.ID, blocked.ID)
	}
	got = pressRune(t, got, 'e')
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.TaskForm {
		t.Fatalf("screenStack = %v, want task form", got.screenStack)
	}
	got = pressKey(t, got, tea.KeyCtrlB)
	assertBlockerPickerOpen(t, got)
	got = pressKey(t, got, tea.KeyEsc)
	assertBlockerPickerClosed(t, got)
	got = pressKey(t, got, tea.KeyCtrlB)
	got = pressKey(t, pressKey(t, got, tea.KeySpace), tea.KeyCtrlS)
	assertBlockerPickerClosed(t, got)
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
	for _, want := range []string{"BLOCKERS · 1", "Design dependency", "backlog · normal"} {
		if !strings.Contains(view, want) {
			t.Fatalf("task view missing blocker detail %q\n%s", want, view)
		}
	}
}

func newBlockerPickerModel(t *testing.T, ctx context.Context, project domain.Project, store *snapstore.Store) Model {
	t.Helper()
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	return model
}

func assertBlockerPickerOpen(t *testing.T, model Model) {
	t.Helper()
	if !model.inTaskFormBlockerOverlay() || model.taskDetailScreen.State().Mode != taskdetail.ModeBlockers {
		t.Fatalf("task detail mode = %v stack = %v, want Task Form blocker overlay", model.taskDetailScreen.State().Mode, model.screenStack)
	}
	if descriptor, ok := model.activeScreenDescriptor(); !ok || descriptor.ID != screenhost.TaskDetail {
		t.Fatalf("blocker picker active screen = %v, %v; want task detail", descriptor.ID, ok)
	}
	if !strings.Contains(model.View(), "Design dependency") {
		t.Fatalf("blocker picker view missing candidate\n%s", model.View())
	}
	if titles := model.currentHelpTitles(); len(titles) != 1 || titles[0] != "blocker_picker" {
		t.Fatalf("currentHelpTitles() = %v, want blocker_picker", titles)
	}
	if footer := model.footerTokens(); len(footer) == 0 || footer[0].key != "space" {
		t.Fatalf("footerTokens() = %#v, want hosted blocker picker footer", footer)
	}
}

func assertBlockerPickerClosed(t *testing.T, model Model) {
	t.Helper()
	if model.inTaskFormBlockerOverlay() || model.taskDetailScreen.State().Mode != taskdetail.ModeNormal {
		t.Fatalf("task detail mode = %v stack = %v, want blocker overlay closed", model.taskDetailScreen.State().Mode, model.screenStack)
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressStringKey(t, model, "right")
	if got.boardScreen.Column() != 1 {
		t.Fatalf("column = %d, want 1 (dev column)", got.boardScreen.Column())
	}
	got = pressRune(t, got, 'm')
	if !got.boardScreen.MoveMode() {
		t.Fatalf("moveMode = false, want true")
	}
	got = pressStringKey(t, got, "left")

	if got.boardScreen.Column() != 1 {
		t.Fatalf("column after blocked move = %d, want 1 (task should not move visually)", got.boardScreen.Column())
	}
	if got.boardScreen.MoveMode() {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	assertWrappedTaskGrid(t, got)
}

func assertWrappedTaskGrid(t *testing.T, model Model) {
	t.Helper()
	plain := ansi.Strip(model.View())
	lines := strings.Split(plain, "\n")
	start, end := findTaskGridRows(t, lines, plain)
	edge := strings.Index(lines[start], "┐")
	if edge < 0 {
		t.Fatalf("grid top border has no right corner\n%s", plain)
	}
	column := len([]rune(lines[start][:edge]))
	for i := start; i <= end; i++ {
		row := []rune(lines[i])
		if column >= len(row) {
			t.Fatalf("grid row %d is %d cells wide, the box's right edge is at %d\n%s", i, len(row), column, plain)
		}
		if !strings.ContainsRune("┐┤┘│┴", row[column]) {
			t.Fatalf("grid row %d carries %q where the box's right edge should be\n%s", i, string(row[column]), plain)
		}
	}
	painted := plain + "\n" + taskDetailZoneSweep(t, model)
	for _, want := range []string{"TITLE", "DESCRIPTION", "comportamento", "projeto"} {
		if !strings.Contains(painted, want) {
			t.Fatalf("View() missing %q\n%s", want, plain)
		}
	}
}

func findTaskGridRows(t *testing.T, lines []string, plain string) (int, int) {
	t.Helper()
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "┌") && strings.Contains(line, "┐") {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("task view grid top border not found\n%s", plain)
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if !strings.Contains(lines[i], "│") {
			break
		}
		end = i
	}
	if end == -1 {
		t.Fatalf("task view grid has no rows under its top border\n%s", plain)
	}
	return start, end
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	if got.boardScreen.Column() != 0 {
		t.Fatalf("after %d rights column = %d, want 0 (wrap)", n, got.boardScreen.Column())
	}

	// Left from the first lane wraps to the last.
	got = pressStringKey(t, got, "left")
	if got.boardScreen.Column() != n-1 {
		t.Fatalf("left from first column = %d, want %d (wrap)", got.boardScreen.Column(), n-1)
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 50
	// Settings/general first, then advance to Settings/laws.
	got := pressRune(t, model, '4')
	got = pressStringKey(t, got, "/")

	view := ansi.Strip(got.View())
	if !strings.Contains(view, "LAWS") {
		t.Fatalf("Settings › Laws column header missing on narrow terminal:\n%s", view)
	}
	for _, leaked := range []string{"PERSONAS", "SKILLS", "TEMPLATES", "TAGS"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressStringKey(t, model, "?")
	view := ansi.Strip(got.View())
	for _, want := range []string{"KEYBINDINGS · CURRENT CONTEXT", "GLOBAL", "TASKS · BOARD LENS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("context help missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "SKILL PICKER") {
		t.Fatalf("context help rendered unrelated skill picker group\n%s", view)
	}

	got = pressRune(t, got, 'a')
	view = ansi.Strip(got.View())
	for _, want := range []string{"KEYBINDINGS · ALL CONTEXTS", "SKILL PICKER"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := sendText(t, pressRune(t, model, 'n'), "Draft title")
	got = pressKey(t, got, tea.KeyEsc)
	if got.inTaskDetail() {
		t.Fatal("task detail opened while cancelling create")
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
// the per-project header renders the top zones as `01 // TASKS`,
// `02 // STATS`, `03 // STUDIO`, `04 // SETTINGS`, and surfaces a sub-menu
// strip when the active top has more than one sub. The strip is suppressed
// on Settings (single sub).
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 200

	tasksHeader := ansi.Strip(model.View())
	assertHeaderContains(t, tasksHeader, "tasks", "01 // TASKS", "02 // STATS", "03 // STUDIO", "04 // SETTINGS", "// board", "// table", "// graph")

	statsModel := pressRune(t, model, '2')
	statsHeader := ansi.Strip(statsModel.View())
	assertHeaderContains(t, statsHeader, "stats", "01 // TASKS", "02 // STATS", "03 // STUDIO", "04 // SETTINGS", "// general", "// logs")
	assertHeaderMissing(t, statsHeader, "stats", "// board", "// graph")

	settingsModel := pressRune(t, model, '4')
	settingsHeader := ansi.Strip(settingsModel.View())
	assertHeaderContains(t, settingsHeader, "settings", "01 // TASKS", "02 // STATS", "03 // STUDIO", "04 // SETTINGS", "// general", "// laws", "// personas", "// skills", "// templates", "// tags")
	assertHeaderMissing(t, settingsHeader, "settings", "// board", "// table", "// graph")
}

func assertHeaderContains(t *testing.T, header, name string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(header, value) {
			t.Fatalf("%s header missing %q\n%s", name, value, header)
		}
	}
}

func assertHeaderMissing(t *testing.T, header, name string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(header, value) {
			t.Fatalf("%s header leaked %q\n%s", name, value, header)
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

	got = pressRune(t, model, '4')
	if got.top != topSettings || got.sub != subSettingsGeneral {
		t.Fatalf("after '4': (top, sub) = (%d, %d), want (topSettings, subSettingsGeneral)", got.top, got.sub)
	}
	got = pressStringKey(t, got, "/")
	if got.top != topSettings || got.sub != subSettingsLaws {
		t.Fatalf("'/' on Settings/general should advance to Settings/laws: (top, sub) = (%d, %d)", got.top, got.sub)
	}
}

// TestShiftTabCyclesTops pins the tops ring: shift+tab advances
// Tasks → Stats → Studio → Settings and wraps, and tab on the board
// does not steal that ring (board OwnsKey does not claim tab).
func TestShiftTabCyclesTops(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	if model.top != topTasks || model.sub != subBoard {
		t.Fatalf("start: (top, sub) = (%d, %d), want (topTasks, subBoard)", model.top, model.sub)
	}

	unchanged := pressKey(t, model, tea.KeyTab)
	if unchanged.top != topTasks || unchanged.sub != subBoard {
		t.Fatalf("tab on board mutated nav: (top, sub) = (%d, %d), want (topTasks, subBoard)", unchanged.top, unchanged.sub)
	}

	got := pressKey(t, model, tea.KeyShiftTab)
	if got.top != topStats || got.sub != subStatsGeneral {
		t.Fatalf("after shift+tab from board: (top, sub) = (%d, %d), want (topStats, subStatsGeneral)", got.top, got.sub)
	}
	got = pressKey(t, got, tea.KeyShiftTab)
	if got.top != topStudio {
		t.Fatalf("after second shift+tab: top = %d, want topStudio", got.top)
	}
	got = pressKey(t, got, tea.KeyShiftTab)
	if got.top != topSettings {
		t.Fatalf("after third shift+tab: top = %d, want topSettings", got.top)
	}
	got = pressKey(t, got, tea.KeyShiftTab)
	if got.top != topTasks || got.sub != subBoard {
		t.Fatalf("after wrap shift+tab: (top, sub) = (%d, %d), want (topTasks, subBoard)", got.top, got.sub)
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	got = pressRune(t, got, '4')
	if got.top != topSettings || got.sub != subSettingsGeneral {
		t.Fatalf("'4' should jump to Settings/general: (top, sub) = (%d, %d)", got.top, got.sub)
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 200
	view := ansi.Strip(model.View())
	for _, want := range []string{"00 // HOME", "│", "01 // TASKS", "02 // STATS", "03 // STUDIO", "04 // SETTINGS"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter) // open task view
	if !got.inTaskDetail() {
		t.Fatal("task detail must open before deleting")
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	// Cursor starts in the first column (backlog) which is empty; move
	// right to land on the dev column where the task lives.
	got := pressKey(t, model, tea.KeyRight)
	got = pressKey(t, got, tea.KeyEnter)
	if !got.inTaskDetail() {
		t.Fatal("task detail did not open")
	}
	got = pressRune(t, got, 'e')
	if !got.inTaskDetail() || got.screenStack[len(got.screenStack)-1] != screenhost.TaskDetail {
		t.Fatalf("task detail changed after blocked edit: %v", got.screenStack)
	}
	if !strings.Contains(got.status, "policy:") || !strings.Contains(got.status, "task.edit") {
		t.Fatalf("status = %q, want a policy hint mentioning task.edit", got.status)
	}
}

// TestModelBlocksTaskDeleteArmWhenBucketForbids mirrors the edit gate for
// the destructive `d` arm. The first press in a forbidden bucket should
// surface the policy hint and skip the arm — the user should not see a
// "Confirm delete..." prompt for an action that cannot succeed.
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store)}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter) // open task view
	got = pressKey(t, got, tea.KeyTab)      // details -> activity
	if got.taskDetailScreen.State().Focus != taskdetail.FocusActivity {
		t.Fatalf("task focus = %v, want activity", got.taskDetailScreen.State().Focus)
	}
	// Activity feed is chronological: events[0] is the task.created system
	// event, events[1] is the comment we just added. Advance past the
	// system event so Enter lands on the comment.
	got = pressStringKey(t, got, "J")
	if got.taskDetailScreen.State().ActivityCursor != 1 {
		t.Fatalf("activity cursor = %d, want 1 (comment row)", got.taskDetailScreen.State().ActivityCursor)
	}
	got = pressKey(t, got, tea.KeyEnter)
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.CommentDetail || got.commentDetailScreen.Payload().Comment.ID != comment.ID {
		t.Fatalf("comment route/payload = %v/%+v", got.screenStack, got.commentDetailScreen.Payload())
	}

	armed := pressRune(t, got, 'd')
	if !armed.commentDetailScreen.DeleteArmed() {
		t.Fatalf("comment delete was not armed: footer=%v", armed.commentDetailScreen.Footer(armed.screenFrame()))
	}

	confirmed := pressRune(t, armed, 'd')
	if len(confirmed.screenStack) > 0 && confirmed.screenStack[len(confirmed.screenStack)-1] == screenhost.CommentDetail {
		t.Fatalf("comment route remained after delete: %v", confirmed.screenStack)
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab) // details -> activity
	// Skip past the chronologically-first task.created system event so
	// Enter on the activity card opens the comment we want to edit.
	got = pressStringKey(t, got, "J")
	got = pressKey(t, got, tea.KeyEnter)
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.CommentDetail {
		t.Fatalf("comment route stack = %v", got.screenStack)
	}
	got = pressRune(t, got, 'e')
	if got.commentDetailScreen.Mode() != commentdetail.ModeEdit || got.commentDetailScreen.Payload().Comment.ID != comment.ID {
		t.Fatalf("comment edit state = mode %v payload %+v", got.commentDetailScreen.Mode(), got.commentDetailScreen.Payload())
	}
	if got.commentDetailScreen.Value() != "Original body" {
		t.Fatalf("comment value = %q, want pre-filled with original body", got.commentDetailScreen.Value())
	}

	// Erase the original body via repeated backspace so we test caret
	// editing instead of a blunt SetValue. bubbles' textarea handles
	// rune-aware backspace at the cursor, mirroring real-terminal UX.
	got = pressBackspace(t, got, len("Original body"))
	got = sendText(t, got, "Rewritten body")
	got = pressKey(t, got, tea.KeyCtrlS)

	if got.commentDetailScreen.Mode() != commentdetail.ModeRead || len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.CommentDetail {
		t.Fatalf("after save: mode=%v stack=%v", got.commentDetailScreen.Mode(), got.screenStack)
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
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return foldViewChangeRefresh(t, got, cmd)
}

func pressKey(t *testing.T, model Model, key tea.KeyType) Model {
	t.Helper()
	updated, cmd := model.Update(tea.KeyMsg{Type: key})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return foldViewChangeRefresh(t, got, cmd)
}

func pressAltKey(t *testing.T, model Model, key tea.KeyType) Model {
	t.Helper()
	updated, cmd := model.Update(tea.KeyMsg{Type: key, Alt: true})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return foldViewChangeRefresh(t, got, cmd)
}

func pressStringKey(t *testing.T, model Model, key string) Model {
	t.Helper()
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want Model", updated)
	}
	return foldViewChangeRefresh(t, got, cmd)
}

// foldViewChangeRefresh keeps the synchronous test contract after
// Update started returning a tea.Cmd for the view-change refresh path
// (perf/tui-refresh-async). It only invokes the cmd when it is the
// view-change refresh cmd this package controls — every other cmd
// (write-flow IO, picker pickups, tick reschedulers) is left
// unevaluated so the helper does not pay for IO the production runtime
// would dispatch asynchronously.
func foldViewChangeRefresh(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	if !isViewChangeRefreshCmd(cmd) {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	updated, _ := m.Update(msg)
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(refreshAfterViewChangeMsg) returned %T, want Model", updated)
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
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", SkillRepertoire: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"}}
	return bundle
}

// tuiPermissiveBundle loads the all-allow fixture for tests that exercise
// the success path of policy-gated keybindings.
func tuiPermissiveBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "permissive.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", SkillRepertoire: []string{"go"}}}
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
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", SkillRepertoire: []string{"go"}}}
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

func openPlansList(t *testing.T, model Model) Model {
	t.Helper()
	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	if got.sub != subPlans {
		t.Fatalf("third '/': sub = %d, want subPlans", got.sub)
	}
	return got
}

func openPlanNetwork(t *testing.T, model Model) Model {
	t.Helper()
	return pressKey(t, openPlansList(t, model), tea.KeyEnter)
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := openPlansList(t, model)
	assertPlansRollups(t, got)
}

func assertPlansRollups(t *testing.T, model Model) {
	t.Helper()
	if len(model.plansScreen.Rollups()) != 2 {
		t.Fatalf("len(plans) = %d, want 2", len(model.plansScreen.Rollups()))
	}
	view := ansi.Strip(model.View())
	for _, want := range []string{"PLANS", "rollout-a", "rollout-b", "Wave 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("plans view missing %q\n%s", want, view)
		}
	}
	advanced := pressRune(t, model, 'j')
	if advanced.plansScreen.Cursor() != 1 {
		t.Fatalf("after 'j': plansCursor = %d, want 1", advanced.plansScreen.Cursor())
	}
	back := pressRune(t, advanced, 'k')
	if back.plansScreen.Cursor() != 0 {
		t.Fatalf("after 'k': plansCursor = %d, want 0", back.plansScreen.Cursor())
	}
	goal := pressRune(t, back, 'f')
	if len(goal.screenStack) != 1 || goal.screenStack[0] != screenhost.PlanGoal {
		t.Fatalf("goal route stack = %v, want [%s]", goal.screenStack, screenhost.PlanGoal)
	}
	if descriptor, ok := goal.activeScreenDescriptor(); !ok || descriptor.ID != screenhost.PlanGoal {
		t.Fatalf("active goal descriptor = %+v/%v", descriptor, ok)
	}
	closed := pressKey(t, goal, tea.KeyEsc)
	if len(closed.screenStack) != 0 || closed.plansScreen.Cursor() != 0 {
		t.Fatalf("goal pop stack/cursor = %v/%d", closed.screenStack, closed.plansScreen.Cursor())
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	got := openPlansList(t, model)

	assertPlanNetworkView(t, pressKey(t, got, tea.KeyEnter))
}

func assertPlanNetworkView(t *testing.T, opened Model) {
	t.Helper()
	if !opened.inPlanNetwork() || len(opened.planNetworkScreen.Show().Waves) != 2 {
		t.Fatalf("plan network open/waves = %v/%d, want true/2", opened.inPlanNetwork(), len(opened.planNetworkScreen.Show().Waves))
	}
	view := ansi.Strip(opened.View())
	for _, want := range []string{"// PLAN · rollout", "W1", "W2", "‹active›", "foundation-task", "migration-task"} {
		if !strings.Contains(view, want) {
			t.Fatalf("network missing %q\n%s", want, view)
		}
	}
	startCursor := opened.planNetworkScreen.Cursor()
	advanced := pressRune(t, opened, 'j')
	if advanced.planNetworkScreen.Cursor() != startCursor+1 {
		t.Fatalf("after 'j': planNetworkCursor = %d, want %d", advanced.planNetworkScreen.Cursor(), startCursor+1)
	}
	back := pressRune(t, advanced, 'k')
	if back.planNetworkScreen.Cursor() != startCursor {
		t.Fatalf("after 'k': planNetworkCursor = %d, want %d", back.planNetworkScreen.Cursor(), startCursor)
	}
	closed := pressKey(t, opened, tea.KeyEsc)
	if closed.inPlanNetwork() {
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	assertPlanAssignmentFlow(t, openPlansList(t, model), store, ctx, project.ID, task.ID, snap)
}

func assertPlanAssignmentFlow(t *testing.T, plans Model, store *snapstore.Store, ctx context.Context, projectID, taskID int64, snap domain.BucketResolver) {
	t.Helper()
	opened := pressKey(t, plans, tea.KeyEnter)
	if !opened.inPlanNetwork() {
		t.Fatalf("network did not open")
	}
	editing := pressRune(t, pressRune(t, opened, 'j'), 'c')
	if editing.planNetworkScreen.Mode() != plannetwork.ModeAssign || editing.planNetworkScreen.AssignTaskID() != taskID {
		t.Fatalf("assignment mode/target = %v/%d, want assign/%d", editing.planNetworkScreen.Mode(), editing.planNetworkScreen.AssignTaskID(), taskID)
	}
	typed := editing
	for _, r := range "alice" {
		typed = pressRune(t, typed, r)
	}
	submitted := pressKey(t, typed, tea.KeyEnter)
	if submitted.mode != modeNormal {
		t.Fatalf("after submit mode = %v, want modeNormal", submitted.mode)
	}
	tasks, err := store.ListTasks(ctx, projectID, domain.TaskFilter{}, snap)
	if err != nil || len(tasks) != 1 || tasks[0].BucketKey != "backlog" {
		t.Fatalf("assigned task state = %#v, err=%v; want one backlog task", tasks, err)
	}
	view := ansi.Strip(submitted.View())
	for _, want := range []string{"@alice", "assigned"} {
		if !strings.Contains(view, want) {
			t.Fatalf("network view missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "in-progress") {
		t.Fatalf("network view should not show in-progress for a backlog assignment\n%s", view)
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
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
	if !opened.inPlanNetwork() {
		t.Fatalf("network did not open")
	}

	claimed := pressRune(t, opened, 'c')
	if !strings.Contains(claimed.status, "no task selected") {
		t.Fatalf("status after 'c' on empty plan = %q, want no-task-selected message", claimed.status)
	}
	if claimed.planNetworkScreen.Mode() == plannetwork.ModeAssign {
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	assertPlanDependencyRail(t, openPlanNetwork(t, model), blocker.ID, dependent.ID)
}

func assertPlanDependencyRail(t *testing.T, opened Model, blockerID, dependentID int64) {
	t.Helper()
	view := ansi.Strip(opened.View())
	blockerStr := "#" + strconv.FormatInt(blockerID, 10)
	dependentStr := "#" + strconv.FormatInt(dependentID, 10)
	blockerIdx := strings.Index(view, blockerStr)
	dependentIdx := strings.Index(view, dependentStr)
	if blockerIdx < 0 || dependentIdx < 0 || blockerIdx >= dependentIdx {
		t.Fatalf("network dependency order invalid for %s/%s\n%s", blockerStr, dependentStr, view)
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
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
	if !opened.inPlanNetwork() {
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
	rowCount := cursor.planNetworkScreen.RowCount()
	if rowCount == 0 {
		t.Fatalf("plan network produced no rows")
	}
	if got := cursor.planNetworkScreen.Cursor(); got != rowCount-1 {
		t.Fatalf("plan network cursor = %d, want %d", got, rowCount-1)
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.height = 40
	model.width = 160
	assertDirectionalPlanView(t, openPlanNetwork(t, model), b.ID)
}

func assertDirectionalPlanView(t *testing.T, opened Model, taskID int64) {
	t.Helper()
	view := ansi.Strip(opened.View())
	line := planTestLineFor(t, view, "#"+strconv.FormatInt(taskID, 10))
	if !strings.Contains(line, "└─") && !strings.Contains(line, "├─") {
		t.Fatalf("dependent line missing rail glyph: %q", line)
	}
	for _, want := range []string{"Dependencies:", "▶ next claimable:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %s footer\n%s", want, view)
		}
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.height = 40
	model.width = 160

	assertCriticalPathView(t, openPlanNetwork(t, model))
}

func assertCriticalPathView(t *testing.T, opened Model) {
	t.Helper()
	view := ansi.Strip(opened.View())
	for _, title := range []string{"alpha", "bravo", "charlie", "delta"} {
		if !strings.Contains(view, title) {
			t.Fatalf("task %q missing from outline\n%s", title, view)
		}
	}
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

	_, err = store.CreatePlan(ctx, project.ID, "rollout", "Rollout", "original goal")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
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
	if !opened.inPlanNetwork() {
		t.Fatalf("network did not open")
	}

	editing := pressRune(t, opened, 'e')
	if editing.planNetworkScreen.Mode() != plannetwork.ModeGoal {
		t.Fatalf("after 'e': mode = %d, want goal editor", editing.planNetworkScreen.Mode())
	}
	if got := editing.planNetworkScreen.EditorValue(); got != "original goal" {
		t.Fatalf("textarea prefill = %q, want %q", got, "original goal")
	}

	editing.planNetworkScreen = editing.planNetworkScreen.WithEditorValue("rewritten goal body")
	saved, _ := editing.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	savedModel := saved.(Model)
	if savedModel.planNetworkScreen.Mode() != plannetwork.ModeBrowse {
		t.Fatalf("after ctrl+s: mode = %d, want browse", savedModel.planNetworkScreen.Mode())
	}
	if savedModel.planNetworkScreen.Show().Plan.GoalBody != "rewritten goal body" {
		t.Fatalf("plan goal = %q, want %q", savedModel.planNetworkScreen.Show().Plan.GoalBody, "rewritten goal body")
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
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
		Plans:        store,
		Cache:        runtimecache.InstallWithStoreSnap(0, store, snap),
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

// taskDetailZoneSweep drives the task detail body down each of its zones and
// returns every frame it painted, joined.
//
// The three zones hold independent windows since the screenlayout migration
// (#2425), so "the screen paints X" is a claim about what is REACHABLE from the
// zone that owns X rather than about the first frame. This is the host-level
// twin of the per-zone reachability the property in
// content_reachability_test.go drives.
func taskDetailZoneSweep(t *testing.T, model Model) string {
	t.Helper()
	var painted []string
	for zone := 0; zone < 3; zone++ {
		current := model
		for press := 0; press < 12; press++ {
			painted = append(painted, stripANSI(current.View()))
			current = pressKey(t, current, tea.KeyPgDown)
		}
		model = pressKey(t, model, tea.KeyTab)
	}
	return strings.Join(painted, "\n")
}
