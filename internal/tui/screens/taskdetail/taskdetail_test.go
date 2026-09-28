package taskdetail

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
)

func testFrame(width, height int) screenhost.Frame {
	return screenhost.NewFrame(screenhost.FrameOptions{Width: width, Height: height, Text: func(key string) string { return key }})
}

func testPayload() Payload {
	tasks := []domain.Task{
		{ID: 10, Title: "parent", BucketKey: "dev"},
		{ID: 11, Title: "child one", BucketKey: "dev", ParentID: ptr(int64(10))},
		{ID: 12, Title: "child two", BucketKey: "done", ParentID: ptr(int64(10))},
	}
	workflow := domain.Workflow{Buckets: []domain.Bucket{{Key: "dev", Name: "Development", Position: 1}, {Key: "done", Name: "Done", Position: 2}}}
	return Payload{
		Generation: 7,
		Task:       domain.Task{ID: 10, Title: "parent", BucketKey: "dev"},
		Tasks:      tasks,
		Workflow:   workflow,
		Activity:   []domain.Event{{ID: 101, EventType: domain.EventTypeComment}, {ID: 102, EventType: domain.EventTypeTaskMoved}},
	}
}

func ptr[T any](value T) *T { return &value }

func TestRandomCursorInvariants(t *testing.T) {
	seed := int64(1992)
	t.Logf("seed=%d", seed)
	rng := rand.New(rand.NewSource(seed))
	s := New().Open(testPayload(), testFrame(120, 40))
	keys := []string{"tab", "j", "k", "h", "l", "g", "G", "pgup", "pgdown"}
	for step := 0; step < 2_000; step++ {
		out := s.Update(testFrame(30+rng.Intn(130), 12+rng.Intn(60)), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(keys[rng.Intn(len(keys))])})
		s = out.Screen.(Screen)
		state := s.State()
		if state.ActivityCursor < -1 || state.ActivityCursor >= len(s.Payload().Activity) {
			t.Fatalf("step %d activity cursor=%d", step, state.ActivityCursor)
		}
		if state.SubtaskCursor < -1 || state.SubtaskCursor >= 2 {
			t.Fatalf("step %d subtask cursor=%d", step, state.SubtaskCursor)
		}
		if state.SubtaskColumn < 0 || state.SubtaskColumn >= 2 {
			t.Fatalf("step %d subtask column=%d", step, state.SubtaskColumn)
		}
	}
}

func TestNestedModesEmitTypedOutcomesAndCancelLocally(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30))
	cases := map[string]struct {
		open string
		want Mode
	}{
		"blockers": {"b", ModeBlockers},
		"comment":  {"c", ModeComment},
		"move":     {"m", ModeMove},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out := s.Update(testFrame(100, 30), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.open)})
			opened := out.Screen.(Screen)
			if opened.State().Mode != tc.want || !opened.BlocksHostInput() {
				t.Fatalf("mode=%v blocks=%v", opened.State().Mode, opened.BlocksHostInput())
			}
			out = opened.Update(testFrame(100, 30), tea.KeyMsg{Type: tea.KeyEsc})
			cancelled := out.Screen.(Screen)
			if cancelled.State().Mode != ModeNormal || out.Action.Kind != screenhost.ActionNone {
				t.Fatalf("cancel mode=%v action=%v", cancelled.State().Mode, out.Action.Kind)
			}
		})
	}
}

func TestApplyRejectsStaleWrongTaskAndCancelledResults(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30))
	fresh := Result{Generation: 7, TaskID: 10, Activity: []domain.Event{{ID: 200}}, ActivityValid: true}
	if got := s.Apply(Result{Generation: 6, TaskID: 10, ActivityValid: true}); len(got.Payload().Activity) != 2 {
		t.Fatal("stale generation applied")
	}
	if got := s.Apply(Result{Generation: 7, TaskID: 99, ActivityValid: true}); len(got.Payload().Activity) != 2 {
		t.Fatal("wrong task result applied")
	}
	s = s.CancelPending()
	if got := s.Apply(fresh); len(got.Payload().Activity) != 2 {
		t.Fatal("cancelled result applied")
	}
}

func TestResizeRefreshAndActivityAnchor(t *testing.T) {
	s := New().Open(testPayload(), testFrame(80, 20)).WithActivityCursor(1)
	resized := s.Lifecycle(testFrame(150, 55), screenhost.LifecycleResize).Screen.(Screen)
	if resized.State().Width != 150 || resized.State().Height != 55 {
		t.Fatalf("size=%dx%d", resized.State().Width, resized.State().Height)
	}
	updated := resized.Apply(Result{Generation: 7, TaskID: 10, ActivityValid: true, Activity: []domain.Event{{ID: 99}, {ID: 101}, {ID: 102}}})
	if updated.FocusedActivityID() != 102 {
		t.Fatalf("focused activity=%d, want 102", updated.FocusedActivityID())
	}
}

func key(value string) tea.KeyMsg {
	switch value {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}
}

func update(t *testing.T, s Screen, value string) screenhost.Outcome {
	t.Helper()
	return s.Update(testFrame(100, 30), key(value))
}

func TestNormalModeTypedOperations(t *testing.T) {
	if !New().Open(testPayload(), testFrame(100, 30)).OwnsKey(key("f")) {
		t.Fatal("task detail did not claim its section-focus key")
	}
	cases := []struct {
		key  string
		want screenhost.ActionKind
	}{
		{"esc", screenhost.ActionCloseTaskDetail},
		{"r", screenhost.ActionRefreshTaskDetail},
		{"e", screenhost.ActionEditTask},
		{"d", screenhost.ActionDeleteTaskFromDetail},
		{"x", screenhost.ActionArchiveTaskFromDetail},
		{"enter", screenhost.ActionOpenTaskDescription},
		{"M", screenhost.ActionToggleTaskMarkdown},
		{"a", screenhost.ActionCreateSubtask},
		{"n", screenhost.ActionCreateSubtask},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			out := update(t, New().Open(testPayload(), testFrame(100, 30)), tc.key)
			if out.Action.Kind != tc.want || out.Action.TaskID != 10 {
				t.Fatalf("action = %+v, want kind %v task 10", out.Action, tc.want)
			}
		})
	}
	if got := update(t, New().Open(testPayload(), testFrame(100, 30)), "ctrl+c").Action.Kind; got != screenhost.ActionQuit {
		t.Fatalf("ctrl+c action = %v", got)
	}
	archived := testPayload()
	archived.Task.State = domain.TaskStateArchived
	if got := update(t, New().Open(archived, testFrame(100, 30)), "x").Action.Kind; got != screenhost.ActionUnarchiveTaskFromDetail {
		t.Fatalf("archived x action = %v, want unarchive", got)
	}
}

func TestChildOperationsUseFocusedSubtask(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30))
	s = update(t, s, "s").Screen.(Screen)
	if out := update(t, s, "enter"); out.Action.Kind != screenhost.ActionOpenNestedTask || out.Action.TaskID != 11 {
		t.Fatalf("open nested = %+v", out.Action)
	}
	if out := update(t, s, " "); out.Action.Kind != screenhost.ActionCompleteSubtask || out.Action.TaskID != 11 {
		t.Fatalf("complete = %+v", out.Action)
	}
	s = update(t, s, "m").Screen.(Screen)
	if s.MoveTaskID() != 11 {
		t.Fatalf("move task = %d", s.MoveTaskID())
	}
	for _, r := range "done" {
		s = update(t, s, string(r)).Screen.(Screen)
	}
	out := update(t, s, "enter")
	if out.Action.Kind != screenhost.ActionMoveTaskFromDetail || out.Action.TaskID != 11 || out.Action.BucketKey != "done" {
		t.Fatalf("move action = %+v", out.Action)
	}
}

func TestCommentAndBlockerOperations(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30))
	s = update(t, s, "c").Screen.(Screen)
	for _, r := range "hello" {
		s = update(t, s, string(r)).Screen.(Screen)
	}
	out := update(t, s, "enter")
	if out.Action.Kind != screenhost.ActionAddTaskComment || out.Action.Value != "hello" {
		t.Fatalf("comment action = %+v", out.Action)
	}

	s = New().Open(testPayload(), testFrame(100, 30))
	s = update(t, s, "b").Screen.(Screen)
	s = update(t, s, " ").Screen.(Screen)
	out = update(t, s, "ctrl+s")
	if out.Action.Kind != screenhost.ActionSaveTaskBlockers || !reflect.DeepEqual(out.Action.TaskIDs, []int64{11}) {
		t.Fatalf("blocker action = %+v", out.Action)
	}
}

func TestNestedModesRenderInsideTaskDetail(t *testing.T) {
	frame := testFrame(160, 50)
	s := New().Bind(Deps{MovePrompt: func(domain.Task) string { return "move target" }}).Open(testPayload(), frame)

	blockers := update(t, s, "b").Screen.(Screen)
	if got := blockers.View(frame); !strings.Contains(got, "child one") || !strings.Contains(got, "tui.picker.hint.blockers") {
		t.Fatalf("blocker view missing screen-owned presentation:\n%s", got)
	}

	comment := update(t, s, "c").Screen.(Screen)
	if got := comment.View(frame); !strings.Contains(got, "tui.form.hint.enter_saves") || !strings.Contains(got, "parent") {
		t.Fatalf("comment view missing input or task context:\n%s", got)
	}

	move := update(t, s, "m").Screen.(Screen)
	if got := move.View(frame); !strings.Contains(got, "move target") || !strings.Contains(got, "parent") {
		t.Fatalf("move view missing input or task context:\n%s", got)
	}
}

func TestActivityOpenOnlyEmitsForFocusedRow(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30)).WithFocus(FocusActivity).WithActivityCursor(0)
	if out := update(t, s, "enter"); out.Action.Kind != screenhost.ActionOpenTaskComment || out.Action.CommentID != 101 {
		t.Fatalf("activity open = %+v", out.Action)
	}
	s = s.WithActivityCursor(-1)
	if out := update(t, s, "enter"); out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("empty activity action = %+v", out.Action)
	}
}

func TestProjectionAccessorsLifecycleAndPresentation(t *testing.T) {
	s := New().Open(testPayload(), testFrame(80, 20))
	if s.ID() != screenhost.TaskDetail || len(s.Activity()) != 2 || s.State().ActivityScroll < 0 || s.Subtasks().Cursor() < -1 {
		t.Fatalf("invalid accessors: id=%s state=%+v", s.ID(), s.State())
	}
	_ = s.BlockerPicker()
	_ = s.BlockerChecks()
	_ = s.CommentInput()
	_ = s.MoveInput()
	_ = s.Details()
	if got := s.View(testFrame(80, 20)); !strings.Contains(got, "parent") {
		t.Fatalf("view = %q", got)
	}
	if len(s.Footer(testFrame(80, 20))) == 0 || len(s.Help(testFrame(80, 20))) == 0 || !s.OwnsFooter() || !s.OwnsKey(key("x")) {
		t.Fatal("presentation contract incomplete")
	}
	if got := New().Open(testPayload(), testFrame(80, 20)).View(testFrame(80, 20)); !strings.Contains(got, "parent") {
		t.Fatalf("fallback view = %q", got)
	}
	if out := s.Update(testFrame(80, 20), struct{}{}); out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("non-key action = %v", out.Action.Kind)
	}
	left := s.Lifecycle(testFrame(80, 20), screenhost.LifecycleLeave).Screen.(Screen)
	if left.State().Pending {
		t.Fatal("leave did not cancel pending result")
	}
	entered := left.Lifecycle(testFrame(90, 25), screenhost.LifecycleEnter).Screen.(Screen)
	if entered.State().Width != 90 || entered.State().Height != 25 {
		t.Fatalf("enter size = %+v", entered.State())
	}
}

func TestFinishReplaceAndCompleteResult(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30)).WithFocus(FocusSubtasks)
	s = update(t, s, "c").Screen.(Screen).FinishOperation()
	if s.State().Mode != ModeNormal || !s.State().Pending || s.CommentInput().Value() != "" {
		t.Fatalf("finished state = %+v", s.State())
	}
	payload := testPayload()
	payload.Task.Title = "replacement"
	s = s.Replace(payload)
	if s.Payload().Task.Title != "replacement" || s.State().Focus != FocusSubtasks {
		t.Fatalf("replace lost state: payload=%+v state=%+v", s.Payload(), s.State())
	}
	result := Result{
		Generation: 7, TaskID: 10,
		Task: domain.Task{ID: 10, Title: "applied"}, TaskValid: true,
		Tasks: []domain.Task{{ID: 10}}, TasksValid: true,
		Dependencies: []domain.TaskDependency{{TaskID: 10, DependsOnTaskID: 12}}, BlockersValid: true,
		Activity: []domain.Event{{ID: 300}}, ActivityValid: true,
	}
	s = s.Apply(result)
	got := s.Payload()
	if got.Task.Title != "applied" || len(got.Tasks) != 1 || len(got.Dependencies) != 1 || len(got.Activity) != 1 {
		t.Fatalf("applied payload = %+v", got)
	}
}

func TestFooterTracksModesFocusAndDeleteConfirmation(t *testing.T) {
	s := New().Open(testPayload(), testFrame(100, 30))
	assertKey := func(bindings []screenhost.FooterBinding, want string) {
		t.Helper()
		for _, binding := range bindings {
			if binding.Key == want {
				return
			}
		}
		t.Fatalf("footer missing %q: %+v", want, bindings)
	}
	assertKey(s.Footer(testFrame(100, 30)), "d")
	armed := update(t, s, "d").Screen.(Screen)
	if !armed.State().DeleteArmed {
		t.Fatal("delete confirmation was not screen-owned")
	}
	for _, binding := range armed.Footer(testFrame(100, 30)) {
		if binding.Key == "d" && !binding.Primary {
			t.Fatal("armed delete footer is not primary")
		}
	}
	comment := update(t, s, "c").Screen.(Screen)
	assertKey(comment.Footer(testFrame(100, 30)), "alt+enter/shift+enter")
	blockers := update(t, s, "b").Screen.(Screen)
	assertKey(blockers.Footer(testFrame(100, 30)), "ctrl+s")
	activity := s.WithFocus(FocusActivity)
	assertKey(activity.Footer(testFrame(100, 30)), "enter")
}

func TestFocusSubtasksWithoutChildrenEmitsStatus(t *testing.T) {
	payload := testPayload()
	payload.Tasks = payload.Tasks[:1]
	out := update(t, New().Open(payload, testFrame(100, 30)), "s")
	if out.Action.Kind != screenhost.ActionSetStatus || out.Action.Status != "tui.status.no_subtasks_to_focus" {
		t.Fatalf("status action = %+v", out.Action)
	}
}
