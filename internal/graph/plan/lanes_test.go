package plan

import (
	"testing"
)

// TestLanesGreedyAllocation proves overlapping cross-wave sources allocate
// distinct lanes. Two sources whose destination ranges overlap must land in
// lanes 0 and 1; a third source whose range starts after the first lane has
// freed collapses back to lane 0.
func TestLanesGreedyAllocation(t *testing.T) {
	rowByTask := map[int64]int{
		1: 0,
		2: 1,
		3: 2, // dst of #1
		4: 3, // dst of #2
		5: 4, // src
		6: 5, // dst of #5
	}
	cross := map[int64][]int64{
		3: {1},
		4: {2},
		6: {5},
	}
	filaments, laneCount := Lanes(rowByTask, cross)
	if laneCount != 2 {
		t.Fatalf("laneCount = %d, want 2", laneCount)
	}
	bySrc := map[int]Filament{}
	for _, f := range filaments {
		bySrc[f.SrcRow] = f
	}
	if bySrc[0].Lane != 0 {
		t.Fatalf("filament from #1 (row 0) lane = %d, want 0", bySrc[0].Lane)
	}
	if bySrc[1].Lane != 1 {
		t.Fatalf("filament from #2 (row 1) lane = %d, want 1 (overlaps lane 0)", bySrc[1].Lane)
	}
	if bySrc[4].Lane != 0 {
		t.Fatalf("filament from #5 (row 4) lane = %d, want 0 (lane 0 freed after row 2)", bySrc[4].Lane)
	}
}

// TestLanesHubReusesOneLane proves ONE source with N dependents collapses to
// ONE lane, not N. A hub task #1 blocking #2, #3, #4 emits a single filament
// with three destination rows; the lane count is 1.
func TestLanesHubReusesOneLane(t *testing.T) {
	rowByTask := map[int64]int{
		1: 0, // hub src
		2: 1, // dst
		3: 2, // dst
		4: 3, // dst
	}
	cross := map[int64][]int64{
		2: {1},
		3: {1},
		4: {2, 1}, // #4 depends on hub AND on #2
	}
	filaments, laneCount := Lanes(rowByTask, cross)
	if laneCount != 2 {
		t.Fatalf("laneCount = %d, want 2 (hub reuse + #2's single edge to #4)", laneCount)
	}
	var hub Filament
	found := false
	for _, f := range filaments {
		if f.SrcRow == 0 {
			hub = f
			found = true
		}
	}
	if !found {
		t.Fatalf("hub filament missing from %+v", filaments)
	}
	if len(hub.DstRows) != 3 || hub.DstRows[0] != 1 || hub.DstRows[1] != 2 || hub.DstRows[2] != 3 {
		t.Fatalf("hub DstRows = %v, want [1 2 3] (lane reused for all 3 dependents)", hub.DstRows)
	}
}

// TestLanesDropsUnplacedSource proves cross-wave edges whose source has no row
// are dropped from the filament list. The caller still surfaces the blocker as
// text through its regular annotation path.
func TestLanesDropsUnplacedSource(t *testing.T) {
	rowByTask := map[int64]int{2: 2} // #1 was never placed
	cross := map[int64][]int64{2: {1}}
	filaments, laneCount := Lanes(rowByTask, cross)
	if laneCount != 0 || len(filaments) != 0 {
		t.Fatalf("filaments = %v laneCount = %d, want empty (source row missing)", filaments, laneCount)
	}
}

// TestFilamentEndRow pins the reach of a filament with and without
// destinations — the value the lane painter tests "still in flight" against.
func TestFilamentEndRow(t *testing.T) {
	if got := (Filament{SrcRow: 3, DstRows: []int{5, 9}}).EndRow(); got != 9 {
		t.Fatalf("EndRow = %d, want 9 (last destination)", got)
	}
	if got := (Filament{SrcRow: 3}).EndRow(); got != 3 {
		t.Fatalf("EndRow = %d, want 3 (source row when it reaches nothing)", got)
	}
}
