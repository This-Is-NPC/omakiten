package commentdetail

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestCommentMarkdownBoundarySurvivesToggle(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	screen := New().Open(Payload{Comment: domain.Comment{ID: 7, TaskID: 42, Scope: domain.CommentScopeTask, Body: hostileMarkdown}})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for _, rendered := range []bool{true, false, true} {
		if screen.rendered != rendered {
			screen = screen.Update(frame, screentest.Key("M")).Screen.(Screen)
		}
		assertCommentMarkdownSafe(t, screentest.StripANSI(screen.View(frame)))
	}
}

func TestCommentEditErrorHeaderSanitizesTerminalControls(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	screen := New().Open(Payload{Comment: domain.Comment{ID: 7, TaskID: 42, Scope: domain.CommentScopeTask, Body: "editable"}})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	screen.mode = ModeEdit
	screen.err = errors.New(hostileMarkdown)
	assertCommentMarkdownSafe(t, screentest.StripANSI(screen.View(frame)))
}

const hostileMarkdown = "## Safe 漢字 👋\n\n**bold**\x1b[31mred\x1b]0;title\a\x00\u009b31m\n\n- bullet"

func assertCommentMarkdownSafe(t *testing.T, view string) {
	t.Helper()
	if strings.IndexFunc(view, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("comment view retained terminal control: %q", view)
	}
	if !strings.Contains(view, "Safe 漢字 👋") {
		t.Fatalf("comment view lost safe content: %q", view)
	}
}
