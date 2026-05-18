package tui

import (
	"strings"
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

// TestPlanNetworkGutterDrawsHorizontalArrow proves the gutter router
// emits a horizontal line + arrow head for a single cross-wave
// dependency where source and destination cards share the same row.
func TestPlanNetworkGutterDrawsHorizontalArrow(t *testing.T) {
	rows := renderPlanNetworkGutter(6, 5, []planNetworkGutterEdge{{SrcY: 2, DstY: 2}})
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	// Row 2 should carry the horizontal line ending in an arrow head.
	if !strings.Contains(rows[2], "─") {
		t.Fatalf("row 2 missing horizontal line: %q", rows[2])
	}
	if !strings.Contains(rows[2], "►") {
		t.Fatalf("row 2 missing arrow head: %q", rows[2])
	}
	// Other rows should be blank.
	for i, r := range rows {
		if i == 2 {
			continue
		}
		if strings.TrimSpace(r) != "" {
			t.Fatalf("row %d non-blank for single edge: %q", i, r)
		}
	}
}

// TestPlanNetworkGutterRoutesBend confirms the router draws a
// horizontal-vertical-horizontal path with ┐/┘/┌/└ bends when the
// source and destination cards do not share a row.
func TestPlanNetworkGutterRoutesBend(t *testing.T) {
	rows := renderPlanNetworkGutter(6, 6, []planNetworkGutterEdge{{SrcY: 1, DstY: 4}})
	joined := strings.Join(rows, "\n")
	// Bend glyphs surface: descending (left turn ┐ + right turn └)
	// or the equivalents depending on midX placement. Assert at
	// least one of the descending corners is rendered.
	if !strings.ContainsAny(joined, "┐└┌┘") {
		t.Fatalf("router did not emit any bend glyph:\n%s", joined)
	}
	if !strings.Contains(joined, "│") {
		t.Fatalf("router did not emit vertical segment:\n%s", joined)
	}
	if !strings.Contains(joined, "►") {
		t.Fatalf("router did not emit arrow head:\n%s", joined)
	}
}

// TestPlanNetworkJunctionCoversFourWay confirms the junction table
// renders the 4-way crossing (├ ┤ ┴ ┬ ┼) for overlapping edges.
func TestPlanNetworkJunctionCoversFourWay(t *testing.T) {
	cases := map[uint8]rune{
		dirN | dirS:                       '│',
		dirE | dirW:                       '─',
		dirS | dirE:                       '┌',
		dirN | dirE:                       '└',
		dirN | dirS | dirE:                '├',
		dirN | dirS | dirW:                '┤',
		dirN | dirE | dirW:                '┴',
		dirS | dirE | dirW:                '┬',
		dirN | dirS | dirE | dirW:         '┼',
		dirArrow:                          '►',
	}
	for bits, want := range cases {
		if got := planNetworkJunction(bits); got != want {
			t.Errorf("junction(%08b) = %q, want %q", bits, got, want)
		}
	}
}

// TestPlanNetworkSkipEdgesFiltersAdjacentAndIntra confirms the
// helper returns only edges that span 2+ wave boundaries; adjacent
// (gutter-routed) and intra-wave edges are excluded because they
// have their own surfaces.
func TestPlanNetworkSkipEdgesFiltersAdjacentAndIntra(t *testing.T) {
	taskToWave := map[int64]int64{
		10: 100, // task 10 in wave W1
		20: 200, // task 20 in wave W2
		30: 300, // task 30 in wave W3
		11: 100, // task 11 in wave W1
	}
	waveToIdx := map[int64]int{100: 0, 200: 1, 300: 2}
	deps := []domain.TaskDependency{
		{TaskID: 11, DependsOnTaskID: 10}, // intra W1 — skip
		{TaskID: 20, DependsOnTaskID: 10}, // adjacent W1→W2 — skip
		{TaskID: 30, DependsOnTaskID: 10}, // skip W1→W3 — KEEP
	}
	got := planNetworkSkipEdges(deps, taskToWave, waveToIdx)
	if len(got) != 1 {
		t.Fatalf("skip edges = %d, want 1: %+v", len(got), got)
	}
	if got[0].SrcIdx != 0 || got[0].DstIdx != 2 || got[0].SrcTaskID != 10 || got[0].DstTaskID != 30 {
		t.Fatalf("unexpected skip edge: %+v", got[0])
	}
}

// TestPlanNetworkSkipEdgesEmptyOnNoSkips verifies the helper returns
// nil when every edge is adjacent or intra-wave, so the backplane
// band stays unrendered (zero visual cost when there's no signal).
func TestPlanNetworkSkipEdgesEmptyOnNoSkips(t *testing.T) {
	taskToWave := map[int64]int64{10: 100, 20: 200}
	waveToIdx := map[int64]int{100: 0, 200: 1}
	deps := []domain.TaskDependency{
		{TaskID: 20, DependsOnTaskID: 10}, // adjacent — not a skip
	}
	if got := planNetworkSkipEdges(deps, taskToWave, waveToIdx); len(got) != 0 {
		t.Fatalf("skip edges = %+v, want empty on no-skip plan", got)
	}
}
