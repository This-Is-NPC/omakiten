package plan

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestBuildWaveRailsDFSPreOrder proves the DFS pre-order rail builder:
//   - tasks render in input order (NOT readiness-sorted)
//   - intra-wave parent → child rails render as ├─/└─ INSIDE the row
//     body, with │ continuations on intervening sibling rows.
func TestBuildWaveRailsDFSPreOrder(t *testing.T) {
	tasks := []domain.PlanTaskRow{
		{TaskID: 1, Title: "root-a"},
		{TaskID: 2, Title: "child-of-1"},
		{TaskID: 3, Title: "child-of-1"},
		{TaskID: 4, Title: "grandchild-of-2"},
		{TaskID: 5, Title: "root-b"},
	}
	intraBlockers := map[int64][]int64{
		2: {1},
		3: {1},
		4: {2},
	}
	layout := BuildWaveRails(tasks, intraBlockers)
	if len(layout.OrderedIdx) != 5 {
		t.Fatalf("ordered = %d, want 5", len(layout.OrderedIdx))
	}
	// DFS pre-order: 1 → 2 → 4 → 3 → 5. The branch under #1 fully
	// expands before #5 (the sibling root) appears.
	want := []struct {
		taskID   int64
		parentID int64
		railHas  string
	}{
		{1, 0, ""},
		{2, 1, "├─"},
		{4, 2, "└─"},
		{3, 1, "└─"},
		{5, 0, ""},
	}
	for pos, w := range want {
		gotID := tasks[layout.OrderedIdx[pos]].TaskID
		gotParent := layout.ParentByPos[pos]
		if gotID != w.taskID || gotParent != w.parentID {
			t.Fatalf("layout[%d] = (task=%d parent=%d), want (task=%d parent=%d)",
				pos, gotID, gotParent, w.taskID, w.parentID)
		}
		if w.railHas != "" && !strings.Contains(layout.Rails[pos], w.railHas) {
			t.Fatalf("rail[#%d] = %q, want contains %q", gotID, layout.Rails[pos], w.railHas)
		}
	}
}

// TestBuildWaveRailsRejectsTopologicalReorder pins the design decision: even
// when a later root would be "more ready" than an earlier task with children,
// the rail builder keeps input order. The previously-attempted topological
// reorder is not allowed to return.
func TestBuildWaveRailsRejectsTopologicalReorder(t *testing.T) {
	tasks := []domain.PlanTaskRow{
		{TaskID: 10, Title: "blocked-by-nothing"},
		{TaskID: 20, Title: "blocks-30"},
		{TaskID: 30, Title: "blocked-by-20"},
		{TaskID: 40, Title: "blocked-by-nothing-too"},
	}
	intraBlockers := map[int64][]int64{30: {20}}
	layout := BuildWaveRails(tasks, intraBlockers)
	wantOrder := []int64{10, 20, 30, 40}
	for pos, want := range wantOrder {
		gotID := tasks[layout.OrderedIdx[pos]].TaskID
		if gotID != want {
			t.Fatalf("rails ordered[%d] = #%d, want #%d (input order, NOT readiness)", pos, gotID, want)
		}
	}
}
