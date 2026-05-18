package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestPlanNetworkCriticalPathPicksLongestChain proves the helper
// identifies the deepest blocker chain (A → B → C) and excludes
// isolated tasks (D) from the highlight set. Ties on depth break by
// lowest id for stable rendering across refreshes.
func TestPlanNetworkCriticalPathPicksLongestChain(t *testing.T) {
	tasks := []domain.PlanTaskRow{
		{TaskID: 1, Title: "A"},
		{TaskID: 2, Title: "B"},
		{TaskID: 3, Title: "C"},
		{TaskID: 4, Title: "D"},
	}
	deps := []domain.TaskDependency{
		{TaskID: 2, DependsOnTaskID: 1}, // B depends on A
		{TaskID: 3, DependsOnTaskID: 2}, // C depends on B
	}

	path := planNetworkCriticalPath(deps, tasks)
	if len(path) != 3 {
		t.Fatalf("path len = %d, want 3 (A→B→C): %+v", len(path), path)
	}
	for _, id := range []int64{1, 2, 3} {
		if !path[id] {
			t.Fatalf("task #%d missing from critical path: %+v", id, path)
		}
	}
	if path[4] {
		t.Fatal("isolated task #4 should not be on critical path")
	}
}

// TestPlanNetworkCriticalPathNilWhenNoDeps confirms zero-dep plans
// return a nil set so the renderer skips the ║ glyph entirely.
func TestPlanNetworkCriticalPathNilWhenNoDeps(t *testing.T) {
	tasks := []domain.PlanTaskRow{{TaskID: 1}, {TaskID: 2}}
	if path := planNetworkCriticalPath(nil, tasks); path != nil {
		t.Fatalf("path = %+v, want nil on zero-dep plan", path)
	}
}

// TestPlanNetworkCriticalPathSurvivesCycle proves the cycle guard
// prevents infinite recursion when an accidentally-circular
// dependency slips through (DB constraints normally block this; the
// helper still must not panic).
func TestPlanNetworkCriticalPathSurvivesCycle(t *testing.T) {
	tasks := []domain.PlanTaskRow{{TaskID: 1}, {TaskID: 2}}
	deps := []domain.TaskDependency{
		{TaskID: 1, DependsOnTaskID: 2},
		{TaskID: 2, DependsOnTaskID: 1},
	}
	_ = planNetworkCriticalPath(deps, tasks)
}

// TestPlanNetworkDependentIndexInvertsEdges proves the dependent
// lookup inverts the edge direction so the renderer can suffix
// "→ #N" on the blocker side without rescanning the slice per row.
func TestPlanNetworkDependentIndexInvertsEdges(t *testing.T) {
	deps := []domain.TaskDependency{
		{TaskID: 2, DependsOnTaskID: 1}, // 1 blocks 2
		{TaskID: 3, DependsOnTaskID: 1}, // 1 blocks 3
		{TaskID: 4, DependsOnTaskID: 2}, // 2 blocks 4
	}
	got := planNetworkDependentIndex(deps)
	if len(got[1]) != 2 || got[1][0] != 2 || got[1][1] != 3 {
		t.Fatalf("dependents[1] = %v, want [2 3]", got[1])
	}
	if len(got[2]) != 1 || got[2][0] != 4 {
		t.Fatalf("dependents[2] = %v, want [4]", got[2])
	}
	if _, ok := got[4]; ok {
		t.Fatalf("dependents[4] should not exist (4 is a leaf)")
	}
}

// TestPlanNetworkDepsFooterFormatsLine confirms the footer reads
// "Dependencies: #A→#B,#C  #D→#E" with stable ordering across
// refreshes.
func TestPlanNetworkDepsFooterFormatsLine(t *testing.T) {
	deps := []domain.TaskDependency{
		{TaskID: 3, DependsOnTaskID: 1},
		{TaskID: 3, DependsOnTaskID: 2},
		{TaskID: 4, DependsOnTaskID: 3},
	}
	got := planNetworkDepsFooter(deps)
	want := "Dependencies: #3→#1,#2  #4→#3"
	if got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

// TestPlanNetworkDepsFooterEmpty confirms zero-dep plans return an
// empty string so the renderer can skip writing the footer line.
func TestPlanNetworkDepsFooterEmpty(t *testing.T) {
	if got := planNetworkDepsFooter(nil); got != "" {
		t.Fatalf("footer = %q, want empty", got)
	}
}
