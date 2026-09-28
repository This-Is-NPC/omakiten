package graph

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func diamondTasks() []domain.Task {
	return []domain.Task{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}, {ID: 3, Title: "C"}, {ID: 4, Title: "D"}}
}

func diamondDeps() []domain.TaskDependency {
	return []domain.TaskDependency{
		{TaskID: 2, DependsOnTaskID: 1},
		{TaskID: 3, DependsOnTaskID: 1},
		{TaskID: 4, DependsOnTaskID: 2},
		{TaskID: 4, DependsOnTaskID: 3},
	}
}

// TestLinesProjectDiamondWithBackRef is the projection half of what the graph
// screen's TestDAGProjectionDiamondAndSort asserted before the projection
// moved here: the diamond's join node is drawn twice, and the second drawing is
// the back-reference row rather than a repeated subtree.
func TestLinesProjectDiamondWithBackRef(t *testing.T) {
	lines := Lines(diamondDeps(), diamondTasks(), RootOrder{Field: "id", Order: "asc"})
	count, backref := 0, false
	for _, line := range lines {
		if line.TaskID == 4 {
			count++
			backref = backref || strings.Contains(line.Text, "→ #")
		}
	}
	if count != 2 || !backref {
		t.Fatalf("diamond task count/backref = %d/%v", count, backref)
	}
}

// TestLinesEmptyWithoutEdges pins that a graph with no edges projects nothing —
// the branch the screen's empty state depends on.
func TestLinesEmptyWithoutEdges(t *testing.T) {
	if lines := Lines(nil, diamondTasks(), RootOrder{}); lines != nil {
		t.Fatalf("lines = %+v, want nil with no dependencies", lines)
	}
}

// TestLinesOrderRoots pins the two root orderings the view settings can ask
// for, and that the ordering reaches ROOTS only: the subtree under each root
// keeps its ascending-id walk under every order.
func TestLinesOrderRoots(t *testing.T) {
	tasks := []domain.Task{
		{ID: 1, Title: "zulu"}, {ID: 2, Title: "alpha"},
		{ID: 11, Title: "child-of-1"}, {ID: 12, Title: "child-of-1"},
		{ID: 21, Title: "child-of-2"},
	}
	deps := []domain.TaskDependency{
		{TaskID: 12, DependsOnTaskID: 1},
		{TaskID: 11, DependsOnTaskID: 1},
		{TaskID: 21, DependsOnTaskID: 2},
	}
	cases := map[string]struct {
		order RootOrder
		want  []int64
	}{
		"id asc":     {RootOrder{Field: "id", Order: "asc"}, []int64{1, 11, 12, 0, 2, 21}},
		"id desc":    {RootOrder{Field: "id", Order: "desc"}, []int64{2, 21, 0, 1, 11, 12}},
		"title asc":  {RootOrder{Field: "title", Order: "asc"}, []int64{2, 21, 0, 1, 11, 12}},
		"title desc": {RootOrder{Field: "title", Order: "desc"}, []int64{1, 11, 12, 0, 2, 21}},
		"zero value": {RootOrder{}, []int64{1, 11, 12, 0, 2, 21}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			lines := Lines(deps, tasks, tc.order)
			got := make([]int64, len(lines))
			for i, line := range lines {
				got[i] = line.TaskID
			}
			if len(got) != len(tc.want) {
				t.Fatalf("projected %d lines %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("line %d = #%d, want #%d (whole projection %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
