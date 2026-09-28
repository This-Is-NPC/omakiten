package taskform

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screens/screentest"
)

const taskFormControlFixture = "漢字 😀 \x1b[31mESC\x1b]0;owned\a C0\x00 C1\u009b31m\u009d end"

func TestTaskFormSanitizesParentPickerAndPersistedFields(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		width int
		tabs  int
	}{
		"compact-parent-selected": {width: 80, tabs: 4},
		"wide-detail":             {width: 200},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			frame := screentest.FrameAt(t, tc.width, 40)
			payload := taskformGoldenEditPayload(frame)
			payload.Values.Title = taskFormControlFixture
			payload.Values.Description = "Safe 日本語\n\n" + taskFormControlFixture
			payload.Values.TagsCSV = taskFormControlFixture
			payload.Kicker = taskFormControlFixture
			screen := New().Bind(taskformGoldenDeps()).Open(payload, frame)
			screen = screen.ApplyLookup(LookupResult{Generation: payload.Generation, Parent: payload.Values.Parent, Label: taskFormControlFixture})
			screen.err = taskFormControlFixture
			screen.form.parentError = ""
			for range tc.tabs {
				screen = screen.Update(frame, screentest.Key("tab")).Screen.(Screen)
			}
			assertTaskFormRenderSafe(t, screen.View(frame))
		})
	}
}

func assertTaskFormRenderSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("task form render retained terminal control U+%04X:\n%s", r, plain)
		}
	}
	if strings.Contains(plain, "owned") {
		t.Fatalf("task form render retained OSC payload:\n%s", plain)
	}
	if !strings.Contains(plain, "漢字") || !strings.Contains(plain, "😀") {
		t.Fatalf("task form render lost harmless Unicode:\n%s", plain)
	}
}
