package table

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screens/screentest"
)

const tableControlFixture = "漢字 😀 \x1b[31mESC\x1b]0;owned\a C0\x00 C1\u009b31m\u009d end"

func TestTableSanitizesTaskRowsAcrossShapes(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		width    int
		selected int64
	}{
		"compact":       {width: 60, selected: 1},
		"wide-selected": {width: 120, selected: 2},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			deps := tableDeps()
			deps.Tasks = []domain.Task{
				{ID: 1, Title: tableControlFixture, BucketKey: tableControlFixture, Priority: 1},
				{ID: 2, Title: "safe 日本語", BucketKey: "dev", Priority: 1},
			}
			deps.Priorities = []config.PriorityDefinition{{ID: 1, Value: tableControlFixture}}
			frame := screentest.FrameAt(t, tc.width, 24)
			screen := New().Bind(deps, frame).SelectTaskID(tc.selected, frame)
			assertTableRenderSafe(t, screen.View(frame))
		})
	}
}

func assertTableRenderSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("table render retained terminal control U+%04X:\n%s", r, plain)
		}
	}
	if strings.Contains(plain, "owned") {
		t.Fatalf("table render retained OSC payload:\n%s", plain)
	}
	if !strings.Contains(plain, "漢字") || !strings.Contains(plain, "😀") {
		t.Fatalf("table render lost harmless Unicode:\n%s", plain)
	}
}
