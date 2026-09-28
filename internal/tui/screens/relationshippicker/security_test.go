package relationshippicker

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screens/screentest"
)

func TestRelationshipPickerSanitizesOptionFieldsAndKicker(t *testing.T) {
	t.Parallel()
	const hostile = "agent\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	payload := Payload{Kind: TemplateDefault, EntitySlug: hostile, ProjectSlug: hostile, Options: []Option{{Value: hostile, Label: hostile, Detail: hostile, Selected: true}}}
	for _, width := range []int{80, 200} {
		frame := screentest.FrameAt(t, width, 24)
		screen := screenfixture.Enter(New(TemplateDefault).Open(payload), frame).(Screen)
		if got := screen.Selected().Value; got != hostile {
			t.Fatalf("width %d changed selection identity to %q", width, got)
		}
		plain := ansi.Strip(screen.View(frame))
		for _, r := range plain {
			if r != '\n' && unicode.IsControl(r) {
				t.Fatalf("width %d retained control U+%04X: %q", width, r, screen.View(frame))
			}
		}
		if !strings.Contains(plain, "漢字") || strings.Contains(plain, "owned") {
			t.Fatalf("width %d lost Unicode or retained OSC payload: %q", width, plain)
		}
	}
}
