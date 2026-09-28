package board

import (
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screens/screentest"
)

func TestBoardLaneHeaderSanitizesBucketNameAndKeepsUnicode(t *testing.T) {
	t.Parallel()
	const hostile = "review\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	deps := boardGoldenDeps()
	deps.Workflow.Buckets[0].Name = hostile
	for _, width := range []int{80, 200} {
		frame := screentest.FrameAt(t, width, 40)
		screen := screenfixture.Enter(New().Bind(deps, frame), frame)
		assertBoardTerminalSafe(t, screen.View(frame))
	}
}

func assertBoardTerminalSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("board retained terminal control U+%04X: %q", r, view)
		}
	}
	if !containsText(plain, "漢字") || containsText(plain, "owned") {
		t.Fatalf("board lost Unicode or retained OSC payload: %q", plain)
	}
}

func containsText(value, want string) bool {
	for len(value) >= len(want) {
		if value[:len(want)] == want {
			return true
		}
		value = value[1:]
	}
	return false
}
