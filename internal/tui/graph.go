package tui

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// dagLine is a single render line in the ASCII dependency graph.
// taskID is 0 for blank separator lines between root trees.
type dagLine struct {
	taskID int64
	text   string
}

// buildDAGLines builds the ordered render lines for the ASCII dependency graph.
//
// Tree direction: parent = blocker (DependsOnTaskID); children = tasks it blocks (TaskID).
// Roots are tasks in the graph that have no blockers. Diamond patterns (same task reached
// via multiple paths) are rendered in full on first visit and shown with a back-reference
// annotation [→ #A, ...] on subsequent visits to avoid duplicating subtrees.
func buildDAGLines(deps []domain.TaskDependency, tasks []domain.Task) []dagLine {
	return buildDAGLinesSorted(deps, tasks, nil)
}

// buildDAGLinesSorted is buildDAGLines with a custom root ordering. When less
// is nil, roots are ordered by ascending task id (the legacy default). The TUI
// view layer passes a comparator built from `config.views.graph.sort` so the
// graph respects the user's preference.
func buildDAGLinesSorted(deps []domain.TaskDependency, tasks []domain.Task, less func(a, b domain.Task) bool) []dagLine {
	if len(deps) == 0 {
		return nil
	}

	taskByID := make(map[int64]domain.Task, len(tasks))
	for _, t := range tasks {
		taskByID[t.ID] = t
	}

	childrenOf := map[int64][]int64{}
	parentsOf  := map[int64][]int64{}
	inGraph    := map[int64]bool{}

	for _, d := range deps {
		childrenOf[d.DependsOnTaskID] = append(childrenOf[d.DependsOnTaskID], d.TaskID)
		parentsOf[d.TaskID]           = append(parentsOf[d.TaskID], d.DependsOnTaskID)
		inGraph[d.TaskID]             = true
		inGraph[d.DependsOnTaskID]    = true
	}

	for id := range childrenOf {
		sort.Slice(childrenOf[id], func(i, j int) bool { return childrenOf[id][i] < childrenOf[id][j] })
	}
	for id := range parentsOf {
		sort.Slice(parentsOf[id], func(i, j int) bool { return parentsOf[id][i] < parentsOf[id][j] })
	}

	var roots []int64
	for id := range inGraph {
		if len(parentsOf[id]) == 0 {
			roots = append(roots, id)
		}
	}
	if less != nil {
		sort.SliceStable(roots, func(i, j int) bool {
			a, aok := taskByID[roots[i]]
			b, bok := taskByID[roots[j]]
			// Tasks missing from the lookup (deps reference non-existent ids)
			// fall back to id ordering so the comparator never gets a zero
			// value with a meaningful Title.
			if !aok || !bok {
				return roots[i] < roots[j]
			}
			return less(a, b)
		})
	} else {
		sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	}

	var lines []dagLine
	visited := map[int64]bool{}

	var walk func(id int64, indent, connector string)
	walk = func(id int64, indent, connector string) {
		t, ok := taskByID[id]
		var title string
		if ok {
			title = t.Title
		}
		label := fmt.Sprintf("#%-4d %s", id, title)

		if visited[id] {
			pids := parentsOf[id]
			refs := make([]string, len(pids))
			for i, pid := range pids {
				refs[i] = fmt.Sprintf("→ #%d", pid)
			}
			lines = append(lines, dagLine{
				taskID: id,
				text:   indent + connector + label + "  [" + strings.Join(refs, ", ") + "]",
			})
			return
		}
		visited[id] = true

		lines = append(lines, dagLine{taskID: id, text: indent + connector + label})

		children := childrenOf[id]
		for i, child := range children {
			isLast := i == len(children)-1
			// continuation is what this node contributes to its children's prefix.
			// "├── " means this node has unrendered siblings below → show │ continuation.
			// "└── " means this is the last child → show blank continuation.
			// "" (root) contributes nothing: root trees are separated by blank lines.
			var continuation string
			switch connector {
			case "├── ":
				continuation = "│   "
			case "└── ":
				continuation = "    "
			default:
				continuation = ""
			}
			childConnector := "├── "
			if isLast {
				childConnector = "└── "
			}
			walk(child, indent+continuation, childConnector)
		}
	}

	for i, root := range roots {
		if i > 0 {
			lines = append(lines, dagLine{}) // blank separator between root trees
		}
		walk(root, "", "")
	}

	return lines
}

// dagSelectableIndices returns the indices into lines where taskID != 0 (navigable nodes).
func dagSelectableIndices(lines []dagLine) []int {
	out := make([]int, 0, len(lines))
	for i, l := range lines {
		if l.taskID != 0 {
			out = append(out, i)
		}
	}
	return out
}
