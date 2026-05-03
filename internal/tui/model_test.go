package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got := updated.(Model)
	if got.view != 1 {
		t.Fatalf("view = %d, want 1", got.view)
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
	task, err := store.CreateTask(ctx, project.ID, "Existing task", "First line\nSecond line", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	humanComment, err := store.AddComment(ctx, project.ID, task.ID, "Looks good to me.", "human")
	if err != nil {
		t.Fatalf("AddComment(human) error = %v", err)
	}
	agentComment, err := store.AddComment(ctx, project.ID, task.ID, "I can take the next step.", "agent")
	if err != nil {
		t.Fatalf("AddComment(agent) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
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
		"// TASK · #",
		"// TITLE",
		"Existing task",
		"// DESCRIPTION",
		"First line",
		"Second line",
		"// COMMENTS · 2",
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
	task, err := store.CreateTask(ctx, project.ID, "Existing task", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'c')
	if !got.isEmbeddedCommentInput() {
		t.Fatalf("isEmbeddedCommentInput() = false, want true")
	}
	view := got.View()
	for _, want := range []string{"// COMMENTS · 0", "// NEW COMMENT", "enter saves", "alt+enter/shift+enter", "newline"} {
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
	for _, want := range []string{"// COMMENTS · 1", "human", comments[0].CreatedAt, "First line", "Second line", "Third line"} {
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
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
	if task.Title != title || task.Description != description || task.BucketKey != "backlog" {
		t.Fatalf("selected task = %#v, want title %q description %q in backlog", task, title, description)
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
		"normal",
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
	if _, err := store.CreateTask(ctx, project.ID, "Old title", "Old description", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
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
	task, err := store.CreateTask(ctx, project.ID, "Pinned", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask(setup) error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
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

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store}, tuiTestTheme(), token.ApproxCounter{})
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
