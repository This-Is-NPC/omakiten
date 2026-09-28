package settings

import (
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/settingsprojection"
	"omakiten/internal/tui/screens/screentest"
)

func TestRenderPreservesGuardMatrixSemanticsAndOrdering(t *testing.T) {
	bundle := matrixBundle([]config.TransitionGuard{{Type: "comments_tagged"}})
	snapshot := config.BuildSnapshot(bundle)
	workflow := snapshot.Workflow()
	workflow.Buckets[0], workflow.Buckets[2] = workflow.Buckets[2], workflow.Buckets[0]

	kit := screentest.FrameAt(t, 120, 40).Kit()
	view := screentest.StripANSI(renderGuardMatrix(kit, kit.AvailableWidth(), guardInput{
		Snapshot: snapshot, Workflow: workflow, Label: "guards",
		From: "from", To: "to", Disallowed: "—", Empty: "[empty]",
	}))
	if !strings.Contains(view, "comments_tagged") || !strings.Contains(view, "[empty]") || strings.Count(view, "—") < 2 {
		t.Fatalf("matrix does not expose all four cell states:\n%s", view)
	}
	if backlog, dev, done := strings.Index(view, "BACKLOG"), strings.Index(view, "DEV"), strings.Index(view, "DONE"); backlog >= dev || dev >= done {
		t.Fatalf("bucket headers not ordered by position: backlog=%d dev=%d done=%d\n%s", backlog, dev, done, view)
	}
}

func TestGuardMatricesEqualIgnoresDeclarationOrderButDetectsGuardChanges(t *testing.T) {
	a := matrixBundle([]config.TransitionGuard{{Type: "comments_tagged", Tag: "reviewed", Count: 1}, {Type: "tests_passing"}})
	b := matrixBundle([]config.TransitionGuard{{Type: "tests_passing"}, {Type: "comments_tagged", Tag: "reviewed", Count: 1}})
	if !settingsprojection.GuardMatricesEqual(config.BuildSnapshot(a), config.BuildSnapshot(b)) {
		t.Fatal("equivalent guard declarations should produce equal matrices")
	}
	b.Workflows[0].Transitions[0].Guards[1].Count = 2
	if settingsprojection.GuardMatricesEqual(config.BuildSnapshot(a), config.BuildSnapshot(b)) {
		t.Fatal("guard payload change should produce divergent matrices")
	}
}

func TestRenderEmptyAndNilSnapshot(t *testing.T) {
	kit := screentest.FrameAt(t, 80, 20).Kit()
	if got := renderGuardMatrix(kit, kit.AvailableWidth(), guardInput{Label: "guards", From: "from", To: "to", Disallowed: "—", Empty: "[empty]"}); !strings.Contains(got, "GUARDS") {
		t.Fatalf("empty matrix must retain its labelled presentation: %q", got)
	}
	bundle := matrixBundle(nil)
	workflow := config.BuildSnapshot(bundle).Workflow()
	if got := renderGuardMatrix(kit, kit.AvailableWidth(), guardInput{Workflow: workflow, Label: "guards", From: "from", To: "to", Disallowed: "—", Empty: "[empty]"}); !strings.Contains(got, "[empty]") {
		t.Fatalf("nil snapshot must degrade allowed transitions to empty: %q", got)
	}
}

func matrixBundle(guards []config.TransitionGuard) config.Bundle {
	return config.Bundle{
		Kit: config.Kit{Key: "root"}, Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Buckets: []config.Bucket{
			{ID: 1, Key: "backlog", Position: 1}, {ID: 2, Key: "dev", Position: 2}, {ID: 3, Key: "done", Position: 3},
		}, Transitions: []config.Transition{{From: 1, To: 2, Guards: guards}, {From: 2, To: 3}}}},
	}
}
