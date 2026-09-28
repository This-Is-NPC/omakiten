package plan

import (
	"omakiten/internal/domain"
)

// WaveRails is the shape of one wave once its intra-wave dependency edges are
// resolved into a tree: the wave's tasks in DFS pre-order, the rail glyph
// prefix per ordered position, and the rail parent of each position so a
// reader can tell the edge it already drew as structure apart from the ones it
// still has to name.
type WaveRails struct {
	OrderedIdx  []int    // indexes into the input task slice, in render order
	Rails       []string // rail prefix per ordered position (├─ / └─ / │ / etc.)
	ParentByPos []int64  // intra-wave rail parent id per ordered position (0 = root)
}

// BuildWaveRails projects a wave's tasks into DFS pre-order over their
// intra-wave parent tree. The rail parent of each task is the lowest-id
// intra-wave blocker that appears BEFORE it in input order; a task with no such
// blocker surfaces as a root, which is also how a back-edge is kept from
// drawing upward. Each position's prefix uses the standard tree glyphs:
//   - "├─" for a non-last child of its parent,
//   - "└─" for the last child,
//   - "│ " or "  " per ancestor lane (continuing vs. closed).
//
// Topological reorder is explicitly NOT performed — the user rejected it. Input
// order survives within sibling groups.
func BuildWaveRails(tasks []domain.PlanTaskRow, intraBlockers map[int64][]int64) WaveRails {
	n := len(tasks)
	if n == 0 {
		return WaveRails{}
	}
	idxByID := make(map[int64]int, n)
	for i, t := range tasks {
		idxByID[t.TaskID] = i
	}
	parentOf := railParents(tasks, idxByID, intraBlockers)
	children, roots := railTree(tasks, parentOf)
	return walkRails(tasks, idxByID, children, roots)
}

func railParents(tasks []domain.PlanTaskRow, idxByID map[int64]int, intraBlockers map[int64][]int64) map[int64]int64 {
	// Intra-wave rail parent = lowest-id intra-wave blocker present in the
	// wave. The DFS only descends parent→child where the parent appears BEFORE
	// the child in input order; back-edges revert the child to a root so the
	// rail never tries to draw upward.
	parentOf := make(map[int64]int64, len(tasks))
	for i, t := range tasks {
		var pid int64
		for _, b := range intraBlockers[t.TaskID] {
			pi, ok := idxByID[b]
			if !ok || pi >= i {
				continue
			}
			if pid == 0 || b < pid {
				pid = b
			}
		}
		if pid != 0 {
			parentOf[t.TaskID] = pid
		}
	}
	return parentOf
}

func railTree(tasks []domain.PlanTaskRow, parentOf map[int64]int64) (map[int64][]int64, []int64) {
	// Children-by-parent retain input order so siblings render in the order the
	// user typed them.
	children := map[int64][]int64{}
	var roots []int64
	for _, t := range tasks {
		if pid, ok := parentOf[t.TaskID]; ok {
			children[pid] = append(children[pid], t.TaskID)
		} else {
			roots = append(roots, t.TaskID)
		}
	}
	return children, roots
}

func walkRails(tasks []domain.PlanTaskRow, idxByID map[int64]int, children map[int64][]int64, roots []int64) WaveRails {
	n := len(tasks)
	orderedIdx := make([]int, 0, n)
	parentByPos := make([]int64, 0, n)
	rails := make([]string, 0, n)

	// Iterative DFS pre-order. The stack stores the ancestor prefix ("│ " for
	// still-open lanes, "  " for closed) plus the current node's last-sibling
	// flag — that lets each emitted row paint its glyph (├─ / └─) and the next
	// descent extend the prefix.
	type frame struct {
		id     int64
		parent int64
		prefix string
		isLast bool
	}
	stack := make([]frame, 0, n)
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{id: roots[i], parent: 0, prefix: "", isLast: i == len(roots)-1})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		rails = append(rails, f.prefix+railGlyph(f.parent, f.isLast))
		orderedIdx = append(orderedIdx, idxByID[f.id])
		parentByPos = append(parentByPos, f.parent)

		kids := children[f.id]
		if len(kids) == 0 {
			continue
		}
		childPrefix := railChildPrefix(f.prefix, f.parent, f.isLast)
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, frame{
				id:     kids[i],
				parent: f.id,
				prefix: childPrefix,
				isLast: i == len(kids)-1,
			})
		}
	}

	return WaveRails{
		OrderedIdx:  orderedIdx,
		Rails:       rails,
		ParentByPos: parentByPos,
	}
}

func railGlyph(parent int64, isLast bool) string {
	if parent == 0 {
		return ""
	}
	if isLast {
		return "└─"
	}
	return "├─"
}

func railChildPrefix(prefix string, parent int64, isLast bool) string {
	if parent == 0 {
		return prefix
	}
	if isLast {
		return prefix + "  "
	}
	return prefix + "│ "
}
