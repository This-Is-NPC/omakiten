package commentdetail

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestCommentReadEditDirtyCancelAndSaveFailureLifecycle(t *testing.T) {
	frame := screentest.FrameAt(t, 90, 24)
	comment := domain.Comment{ID: 7, TaskID: 42, Scope: domain.CommentScopeTask, AuthorType: "human", Body: "original"}
	screen := New().Open(Payload{Comment: comment, Editable: true, Deletable: true})
	if screen.ID() != screenhost.CommentDetail || screen.Mode() != ModeRead {
		t.Fatalf("initial identity/mode = %q/%v", screen.ID(), screen.Mode())
	}
	screen = screen.Update(frame, screentest.Key("e")).Screen.(Screen)
	if screen.Mode() != ModeEdit || screen.Value() != "original" || screen.Dirty() {
		t.Fatalf("edit state = mode %v value %q dirty %v", screen.Mode(), screen.Value(), screen.Dirty())
	}
	screen = screen.Update(frame, screentest.Key("x")).Screen.(Screen)
	if !screen.Dirty() || !strings.HasSuffix(screen.Value(), "x") {
		t.Fatalf("typed state = value %q dirty %v", screen.Value(), screen.Dirty())
	}
	screen = screen.Update(frame, screentest.Key("esc")).Screen.(Screen)
	if screen.Mode() != ModeRead || screen.Dirty() || screen.Value() != "original" {
		t.Fatalf("cancel state = mode %v value %q dirty %v", screen.Mode(), screen.Value(), screen.Dirty())
	}

	screen = screen.Update(frame, screentest.Key("e")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("x")).Screen.(Screen)
	outcome := screen.Update(frame, tea.KeyMsg{Type: tea.KeyCtrlS})
	if outcome.Action.Kind != screenhost.ActionSaveComment || outcome.Action.CommentID != 7 || !strings.HasSuffix(outcome.Action.Value, "x") {
		t.Fatalf("save action = %+v", outcome.Action)
	}
	screen = outcome.Screen.(Screen).Apply(Result{CommentID: 7, Err: errors.New("save failed")})
	if screen.Mode() != ModeEdit || !screen.Dirty() || !strings.Contains(screentest.StripANSI(screen.View(frame)), "save failed") {
		t.Fatalf("failed save lost edit state: mode=%v dirty=%v view=%q", screen.Mode(), screen.Dirty(), screentest.StripANSI(screen.View(frame)))
	}
	saved := comment
	saved.Body = screen.Value()
	screen = screen.Apply(Result{CommentID: 7, Comment: saved, Saved: true})
	if screen.Mode() != ModeRead || screen.Dirty() || screen.Payload().Comment.Body != saved.Body {
		t.Fatalf("saved state = mode %v dirty %v body %q", screen.Mode(), screen.Dirty(), screen.Payload().Comment.Body)
	}
}

func TestCommentPermissionsMarkdownDeleteResizeAndLoading(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 18)
	comment := domain.Comment{ID: 8, Scope: domain.CommentScopeProject, Body: "**body**"}
	screen := New().Open(Payload{Comment: comment, EditDenied: "read only", DeleteDenied: "read only"})
	if got := screen.Update(frame, screentest.Key("e")).Action; got.Kind != screenhost.ActionSetStatus || got.Status != "read only" {
		t.Fatalf("denied edit action = %+v", got)
	}
	if got := screen.Update(frame, screentest.Key("d")).Action; got.Kind != screenhost.ActionSetStatus || got.Status != "read only" {
		t.Fatalf("denied delete action = %+v", got)
	}
	markdownOutcome := screen.Update(frame, screentest.Key("M"))
	if markdownOutcome.Action.Kind != screenhost.ActionSetStatus {
		t.Fatalf("markdown action = %+v, want status", markdownOutcome.Action)
	}
	screen = markdownOutcome.Screen.(Screen)
	_ = screen.View(frame)
	if screen.rendered {
		t.Fatal("M did not switch comment markdown mode")
	}
	screen = screen.Lifecycle(screentest.FrameAt(t, 52, 13), screenhost.LifecycleResize).Screen.(Screen)
	if screen.Width() != 52 || screen.Height() != 13 {
		t.Fatalf("resize = %dx%d", screen.Width(), screen.Height())
	}
	if got := screentest.StripANSI(New().Loading().View(frame)); !strings.Contains(got, "Loading") {
		t.Fatalf("loading view = %q", got)
	}
}

func TestCommentDeclaresChromeScrollDeleteAndDefensiveResults(t *testing.T) {
	frame := screentest.FrameAt(t, 60, 13)
	comment := domain.Comment{ID: 11, TaskID: 4, Scope: domain.CommentScopeTask, AuthorType: "agent", Body: strings.Repeat("line\n", 60), Tags: []domain.Tag{{Label: "review"}}}
	screen := New().Open(Payload{Comment: comment, Editable: true, Deletable: true})
	if !screen.OwnsKey(screentest.Key("x")) || !screen.OwnsFooter() || !screen.BlocksHostInput() || screen.BlocksHelp() || screen.Input().Value() != comment.Body {
		t.Fatal("comment did not expose its owned route capabilities")
	}
	if len(screen.Footer(frame)) == 0 || screen.Help(frame)[0].ID != "comment_view" {
		t.Fatal("read footer/help declarations are missing")
	}
	if got := screen.Update(frame, screentest.Key("q")).Action.Kind; got != screenhost.ActionQuit {
		t.Fatalf("read q action = %v", got)
	}
	_ = screen.View(frame)
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if screen.Scroll() == 0 {
		t.Fatal("comment reader did not scroll")
	}
	screen = screen.Update(frame, screentest.Key("d")).Screen.(Screen)
	if !screen.DeleteArmed() || len(screen.Footer(frame)) == 0 {
		t.Fatal("comment delete did not arm")
	}
	outcome := screen.Update(frame, screentest.Key("d"))
	if outcome.Action.Kind != screenhost.ActionDeleteComment || outcome.Action.CommentID != comment.ID {
		t.Fatalf("delete action = %+v", outcome.Action)
	}
	screen = outcome.Screen.(Screen).Apply(Result{CommentID: 999, Saved: true})
	if screen.Payload().Comment.ID != comment.ID {
		t.Fatal("mismatched result replaced the active comment")
	}
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen = screen.Update(frame, struct{}{}).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("e")).Screen.(Screen)
	if screen.Help(frame)[0].ID != "comment_edit" || len(screen.Footer(frame)) == 0 || !screen.BlocksHelp() {
		t.Fatal("edit footer/help declarations are missing")
	}
	if got := screen.Update(frame, tea.KeyMsg{Type: tea.KeyCtrlC}).Action.Kind; got != screenhost.ActionQuit {
		t.Fatalf("edit ctrl+c action = %v", got)
	}
	if got := screentest.StripANSI(New().View(frame)); !strings.Contains(got, "not found") {
		t.Fatalf("not-found view = %q", got)
	}
}

func TestCommentCellMountKeepsTypingAndChromeWidthOwnership(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Bind(commentGoldenDeps()).Open(Payload{
		Comment:  domain.Comment{ID: 12, Body: "body"},
		Editable: true,
	})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	root := screen.root(frame)
	if !root.IsLeaf() || root.Spec.ID != sectionBody || screen.grid.Focus() != sectionBody {
		t.Fatalf("Cell mount = leaf:%v id:%q focus:%q", root.IsLeaf(), root.Spec.ID, screen.grid.Focus())
	}
	wantWidth := field.WidthFor(frame.Kit().PanelContentWidth(), screen.deps.EditTheme)
	if got := screen.EditWidth(frame); got != wantWidth {
		t.Fatalf("EditWidth() = %d, want chrome-owned panel width %d", got, wantWidth)
	}

	screen = screen.Update(frame, screentest.Key("e")).Screen.(Screen)
	if root := screen.root(frame); !root.IsLeaf() || root.Spec.ID != sectionBody {
		t.Fatalf("edit mode root = leaf:%v id:%q, want the comment body Cell", root.IsLeaf(), root.Spec.ID)
	}
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("k")).Screen.(Screen)
	if got := screen.Value(); got != "bodyjk" {
		t.Fatalf("focused textarea value = %q, want j/k typed as letters", got)
	}
	if got := screen.grid.Layout().Offset(screenlayout.ID(sectionBody)); got != 0 {
		t.Fatalf("textarea j/k moved grid offset to %d", got)
	}
}
