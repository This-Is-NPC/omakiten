package tui

import (
	"context"
	"fmt"
	"omakiten/internal/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

func TestActivityCursorMovesAndScrolls(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "with comments", "", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	// Seed enough comments to require scroll inside the activity column.
	for i := 0; i < 10; i++ {
		if _, err := store.AddComment(ctx, project.ID, task.ID, "comment", "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
		Config:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	// Open the task detail view; activity feed is loaded into m.activity.
	got := pressKey(t, model, tea.KeyEnter)
	if got.activityCursor != -1 {
		t.Fatalf("initial activityCursor = %d, want -1", got.activityCursor)
	}

	// First J should land on the first card (cursor=0). Subsequent Js advance
	// — and once they exceed the viewport, activityScroll auto-follows.
	for i := 0; i < 6; i++ {
		got = pressRune(t, got, 'J')
	}
	if got.activityCursor < 5 {
		t.Errorf("activityCursor after 6×J = %d, want >= 5", got.activityCursor)
	}
	// activityScroll is now line-based; we only assert the cursor advanced.
	// Viewport-following is exercised end-to-end via the View output in the
	// expand test below.
}

func TestActivityEnterOpensCommentScreen(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "long body", "", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}

	// Body that exceeds commentCardLineLimit so the cap+hint kicks in.
	longBody := strings.Repeat("line\n", 12)
	comment, err := store.AddComment(ctx, project.ID, task.ID, longBody, "human", nil)
	if err != nil {
		t.Fatalf("AddComment() = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
		Config:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	view := got.View()
	if !strings.Contains(view, "more lines — enter opens") {
		t.Fatalf("collapsed view should show open-detail hint:\n%s", view)
	}

	// Tab switches focus to the activity column so j/k navigate cards and
	// enter opens the comment detail screen. task.created is at index 0 (asc);
	// pressing j once advances to the comment at index 1.
	got = pressKey(t, got, tea.KeyTab)
	got = pressRune(t, got, 'j')
	got = pressKey(t, got, tea.KeyEnter)
	if !got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = false after enter, want true")
	}
	if got.commentScreenID != comment.ID {
		t.Fatalf("commentScreenID = %d, want %d", got.commentScreenID, comment.ID)
	}

	// The dedicated screen renders the full body without the cap hint, with
	// its own kicker and footer hint so the UX context is unmistakable.
	view = got.View()
	if strings.Contains(view, "more lines — enter opens") {
		t.Fatalf("comment screen should drop the cap hint:\n%s", view)
	}
	// kicker uppercases ("// COMMENT · #N"), so assert on the uppercased form.
	if !strings.Contains(view, fmt.Sprintf("COMMENT · #%d", comment.ID)) {
		t.Fatalf("comment screen header missing:\n%s", view)
	}

	// j/k must scroll the body within the comment screen, not move between
	// activity cards (that's the bug we're fixing — long comments need an
	// in-screen scroll so the top and bottom are reachable).
	before := got.commentScreen.Viewport.Scroll
	got = pressRune(t, got, 'j')
	if got.commentScreen.Viewport.Scroll <= before {
		t.Fatalf("j did not advance commentScreen viewport: before=%d after=%d", before, got.commentScreen.Viewport.Scroll)
	}

	// esc returns to the task view, leaving the activity cursor on the same
	// comment so the user lands back where they were.
	got = pressKey(t, got, tea.KeyEsc)
	if got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = true after esc, want false")
	}
	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v after esc, want taskScreenView", got.taskScreen)
	}
}

func TestCommentScreenIgnoresSystemEvents(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "no comments", "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
		Config:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	// Open task view; the only activity event is task.created (a system
	// event). Tab + j puts the cursor on it; Enter must NOT open the
	// comment screen since system events have no body.
	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)
	got = pressKey(t, got, tea.KeyEnter)
	if got.commentScreenOpen {
		t.Fatalf("Enter on a system event opened the comment screen — system events have no body to read")
	}
}

func TestTabTogglesTaskFocus(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "with comment", "", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "hi", "human", nil); err != nil {
		t.Fatalf("AddComment() = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
		Config:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	if got.taskFocus != taskFocusForm {
		t.Fatalf("default focus = %v, want taskFocusForm", got.taskFocus)
	}

	got = pressKey(t, got, tea.KeyTab)
	if got.taskFocus != taskFocusActivity {
		t.Fatalf("focus after tab = %v, want taskFocusActivity", got.taskFocus)
	}
	if got.activityCursor != 0 {
		t.Errorf("activityCursor on entering activity = %d, want 0 (auto-land on first card)", got.activityCursor)
	}

	got = pressKey(t, got, tea.KeyTab)
	if got.taskFocus != taskFocusForm {
		t.Fatalf("focus after second tab = %v, want taskFocusForm", got.taskFocus)
	}
	if got.activityCursor != -1 {
		t.Errorf("activityCursor after returning focus = %d, want -1 (cleared)", got.activityCursor)
	}
}

// TestActivityScrollKeepsFocusedCardVisible reproduces task #74: navigating
// j past the activity viewport with the (pre-fix) sync routine left the
// focused card hidden behind the "▼ N below" hint row because sync compared
// against the raw viewport while renderScrollWindowSplit reserved up to two
// rows for the split hints. Each comment carries a unique body so the test
// can assert the focused one renders inside the visible region instead of
// asserting on activityScroll directly.
func TestActivityScrollKeepsFocusedCardVisible(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "lots of activity", "", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	const total = 30
	for i := 0; i < total; i++ {
		body := fmt.Sprintf("activity-marker-%02d", i)
		if _, err := store.AddComment(ctx, project.ID, task.ID, body, "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
		Config:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 30
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)

	for n := 0; n < total/2; n++ {
		got = pressRune(t, got, 'j')
	}
	if got.activityCursor < 0 {
		t.Fatalf("activityCursor = %d after j×%d, want >= 0", got.activityCursor, total/2)
	}

	events := got.activityForTaskInView(got.taskID)
	if got.activityCursor >= len(events) {
		t.Fatalf("activityCursor = %d out of range (%d events)", got.activityCursor, len(events))
	}
	focused := events[got.activityCursor]
	body := stripANSI(got.View())
	if focused.Body == "" {
		t.Fatalf("focused event has empty body — picked the wrong cursor")
	}
	if !strings.Contains(body, focused.Body) {
		t.Fatalf("focused card %q hidden after navigation; rendered view did not contain it.\n--- view ---\n%s", focused.Body, body)
	}
}

// TestActivityScrollResyncsOnResize covers AC#2 — shrinking the terminal
// height after navigating must keep the focused card visible. Without the
// resize-time sync, `activityScroll` keeps the offset that was valid for
// the old viewport and the focused card slides off-screen.
func TestActivityScrollResyncsOnResize(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "resize repro", "", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	const total = 25
	for i := 0; i < total; i++ {
		body := fmt.Sprintf("resize-marker-%02d", i)
		if _, err := store.AddComment(ctx, project.ID, task.ID, body, "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
		Config:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 50
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)
	for n := 0; n < total/2; n++ {
		got = pressRune(t, got, 'j')
	}

	resized, _ := got.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	got, ok := resized.(Model)
	if !ok {
		t.Fatalf("Update(WindowSizeMsg) returned %T, want Model", resized)
	}

	events := got.activityForTaskInView(got.taskID)
	focused := events[got.activityCursor]
	body := stripANSI(got.View())
	if !strings.Contains(body, focused.Body) {
		t.Fatalf("focused card %q hidden after resize.\n--- view ---\n%s", focused.Body, body)
	}
}

func TestActivityPanelGrowsWithAvailableWidth(t *testing.T) {
	// Skip NewModel — its refresh path needs a fully wired repo set. We only
	// exercise pure sizing math, so a zero-value Model with width set is enough.
	model := Model{}

	model.width = 80
	narrow := model.activityPanelWidth()

	model.width = 200
	wide := model.activityPanelWidth()

	if wide <= narrow {
		t.Fatalf("activityPanelWidth did not grow with width: narrow=%d wide=%d", narrow, wide)
	}
	if narrow < taskCommentsPanelMinWidth {
		t.Errorf("narrow width %d below floor %d", narrow, taskCommentsPanelMinWidth)
	}
	if wide > taskCommentsPanelMaxWidth {
		t.Errorf("wide width %d above ceiling %d", wide, taskCommentsPanelMaxWidth)
	}
}
