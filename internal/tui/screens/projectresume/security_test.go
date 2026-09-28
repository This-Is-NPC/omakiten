package projectresume

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screens/screentest"
)

func TestProjectResumeSanitizesRowsAndPromptAtCompactAndWideWidths(t *testing.T) {
	t.Parallel()
	const hostile = "resume\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	payload := Payload{
		TaskBuckets:    []BucketCount{{BucketKey: hostile, Name: hostile, Count: 1}},
		LikelyNextWork: []TaskSummary{{ID: 1, Title: hostile, BucketKey: hostile, Priority: hostile}},
		BlockedWork:    []TaskSummary{{ID: 2, Title: hostile, BucketKey: hostile, Priority: hostile}},
		NextStepPrompt: hostile,
	}
	for _, width := range []int{80, 200} {
		frame := screentest.FrameAt(t, width, 40)
		screen := screenfixture.Enter(New().Apply(payload), frame)
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
