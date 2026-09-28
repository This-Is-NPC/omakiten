package plannetwork

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
)

func networkTestFrame(width, height int) screenhost.Frame {
	text := map[string]string{
		"tui.status.cancelled":           "Cancelled.",
		"tui.footer.save":                "save",
		"tui.footer.cancel":              "cancel",
		"tui.plans.network.header_fmt":   "// PLAN · %s · %d/%d · %d%%",
		"tui.plans.network.no_waves_fmt": "Plan %s has no waves.",
	}
	return screenhost.NewFrame(screenhost.FrameOptions{Width: width, Height: height, Text: func(key string) string {
		if value := text[key]; value != "" {
			return value
		}
		return key
	}})
}

func networkPayload() Payload {
	return Payload{Show: domain.PlanShow{
		Plan: domain.Plan{ID: 7, Slug: "rollout", GoalBody: "original"},
		Waves: []domain.PlanWaveView{{
			Wave:  domain.PlanWave{ID: 10, Name: "Foundation", Position: 1},
			Tasks: []domain.PlanTaskRow{{TaskID: 42, WaveID: 10, Title: "Build", BucketKey: "backlog"}},
		}},
	}}
}

func key(value string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)} }

func TestStateMachineCancelsGoalAndAssignmentLocally(t *testing.T) {
	screen := New().Open(networkPayload())
	out := screen.Update(networkTestFrame(100, 30), key("e"))
	goal := out.Screen.(Screen)
	if goal.Mode() != ModeGoal || goal.EditorValue() != "original" {
		t.Fatalf("goal state = mode %v value %q", goal.Mode(), goal.EditorValue())
	}
	out = goal.Update(networkTestFrame(100, 30), tea.KeyMsg{Type: tea.KeyEsc})
	cancelled := out.Screen.(Screen)
	if cancelled.Mode() != ModeBrowse || out.Action.Kind != screenhost.ActionSetStatus {
		t.Fatalf("goal cancel outcome = mode %v action %v", cancelled.Mode(), out.Action.Kind)
	}

	cancelled = cancelled.SelectCursor(1)
	assign := cancelled.Update(networkTestFrame(100, 30), key("c")).Screen.(Screen)
	if assign.Mode() != ModeAssign || assign.AssignTaskID() != 42 {
		t.Fatalf("assign state = mode %v task %d", assign.Mode(), assign.AssignTaskID())
	}
	out = assign.Update(networkTestFrame(100, 30), tea.KeyMsg{Type: tea.KeyEsc})
	if got := out.Screen.(Screen); got.Mode() != ModeBrowse || got.AssignTaskID() != 0 {
		t.Fatalf("assign cancel leaked state: mode %v task %d", got.Mode(), got.AssignTaskID())
	}
}

func TestStateMachineEmitsTypedSaveAndAssignOutcomes(t *testing.T) {
	frame := networkTestFrame(100, 30)
	screen := New().Open(networkPayload())
	goal := screen.Update(frame, key("e")).Screen.(Screen).WithEditorValue("rewritten")
	out := goal.Update(frame, tea.KeyMsg{Type: tea.KeyCtrlS})
	if out.Action.Kind != screenhost.ActionSavePlanGoal || out.Action.PlanID != 7 || out.Action.PlanSlug != "rollout" || out.Action.Value != "rewritten" {
		t.Fatalf("goal outcome = %#v", out.Action)
	}

	assign := out.Screen.(Screen).SelectCursor(1).Update(frame, key("c")).Screen.(Screen).WithEditorValue("alice")
	out = assign.Update(frame, tea.KeyMsg{Type: tea.KeyEnter})
	if out.Action.Kind != screenhost.ActionSetTaskAssignee || out.Action.TaskID != 42 || out.Action.PlanSlug != "rollout" || out.Action.Value != "alice" {
		t.Fatalf("assign outcome = %#v", out.Action)
	}
}

func TestResizePreservesSelectionAndClampsScroll(t *testing.T) {
	screen := New().Open(networkPayload()).SelectCursor(1)
	out := screen.Lifecycle(networkTestFrame(48, 12), screenhost.LifecycleResize)
	resized := out.Screen.(Screen)
	if resized.Cursor() != 1 {
		t.Fatalf("cursor after resize = %d, want 1", resized.Cursor())
	}
	if resized.RowCount() != 2 {
		t.Fatalf("row count after resize = %d, want 2", resized.RowCount())
	}
}

func TestScreenSurfaceAndNavigationBranches(t *testing.T) {
	frame := networkTestFrame(120, 28)
	base := New().Open(networkPayload())
	assertPlanNetworkBrowseSurface(t, frame, base)
	assertPlanNetworkEditors(t, frame, base)
}

func assertPlanNetworkBrowseSurface(t *testing.T, frame screenhost.Frame, base Screen) {
	t.Helper()
	if base.ID() != screenhost.PlanNetwork || !base.OwnsKey(key("j")) || !base.OwnsFooter() || base.BlocksHostInput() {
		t.Fatal("screen capabilities do not describe browse mode")
	}
	if len(base.Footer(frame)) == 0 || len(base.Help(frame)) == 0 {
		t.Fatal("browse chrome is empty")
	}
	if view := base.View(frame); view == "" {
		t.Fatal("populated network view is empty")
	}
	if view := base.View(networkTestFrame(38, 12)); view == "" {
		t.Fatal("narrow network view is empty")
	}
	keys := []tea.KeyMsg{
		key("j"), key("k"), key(" "), key("h"), key("l"),
		{Type: tea.KeyPgUp}, {Type: tea.KeyPgDown}, {Type: tea.KeyHome}, {Type: tea.KeyEnd},
		key("g"), key("G"), key("r"), tea.KeyMsg{Type: tea.KeyEsc},
	}
	for _, msg := range keys {
		out := base.Update(frame, msg)
		if out.Screen == nil {
			t.Fatalf("key %q returned nil screen", msg.String())
		}
	}

}

func assertPlanNetworkEditors(t *testing.T, frame screenhost.Frame, base Screen) {
	t.Helper()
	goal := base.Update(frame, key("e")).Screen.(Screen)
	if !goal.BlocksHostInput() || len(goal.Footer(frame)) == 0 || goal.View(frame) == "" {
		t.Fatal("goal mode surface incomplete")
	}
	if root := goal.root(frame); !root.IsLeaf() || root.Spec.ID != sectionGoal {
		t.Fatalf("goal root = leaf:%v id:%q, want the goal Cell", root.IsLeaf(), root.Spec.ID)
	}
	assign := base.SelectCursor(1).Update(frame, key("c")).Screen.(Screen)
	if !assign.BlocksHostInput() || len(assign.Footer(frame)) == 0 || assign.View(frame) == "" {
		t.Fatal("assign mode surface incomplete")
	}
	if root := assign.root(frame); !root.IsLeaf() || root.Spec.ID != sectionAssign {
		t.Fatalf("assign root = leaf:%v id:%q, want the assign Cell", root.IsLeaf(), root.Spec.ID)
	}
	empty := New().Open(Payload{Show: domain.PlanShow{Plan: domain.Plan{Slug: "empty"}}})
	if root := empty.root(frame); !root.IsLeaf() || root.Spec.ID != sectionEmpty {
		t.Fatalf("empty root = leaf:%v id:%q, want the empty Cell", root.IsLeaf(), root.Spec.ID)
	}
	if out := base.SelectCursor(1).Update(frame, tea.KeyMsg{Type: tea.KeyEnter}); out.Action.Kind != screenhost.ActionOpenTask {
		t.Fatalf("task enter outcome = %v", out.Action.Kind)
	}
}
