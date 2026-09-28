package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/selectlist"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

type securityHookEvents struct {
	rows   []domain.EventRow
	filter domain.EventFilter
}

func (s *securityHookEvents) ListEvents(_ context.Context, filter domain.EventFilter) ([]domain.EventRow, error) {
	s.filter = filter
	rows := make([]domain.EventRow, 0, len(s.rows))
	for _, row := range s.rows {
		if row.ProjectID == filter.ProjectID {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func TestPersistedHookErrorIsSanitizedAtEveryHistorySink(t *testing.T) {
	t.Parallel()

	payload := "safe\x1b[31mred\x1b]0;owned\a timeout"
	bundle := studioHooksGoldenBundle()
	execIndex := HookIndexFor(bundle.Config.Hooks, func(spec config.HookSpec) bool { return spec.Do == "exec" })
	raw, err := json.Marshal(map[string]any{
		"hook_index":  execIndex,
		"success":     false,
		"duration_ms": 3001,
		"event_type":  "task.created",
		"error":       payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := &securityHookEvents{rows: []domain.EventRow{
		{ProjectID: 42, EventType: domain.EventTypeHookExecuted, Payload: string(raw)},
		{ProjectID: 7, EventType: domain.EventTypeHookExecuted, Payload: string(raw)},
	}}
	history, err := studioprojection.LoadHookHistory(context.Background(), events, 42, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if events.filter.ProjectID != 42 || len(history[execIndex]) != 1 {
		t.Fatalf("history filter = %d, rows = %d; want project 42 and one row", events.filter.ProjectID, len(history[execIndex]))
	}

	deps, err := StudioHooksDeps(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deps.HookHistory = history
	for _, geo := range []struct{ width, height int }{{80, 24}, {200, 50}} {
		geo := geo
		t.Run(testName(geo.width, geo.height), func(t *testing.T) {
			t.Parallel()
			assertPersistedHookHistorySafe(t, deps, history, execIndex, geo.width, geo.height)
		})
	}
}

func assertPersistedHookHistorySafe(t *testing.T, deps Deps, history map[int][]studioprojection.HookExecuted, index, width, height int) {
	t.Helper()
	frame := screentest.FrameAt(t, width, height)
	screen := New().Bind(screenhost.StudioHooks, deps).WithState(State{HookIndex: index})
	screen = screen.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	rendered := screen.View(frame)
	if width < 120 {
		_, _, body := screen.hooksHistoryContentFor(index, 50, history[index])
		rendered += strings.Join(body, "\n")
	}
	for _, forbidden := range []string{"\x1b[31m", "owned", "\x1b]0;owned\a"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("history view retained %q\n%s", forbidden, rendered)
		}
	}
	if !strings.Contains(screenkit.Sanitize(rendered), "safe") {
		t.Fatalf("history view lost harmless text\n%s", screenkit.Sanitize(rendered))
	}
}

func testName(width, height int) string {
	return fmt.Sprintf("%dx%d", width, height)
}

func TestCandidateRenderingSanitizesTerminalControlsAtLeafBoundaries(t *testing.T) {
	t.Parallel()
	t.Run("preview warnings impact and diff", testCandidatePreviewSafety)
	t.Run("commands", testCandidateCommandsSafety)
	t.Run("workflow", testCandidateWorkflowSafety)
	t.Run("hooks", testCandidateHooksSafety)
}

func assertCandidateOutputSafe(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{"\x1b[31m", "\x1b]", "owned", "\x00", "\u009b", "\u009d"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("candidate output retained %q in %q", forbidden, output)
		}
	}
}

func testCandidatePreviewSafety(t *testing.T) {
	t.Parallel()
	payload := "safe\x1b[31mred\x1b]0;owned\a\x00\u009b\u009dend"
	m := Screen{}
	assertCandidateOutputSafe(t, strings.Join(prefixLines([]string{payload}, "- "), "\n"))
	rows := m.summaryRows("CANDIDATE", [2]string{"validation", payload}, [2]string{"blocked", payload})
	for _, row := range rows {
		assertCandidateOutputSafe(t, gridtable.RenderCells([][]gridtable.Cell{row}, []int{80, 80}, lipgloss.NewStyle()))
	}
	bundle := config.Bundle{
		MCPCommands: map[string]config.MCPCommandSpec{"okt-task-continue": {Persona: "builder"}},
		Personas:    []config.Persona{{Slug: "builder", Name: "Builder", Body: "first\n" + payload}}}
	for i, row := range studioCommandRows(bundle.MCPCommands, nil) {
		if row.Key == "okt-task-continue" {
			m.studioCommandIndex = i
			break
		}
	}
	assertCandidateOutputSafe(t, m.renderStudioPromptPreview(bundle))
}

func testCandidateCommandsSafety(t *testing.T) {
	t.Parallel()
	payload := "safe\x1b[31mred\x1b]0;owned\a\x00\u009b\u009dend"
	m := Screen{}
	assertCandidateOutputSafe(t, strings.Join(m.commandsInspectorTable(studioprojection.CommandRow{
		Key:  payload,
		Spec: config.MCPCommandSpec{Persona: payload, Skills: []string{payload}}}, config.Bundle{}, 80, 0), "\n"))
	assertCandidateOutputSafe(t, "Candidate diff:\n"+strings.Join(prefixLines([]string{payload}, "- "), "\n"))
	assertCandidateOutputSafe(t, "Command warnings:\n"+strings.Join(prefixLines([]string{payload}, "- "), "\n"))
}

func testCandidateWorkflowSafety(t *testing.T) {
	t.Parallel()
	payload := "safe\x1b[31mred\x1b]0;owned\a\x00\u009b\u009dend"
	workflow := config.Workflow{
		Key:         payload,
		Buckets:     []config.Bucket{{ID: 1, Key: payload, Name: payload, Position: 1}, {ID: 2, Key: "done", Name: "done", Position: 2}},
		Transitions: []config.Transition{{From: 1, To: 2, Guards: []config.TransitionGuard{{Type: "comments_tagged", Tag: payload, Hint: payload}}}},
	}
	m := Screen{tasks: nil}
	rows := studioWorkflowRows(workflow, nil, nil)
	for _, row := range rows {
		assertCandidateOutputSafe(t, row.Left)
		assertCandidateOutputSafe(t, row.Right)
		assertCandidateOutputSafe(t, selectlist.Render(screenkit.Kit{}, selectlist.Spec{
			Width: 40, Cursor: 0, Rows: []selectlist.Row{{Left: row.Left, Right: row.Right}},
		}))
	}
	table := m.workflowInspectorGuardFields(rows[1], 80, 0)
	kicker, body := m.workflowInspectorGuardExample(rows[1])
	for _, line := range append(append(table, kicker), body...) {
		assertCandidateOutputSafe(t, line)
	}
}

func testCandidateHooksSafety(t *testing.T) {
	t.Parallel()
	payload := "safe\x1b[31mred\x1b]0;owned\a\x00\u009b\u009dend"
	m := Screen{}
	spec := config.HookSpec{On: payload, When: map[string]string{"operation": payload}, Notification: payload, Message: payload, DetailMessageField: payload}
	table := m.studioFieldTable(80, 0, hooksFieldOpts,
		m.studioFieldRows("01 // "+screenkit.Sanitize(spec.On), false, hookInspectorFields(m, spec, nil)...))
	kicker, columns, body := m.hooksHistoryContent(0, 80)
	body = append([]string{columns}, body...)
	for _, line := range append(append(table, kicker), body...) {
		assertCandidateOutputSafe(t, line)
	}
	left := "01 // " + screenkit.Sanitize(spec.On)
	right := screenkit.Sanitize(hookWhenShort(spec.When)) + "  notify"
	assertCandidateOutputSafe(t, selectlist.Render(screenkit.Kit{}, selectlist.Spec{
		Width: 40, Cursor: 0, Rows: []selectlist.Row{{Left: left, Right: right}},
	}))
}
