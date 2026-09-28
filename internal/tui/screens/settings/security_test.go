package settings

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func TestSettingsSanitizesRuntimeValuesAndGuardFields(t *testing.T) {
	t.Parallel()
	const hostile = "setting\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	payload := testPayload()
	payload.Runtime = Runtime{Version: hostile, Scope: hostile, ConfigPath: hostile, DBPath: hostile}
	payload.ThemeKey = hostile
	payload.Languages = config.LanguageSettings{CLI: hostile, TUI: hostile, AgentOutput: hostile}
	payload.Workflow.Buckets[0].Key = hostile
	root := testBundle([]config.TransitionGuard{{Type: hostile}})
	root.Workflows[0].Buckets[0].Key = hostile
	payload.Snapshot = config.BuildSnapshot(root)
	payload.Workflow = payload.Snapshot.Workflow()
	for _, id := range []screenhost.ID{screenhost.SettingsGeneral, screenhost.SettingsGuards} {
		for _, width := range []int{80, 200} {
			assertSettingsScreenSafe(t, payload, id, width)
		}
	}
}

func assertSettingsScreenSafe(t *testing.T, payload Payload, id screenhost.ID, width int) {
	t.Helper()
	frame := screentest.FrameAt(t, width, 40)
	screen := NewGeneral()
	if id == screenhost.SettingsGuards {
		screen = NewGuards()
	}
	screen = screenfixture.Enter(screen.Bind(payload, nil), frame).(Screen)
	view := screen.View(frame)
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("%s width %d retained control U+%04X: %q", id, width, r, view)
		}
	}
	if !strings.Contains(plain, "漢字") || strings.Contains(plain, "owned") {
		t.Fatalf("%s width %d lost Unicode or retained OSC payload: %q", id, width, plain)
	}
}
