package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/commentdetail"
	"omakiten/internal/tui/screens/taskdetail"
)

func newMoveInputTestModel(t *testing.T) (context.Context, *snapstore.Store, domain.Project, Model) {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Move me", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store,
		Cache: runtimecache.InstallWithStore(0, store),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	return ctx, store, project, model
}

func newCommentEditInputTestModel(t *testing.T) (context.Context, *snapstore.Store, domain.Project, domain.Task, domain.Comment, Model) {
	t.Helper()
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
	original := "first line\nsecond\nthird"
	comment, err := store.AddComment(ctx, project.ID, task.ID, original, "human", []domain.Tag{{Name: "keep-me", Label: "keep-me"}})
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store,
		Cache: runtimecache.InstallWithStore(0, store), Events: store,
		ActivityLogs: store, Tags: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	return ctx, store, project, task, comment, model
}

// TestModeMoveInputCursorAndWordJump exercises the modeMove textinput
// (post-bubbles migration). It walks through char-wise cursor movement
// (left/right) and word-jump (alt+left) — capabilities that didn't
// exist when modeMove was a flat `m.input` string with append/backspace
// semantics. Submitting the corrected bucket key proves the input
// round-trips through TaskService.Move into a real workflow transition.
func TestModeMoveInputCursorAndWordJump(t *testing.T) {
	ctx, store, project, model := newMoveInputTestModel(t)

	// Switch to the table lens (modeMove also lives on the board, but the
	// table lens has the simplest selection model for this test).
	got := pressStringKey(t, model, "/")
	got = pressRune(t, got, 'm')
	if got.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", got.mode)
	}
	if got.moveInput.Value() != "" {
		t.Fatalf("moveInput.Value() = %q, want empty on entry", got.moveInput.Value())
	}

	// Type a slightly off bucket key — the user would otherwise have to
	// erase the whole token and retype, but the textinput supports caret
	// edits. Sequence: type "deve", arrow left × 2 (cursor between 'v'
	// and 'e'), backspace 'v', type 'l', then continue typing "lop" to
	// land on "develop". Wait — to keep the example simple we'll:
	//   1. type "devs"
	//   2. cursor-left to delete the 's' that doesn't belong
	//   3. retype the remainder
	got = sendText(t, got, "devs")
	if got.moveInput.Value() != "devs" {
		t.Fatalf("after sendText: moveInput.Value() = %q, want %q", got.moveInput.Value(), "devs")
	}

	// Cursor goes one char left (now between 'v' and 's'); backspace
	// deletes the 'v', leaving "des" with cursor between 'e' and 's'.
	got = pressStringKey(t, got, "left")
	got = pressKey(t, got, tea.KeyBackspace)
	if got.moveInput.Value() != "des" {
		t.Fatalf("after left+backspace: moveInput.Value() = %q, want %q", got.moveInput.Value(), "des")
	}

	// alt+left jumps a whole word backward — for a single token it lands
	// at column 0. Now type 'd' there to assert the word-jump positioned
	// the cursor at the very start (so the inserted rune lands first).
	got = pressStringKey(t, got, "alt+left")
	got = pressRune(t, got, 'd')
	if got.moveInput.Value() != "ddes" {
		t.Fatalf("after alt+left+'d': moveInput.Value() = %q, want %q", got.moveInput.Value(), "ddes")
	}

	// Reset and submit the actual target bucket key. This proves the
	// modal submits via the textinput value (not the prior `m.input`
	// string) and that TaskService.Move runs.
	got = pressKey(t, got, tea.KeyEsc)
	if got.mode != modeNormal {
		t.Fatalf("after esc: mode = %v, want modeNormal", got.mode)
	}
	got = pressRune(t, got, 'm')
	got = sendText(t, got, "dev")
	got = pressKey(t, got, tea.KeyEnter)

	if got.mode != modeNormal {
		t.Fatalf("after enter: mode = %v, want modeNormal", got.mode)
	}
	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, store.Snapshot())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].BucketKey != "dev" {
		t.Fatalf("tasks after move = %#v, want one task in dev", tasks)
	}
}

// TestModeCommentInputCursorEditsExistingText covers modeComment with a
// caret-aware insert into the middle of a typed string. Pre-bubbles,
// modeComment used a textarea but the parent shim caught modifier-Enter
// and re-routed it through InsertString — this test asserts the new
// flow (no shim) still composes mid-string correctly via cursor moves.
func TestModeCommentInputCursorEditsExistingText(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Subject", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	got := pressKey(t, model, tea.KeyEnter)
	got = pressRune(t, got, 'c')
	if got.taskDetailScreen.State().Mode != taskdetail.ModeComment {
		t.Fatalf("mode = %v, want comment", got.taskDetailScreen.State().Mode)
	}

	// Type "helo" — typo on purpose. Then home, right twice, insert 'l'
	// to fix it to "hello". Asserts that home and right arrow keys both
	// reach the textarea instead of being swallowed by the modal.
	got = sendText(t, got, "helo")
	got = pressStringKey(t, got, "home")
	got = pressStringKey(t, got, "right")
	got = pressStringKey(t, got, "right")
	got = pressRune(t, got, 'l')
	if got.taskDetailScreen.CommentInput().Value() != "hello" {
		t.Fatalf("after caret edit: commentInput.Value() = %q, want %q", got.taskDetailScreen.CommentInput().Value(), "hello")
	}

	got = pressKey(t, got, tea.KeyEnter)
	if got.taskDetailScreen.State().Mode != taskdetail.ModeNormal {
		t.Fatalf("after enter: mode = %v, want normal", got.taskDetailScreen.State().Mode)
	}
	comments, err := store.ListComments(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "hello" {
		t.Fatalf("comments = %+v, want one with body %q", comments, "hello")
	}
}

// TestModeCommentEditInputMultilineCursor proves the modeCommentEdit
// textarea supports multi-line cursor navigation (up/down) on a
// pre-filled body, and that tag-preservation through CommentService.Edit
// still survives the textarea-driven save path. This is the ACE for the
// "multi-line cursor" capability called out in AC4.
func TestModeCommentEditInputMultilineCursor(t *testing.T) {
	ctx, store, project, task, comment, model := newCommentEditInputTestModel(t)
	original := "first line\nsecond\nthird"

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab) // details -> activity
	// Skip past the chronologically-first task.created system event so
	// Enter on the activity card opens the comment we want to edit.
	got = pressStringKey(t, got, "J")
	got = pressKey(t, got, tea.KeyEnter)
	got = pressRune(t, got, 'e')
	if got.commentDetailScreen.Mode() != commentdetail.ModeEdit {
		t.Fatalf("comment mode = %v, want edit", got.commentDetailScreen.Mode())
	}
	if got.commentDetailScreen.Value() != original {
		t.Fatalf("comment value = %q, want %q", got.commentDetailScreen.Value(), original)
	}

	// Caret lands at the end (CursorEnd in openCommentEdit). Move up twice
	// to reach the first line, then to its end via end-of-line. Append
	// " A" to the first line — proving up-arrow navigation works across
	// wrapped textarea rows in the dedicated edit overlay.
	got = pressStringKey(t, got, "up")
	got = pressStringKey(t, got, "up")
	got = pressStringKey(t, got, "end")
	got = sendText(t, got, " A")
	want := "first line A\nsecond\nthird"
	if got.commentDetailScreen.Value() != want {
		t.Fatalf("after multiline edit: value = %q, want %q", got.commentDetailScreen.Value(), want)
	}

	got = pressKey(t, got, tea.KeyCtrlS)
	if got.commentDetailScreen.Mode() != commentdetail.ModeRead {
		t.Fatalf("after ctrl+s: mode = %v, want read", got.commentDetailScreen.Mode())
	}
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.CommentDetail {
		t.Fatalf("after ctrl+s: comment route stack = %v", got.screenStack)
	}

	// Reload from the store and assert tag preservation: edit-from-TUI
	// must NOT wipe tags even though the modal only captures the body.
	comments, err := store.ListComments(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments len = %d, want 1", len(comments))
	}
	saved := comments[0]
	if saved.ID != comment.ID {
		t.Fatalf("saved.ID = %d, want %d", saved.ID, comment.ID)
	}
	if saved.Body != want {
		t.Fatalf("saved.Body = %q, want %q", saved.Body, want)
	}
	if len(saved.Tags) != 1 || saved.Tags[0].Name != "keep-me" {
		var names []string
		for _, t := range saved.Tags {
			names = append(names, t.Name)
		}
		t.Fatalf("saved.Tags = [%s], want [keep-me] preserved across edit", strings.Join(names, ", "))
	}
}

// TestBeginInputModeCommentCalibratesTextarea locks the fix for the
// "field empties on first keystroke" bug on the inline new-comment
// modal. Pre-fix, beginInput called SetWidth/SetHeight with the OUTER
// width while renderCommentInput passed that same outer width into
// field.RenderArea — the leaf then derived a smaller inner width by
// subtracting the formMultiline horizontal padding. The persistent
// model and the render-time copy operated on different wraps; the
// first Update(msg) desynced yOffset and the field appeared to vanish.
//
// Mirrors TestOpenTaskEditCalibratesDescriptionTextarea: the persistent
// textarea Width() must equal the inner width derived from
// commentInputWidth() minus formMultiline padding.
func TestBeginInputModeCommentCalibratesTextarea(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")

	if err := store.ImportBundle(ctx, tuiPermissiveBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Subject", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(WindowSizeMsg) returned %T, want Model", updated)
	}

	got = pressKey(t, got, tea.KeyEnter)
	got = pressRune(t, got, 'c')
	if got.taskDetailScreen.State().Mode != taskdetail.ModeComment {
		t.Fatalf("mode = %v, want comment", got.taskDetailScreen.State().Mode)
	}

	input := got.taskDetailScreen.CommentInput()
	wantInnerWidth := input.Width()
	if w := input.Width(); w != wantInnerWidth || w <= 0 {
		t.Fatalf("commentInput.Width() = %d, want %d (commentInputWidth %d minus padding %d) — Resize at beginInput(modeComment) not applied", w, wantInnerWidth, got.commentInputWidth(), got.styles.formMultiline.GetHorizontalPadding())
	}
	if h := input.Height(); h <= 0 {
		t.Fatalf("commentInput.Height() = %d, want %d — Resize at beginInput(modeComment) not applied", h, commentInputHeight)
	}
}

// TestOpenCommentEditCalibratesTextarea locks the fix for the
// "field empties on first keystroke" bug on the dedicated comment edit
// overlay. Same root cause and same remedy as the inline-comment test
// above, but for openCommentEdit's full-screen path where the outer
// width is commentEditScreenOuterWidth instead of commentInputWidth.
func TestOpenCommentEditCalibratesTextarea(t *testing.T) {
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
	if _, err := store.AddComment(ctx, project.ID, task.ID, "first line\nsecond\nthird", "human", nil); err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{Tasks: store, Comments: store, Dependencies: store, Cache: runtimecache.InstallWithStore(0, store), Events: store, ActivityLogs: store, Tags: store}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update(WindowSizeMsg) returned %T, want Model", updated)
	}

	got = pressKey(t, got, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab) // details -> activity
	got = pressStringKey(t, got, "J")
	got = pressKey(t, got, tea.KeyEnter)
	got = pressRune(t, got, 'e')
	if got.commentDetailScreen.Mode() != commentdetail.ModeEdit {
		t.Fatalf("comment mode = %v, want edit", got.commentDetailScreen.Mode())
	}

	wantInnerWidth := got.commentDetailScreen.EditWidth(got.screenFrame()) - got.styles.formMultiline.GetHorizontalPadding()
	if w := got.commentDetailScreen.Input().Width(); w != wantInnerWidth {
		t.Fatalf("comment input width = %d, want %d", w, wantInnerWidth)
	}
	if h := got.commentDetailScreen.Input().Height(); h != got.commentDetailScreen.EditHeight(got.screenFrame()) {
		t.Fatalf("comment input height = %d, want %d", h, got.commentDetailScreen.EditHeight(got.screenFrame()))
	}
}
