package entitydetail

import (
	"strings"
	"testing"
	"unicode"

	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestEntityMarkdownBoundarySurvivesToggle(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	payload := testPayload(KindLaw)
	payload.Body = "## Safe 漢字 👋\n\n**bold**\x1b[31mred\x1b]0;title\a\x00\u009b31m\n\n- bullet"
	screen := New().Open(payload).Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	for _, rendered := range []bool{true, false, true} {
		if screen.rendered != rendered {
			screen = screen.Update(frame, screentest.Key("M")).Screen.(Screen)
		}
		view := screentest.StripANSI(screen.View(frame))
		if strings.IndexFunc(view, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
			t.Fatalf("entity view retained terminal control: %q", view)
		}
		if !strings.Contains(view, "Safe 漢字 👋") || !strings.Contains(view, "bold") {
			t.Fatalf("entity view lost safe content: %q", view)
		}
	}
}
