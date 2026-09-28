package graph

import (
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

// Line is one row of the projected dependency DAG: the node it carries and the
// text that draws it, indent glyphs and back-reference suffix included. TaskID
// is zero on the blank spacer emitted between two roots, which is what makes
// "is this line a node?" answerable without re-walking the graph.
type Line struct {
	TaskID int64
	Text   string
}

// RootOrder names the ordering applied to the projection's ROOTS. Field is
// "id" or "title"; Order is "asc" or "desc". Anything else falls back to the
// id ordering, and the zero value means "id asc" — the order the walk produces
// on its own. Subtrees are never reordered: a child list is always ascending
// by id so the same graph draws the same shape on every refresh.
type RootOrder struct {
	Field string
	Order string
}

// Lines projects a dependency slice into the DAG listing a tree view paints:
// one line per node in depth-first order under each root, a blank spacer
// between roots, and — for a node reached a second time — a back-reference row
// listing every parent instead of a repeated subtree.
//
// Only tasks named by an edge enter the projection; an isolated task has no
// place in a dependency tree. A graph with no edges projects nothing.
func Lines(deps []domain.TaskDependency, tasks []domain.Task, order RootOrder) []Line {
	if len(deps) == 0 {
		return nil
	}
	data := prepareLines(deps, tasks, order)
	walker := lineWalker{data: data, visited: map[int64]bool{}, lines: make([]Line, 0)}
	for i, root := range data.roots {
		if i > 0 {
			walker.lines = append(walker.lines, Line{})
		}
		walker.walk(root, "", "")
	}
	return walker.lines
}

type lineData struct {
	taskByID map[int64]domain.Task
	children map[int64][]int64
	parents  map[int64][]int64
	roots    []int64
}

func prepareLines(deps []domain.TaskDependency, tasks []domain.Task, order RootOrder) lineData {
	taskByID := make(map[int64]domain.Task, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID] = task
	}
	childrenOf := map[int64][]int64{}
	parentsOf := map[int64][]int64{}
	inGraph := map[int64]bool{}
	for _, dependency := range deps {
		childrenOf[dependency.DependsOnTaskID] = append(childrenOf[dependency.DependsOnTaskID], dependency.TaskID)
		parentsOf[dependency.TaskID] = append(parentsOf[dependency.TaskID], dependency.DependsOnTaskID)
		inGraph[dependency.TaskID], inGraph[dependency.DependsOnTaskID] = true, true
	}
	for id := range childrenOf {
		sort.Slice(childrenOf[id], func(i, j int) bool { return childrenOf[id][i] < childrenOf[id][j] })
	}
	for id := range parentsOf {
		sort.Slice(parentsOf[id], func(i, j int) bool { return parentsOf[id][i] < parentsOf[id][j] })
	}
	roots := rootsOf(inGraph, parentsOf, taskByID, order)
	return lineData{taskByID: taskByID, children: childrenOf, parents: parentsOf, roots: roots}
}

type lineWalker struct {
	data    lineData
	visited map[int64]bool
	lines   []Line
}

func (w *lineWalker) walk(id int64, indent, connector string) {
	label := fmt.Sprintf("#%-4d %s", id, w.data.taskByID[id].Title)
	if w.visited[id] {
		refs := make([]string, len(w.data.parents[id]))
		for i, parentID := range w.data.parents[id] {
			refs[i] = fmt.Sprintf("→ #%d", parentID)
		}
		w.lines = append(w.lines, Line{TaskID: id, Text: indent + connector + label + "  [" + strings.Join(refs, ", ") + "]"})
		return
	}
	w.visited[id] = true
	w.lines = append(w.lines, Line{TaskID: id, Text: indent + connector + label})
	children := w.data.children[id]
	continuation := lineContinuation(connector)
	for i, child := range children {
		childConnector := "├── "
		if i == len(children)-1 {
			childConnector = "└── "
		}
		w.walk(child, indent+continuation, childConnector)
	}
}

func lineContinuation(connector string) string {
	switch connector {
	case "├── ":
		return "│   "
	case "└── ":
		return "    "
	default:
		return ""
	}
}

// rootsOf is every node in the graph with no parent, in the order the caller
// asked for. The ordering is stable so two roots the comparator calls equal
// keep the ascending-id order the collection pass established.
func rootsOf(inGraph map[int64]bool, parentsOf map[int64][]int64, taskByID map[int64]domain.Task, order RootOrder) []int64 {
	var roots []int64
	for id := range inGraph {
		if len(parentsOf[id]) == 0 {
			roots = append(roots, id)
		}
	}
	less := rootLess(order)
	if less == nil {
		sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
		return roots
	}
	sort.SliceStable(roots, func(i, j int) bool {
		a, aok := taskByID[roots[i]]
		b, bok := taskByID[roots[j]]
		if !aok || !bok {
			return roots[i] < roots[j]
		}
		return less(a, b)
	})
	return roots
}

// rootLess turns a RootOrder into the comparator the root sort runs. It
// returns nil for the ascending-id order, which is the one case the walk can
// satisfy with a plain id sort rather than a stable comparator pass.
func rootLess(order RootOrder) func(domain.Task, domain.Task) bool {
	field, direction := order.Field, order.Order
	if field == "id" && direction == "asc" {
		return nil
	}
	asc := direction != "desc"
	if field == "title" {
		return func(a, b domain.Task) bool {
			if asc {
				return strings.ToLower(a.Title) < strings.ToLower(b.Title)
			}
			return strings.ToLower(a.Title) > strings.ToLower(b.Title)
		}
	}
	return func(a, b domain.Task) bool {
		if asc {
			return a.ID < b.ID
		}
		return a.ID > b.ID
	}
}
