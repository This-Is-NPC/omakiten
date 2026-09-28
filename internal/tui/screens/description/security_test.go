package description

import (
	"strings"
	"testing"
	"unicode"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestDescriptionMarkdownBoundarySurvivesToggle(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	screen := New().Open(domain.Task{ID: 42, Title: "Description", BucketKey: "dev", Description: hostileMarkdown})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for _, rendered := range []bool{true, false, true} {
		if screen.rendered != rendered {
			screen = screen.Update(frame, screentest.Key("M")).Screen.(Screen)
		}
		assertDescriptionMarkdownSafe(t, screentest.StripANSI(screen.View(frame)))
	}
}

const hostileMarkdown = "## Safe 漢字 👋\n\n**bold**\x1b[31mred\x1b]0;title\a\x00\u009b31m\n\n- bullet"

func assertDescriptionMarkdownSafe(t *testing.T, view string) {
	t.Helper()
	if strings.IndexFunc(view, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("description view retained terminal control: %q", view)
	}
	if !strings.Contains(view, "Safe 漢字 👋") || !strings.Contains(view, "bold") {
		t.Fatalf("description view lost safe content: %q", view)
	}
}
