package plan

import (
	"sort"
)

// Filament is one source task's cross-wave fan-out routed into a lane: the row
// it starts on, the ascending rows it reaches, and the lane slot it occupies.
// ONE filament per SOURCE, not per edge — a source with N dependents reuses the
// same lane and branches off at each destination row.
type Filament struct {
	SrcRow  int
	DstRows []int
	Lane    int
}

// EndRow is the last destination row this filament reaches, or its source row
// when it reaches none.
func (f Filament) EndRow() int {
	if len(f.DstRows) == 0 {
		return f.SrcRow
	}
	return f.DstRows[len(f.DstRows)-1]
}

// Lanes maps cross-wave dependencies onto lane columns, ONE LANE PER SOURCE
// TASK, and returns the filaments together with the number of distinct lanes
// used (= the maximum overlap across the row range). A hub task with seven
// dependents collapses to a single lane, not seven.
//
// Allocation is greedy: pending filaments sort by (srcRow, endRow, srcTaskID)
// and take the leftmost lane whose previous filament ends strictly before the
// new one starts. The tie-break on source id is what makes the layout stable
// across re-renders, since the pending set is collected from a map.
//
// rowByTask places every task that HAS a row; an edge whose source or
// destination is absent — a collapsed wave, a task outside the view — is
// dropped, as is an edge that would have to run upward.
func Lanes(rowByTask map[int64]int, crossBlockers map[int64][]int64) ([]Filament, int) {
	if len(rowByTask) == 0 || len(crossBlockers) == 0 {
		return nil, 0
	}

	queue := collectPending(rowByTask, crossBlockers)
	return assignLanes(queue)
}

type pendingLane struct {
	src   int
	dsts  []int
	srcID int64
}

func collectPending(rowByTask map[int64]int, crossBlockers map[int64][]int64) []pendingLane {
	srcRow := map[int64]int{}
	dstSet := map[int64]map[int]struct{}{}
	for dstID, blockers := range crossBlockers {
		dRow, dok := rowByTask[dstID]
		if !dok {
			continue
		}
		for _, srcID := range blockers {
			sRow, sok := rowByTask[srcID]
			if !sok || sRow >= dRow {
				continue
			}
			if dstSet[srcID] == nil {
				dstSet[srcID] = map[int]struct{}{}
			}
			dstSet[srcID][dRow] = struct{}{}
			srcRow[srcID] = sRow
		}
	}
	return sortPending(srcRow, dstSet)
}

func sortPending(srcRow map[int64]int, dstSet map[int64]map[int]struct{}) []pendingLane {
	queue := make([]pendingLane, 0, len(dstSet))
	for srcID, dsts := range dstSet {
		dlist := make([]int, 0, len(dsts))
		for d := range dsts {
			dlist = append(dlist, d)
		}
		sort.Ints(dlist)
		queue = append(queue, pendingLane{src: srcRow[srcID], dsts: dlist, srcID: srcID})
	}
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].src != queue[j].src {
			return queue[i].src < queue[j].src
		}
		li := queue[i].dsts[len(queue[i].dsts)-1]
		lj := queue[j].dsts[len(queue[j].dsts)-1]
		if li != lj {
			return li < lj
		}
		return queue[i].srcID < queue[j].srcID
	})
	return queue
}

func assignLanes(queue []pendingLane) ([]Filament, int) {
	if len(queue) == 0 {
		return nil, 0
	}
	var laneEnd []int
	out := make([]Filament, 0, len(queue))
	for _, p := range queue {
		slot := -1
		for li, end := range laneEnd {
			if end < p.src {
				slot = li
				break
			}
		}
		if slot == -1 {
			slot = len(laneEnd)
			laneEnd = append(laneEnd, 0)
		}
		laneEnd[slot] = p.dsts[len(p.dsts)-1]
		out = append(out, Filament{SrcRow: p.src, DstRows: p.dsts, Lane: slot})
	}
	return out, len(laneEnd)
}
