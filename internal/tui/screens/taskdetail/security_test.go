package taskdetail

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

const taskDetailControlFixture = "漢字 😀 \x1b[31mESC\x1b]0;owned\a C0\x00 C1\u009b31m\u009d end"

func TestTaskDetailSanitizesTaskFieldsAcrossStates(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		width int
		keys  []string
	}{
		"compact-detail":      {width: 80},
		"wide-selected-child": {width: 200, keys: []string{"s", "l", "j"}},
		"wide-blocker-detail": {width: 120, keys: []string{"b", "down"}},
	} {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			frame := screentest.FrameAt(t, tc.width, 40)
			screen := taskDetailSecurityScreen(frame)
			for _, key := range tc.keys {
				screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
			}
			assertTaskDetailRenderSafe(t, screen.View(frame))
		})
	}
}

func taskDetailSecurityScreen(frame screenhost.Frame) Screen {
	payload := goldenPayload()
	for i := range payload.Tasks {
		payload.Tasks[i].Title = taskDetailControlFixture
		payload.Tasks[i].BucketKey = taskDetailControlFixture
	}
	payload.Task = payload.Tasks[2]
	payload.Task.Description = "Safe 日本語\n\n" + taskDetailControlFixture
	payload.Projection = taskprojection.Build(taskprojection.Input{
		Tasks: payload.Tasks, Workflow: payload.Workflow, Dependencies: payload.Dependencies,
		Comments: goldenComments(), Priorities: goldenPriorities(),
	})
	payload.Activity[1].AuthorType = taskDetailControlFixture
	payload.Activity[1].Body = taskDetailControlFixture
	payload.Activity[1].Tags = []domain.Tag{{Label: taskDetailControlFixture}}
	deps := Deps{
		Priorities: goldenPriorities(),
		TaskTags: func(int64) []domain.Tag {
			return []domain.Tag{{Label: taskDetailControlFixture}}
		},
		MovePrompt: func(domain.Task) string { return taskDetailControlFixture },
	}
	return screenfixture.Enter(New().Bind(deps).Open(payload, frame), frame).(Screen)
}

func assertTaskDetailRenderSafe(t *testing.T, view string) {
	t.Helper()
	plain := ansi.Strip(view)
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("task detail render retained terminal control U+%04X:\n%s", r, plain)
		}
	}
	if strings.Contains(plain, "owned") {
		t.Fatalf("task detail render retained OSC payload:\n%s", plain)
	}
	if !strings.Contains(plain, "漢字") || !strings.Contains(plain, "😀") {
		t.Fatalf("task detail render lost harmless Unicode:\n%s", plain)
	}
	if !strings.Contains(plain, screenkit.Sanitize(taskDetailControlFixture)) {
		t.Fatalf("task detail render lost sanitized task text:\n%s", plain)
	}
}
