package plan

import "omakiten/internal/domain"

// CriticalPath is the set of task ids on the longest blocker chain inside a
// plan. Tasks are scored by the depth of their longest blocker chain (memoised
// DFS over the dependency graph); the deepest task and every blocker reachable
// from it form the path. Returns nil when the plan has fewer than two chained
// tasks — no chain, no path worth highlighting. Ties on the deepest endpoint
// break by lowest task id so the answer is stable across reads, and a cycle is
// survived rather than recursed into.
func CriticalPath(deps []domain.TaskDependency, tasks []domain.PlanTaskRow) map[int64]bool {
	if len(deps) == 0 || len(tasks) == 0 {
		return nil
	}
	blockers := map[int64][]int64{}
	for _, d := range deps {
		blockers[d.TaskID] = append(blockers[d.TaskID], d.DependsOnTaskID)
	}
	memoDepth := criticalDepths(blockers)
	bestID, bestDepth := criticalEndpoint(tasks, memoDepth, blockers)
	if bestDepth == 0 {
		return nil
	}
	return criticalPath(bestID, blockers, memoDepth)
}

func criticalDepths(blockers map[int64][]int64) map[int64]int {
	memoDepth := map[int64]int{}
	var depth func(id int64, seen map[int64]bool) int
	depth = func(id int64, seen map[int64]bool) int {
		if d, ok := memoDepth[id]; ok {
			return d
		}
		if seen[id] {
			return 0
		}
		seen[id] = true
		best := 0
		for _, b := range blockers[id] {
			if d := depth(b, seen); d+1 > best {
				best = d + 1
			}
		}
		delete(seen, id)
		memoDepth[id] = best
		return best
	}
	for id := range blockers {
		depth(id, map[int64]bool{})
	}
	return memoDepth
}

func criticalEndpoint(tasks []domain.PlanTaskRow, memoDepth map[int64]int, blockers map[int64][]int64) (int64, int) {
	bestID := int64(0)
	bestDepth := 0
	for _, t := range tasks {
		d := memoDepth[t.TaskID]
		if d == 0 && len(blockers[t.TaskID]) > 0 {
			d = criticalDepth(t.TaskID, blockers, memoDepth, map[int64]bool{})
		}
		if d > bestDepth || (d == bestDepth && bestID == 0) || (d == bestDepth && t.TaskID < bestID) {
			bestID = t.TaskID
			bestDepth = d
		}
	}
	return bestID, bestDepth
}

func criticalDepth(id int64, blockers map[int64][]int64, memo map[int64]int, seen map[int64]bool) int {
	if d, ok := memo[id]; ok {
		return d
	}
	if seen[id] {
		return 0
	}
	seen[id] = true
	best := 0
	for _, blocker := range blockers[id] {
		if d := criticalDepth(blocker, blockers, memo, seen); d+1 > best {
			best = d + 1
		}
	}
	delete(seen, id)
	memo[id] = best
	return best
}

func criticalPath(bestID int64, blockers map[int64][]int64, memoDepth map[int64]int) map[int64]bool {
	path := map[int64]bool{bestID: true}
	current := bestID
	for {
		next := int64(0)
		bestSubDepth := -1
		for _, b := range blockers[current] {
			d := memoDepth[b]
			if d > bestSubDepth || (d == bestSubDepth && (next == 0 || b < next)) {
				next = b
				bestSubDepth = d
			}
		}
		if next == 0 || path[next] {
			break
		}
		path[next] = true
		current = next
	}
	return path
}
