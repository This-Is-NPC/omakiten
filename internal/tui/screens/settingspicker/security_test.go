package settingspicker

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screens/screentest"
)

func TestSettingsPickerSanitizesSelectedOptionAndKeepsValueIdentity(t *testing.T) {
	t.Parallel()
	const hostile = "profile\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	payload := Payload{Kind: Config, Current: hostile, Options: []Option{{Value: hostile, Label: hostile, Detail: hostile, Active: true}}}
	for _, width := range []int{80, 200} {
		frame := screentest.FrameAt(t, width, 24)
		screen := screenfixture.Enter(New(Config).Open(payload), frame).(Screen)
		if got := screen.Selected().Value; got != hostile {
			t.Fatalf("width %d changed selection identity to %q", width, got)
		}
		assertSettingsPickerSafe(t, screen.View(frame))
	}
	empty := screenfixture.Enter(New(Config).Open(Payload{Kind: Config}), screentest.FrameAt(t, 80, 24)).(Screen)
	if empty.View(screentest.FrameAt(t, 80, 24)) == "" {
		t.Fatal("empty picker lost its deterministic empty state")
	}
}

func assertSettingsPickerSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("settings picker retained control U+%04X: %q", r, view)
		}
	}
	if !strings.Contains(plain, "漢字") || strings.Contains(plain, "owned") {
		t.Fatalf("settings picker lost Unicode or retained OSC payload: %q", plain)
	}
}
