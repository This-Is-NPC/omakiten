package tui

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func makeDep(taskID, dependsOnTaskID int64) domain.TaskDependency {
	return domain.TaskDependency{ProjectID: 1, TaskID: taskID, DependsOnTaskID: dependsOnTaskID}
}

func makeTask(id int64, title string) domain.Task {
	return domain.Task{ID: id, Title: title}
}

func TestBuildDAGLinesEmpty(t *testing.T) {
	lines := buildDAGLines(nil, nil)
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines, got %d", len(lines))
	}
}

func TestBuildDAGLinesLinearChain(t *testing.T) {
	// A blocks B, B blocks C  →  root=A, children: A→B→C
	deps := []domain.TaskDependency{makeDep(2, 1), makeDep(3, 2)}
	tasks := []domain.Task{makeTask(1, "alpha"), makeTask(2, "beta"), makeTask(3, "gamma")}
	lines := buildDAGLines(deps, tasks)

	texts := extractTexts(lines)
	out := strings.Join(texts, "\n")

	if !strings.Contains(out, "alpha") {
		t.Errorf("missing root node (alpha):\n%s", out)
	}
	if !strings.Contains(out, "└── ") {
		t.Errorf("missing └── connector:\n%s", out)
	}

	// Verify DAG order: #1 before #2 before #3
	assertOrder(t, out, "#1", "#2", "#3")

	// All three nodes must be selectable
	sel := dagSelectableIndices(lines)
	if len(sel) != 3 {
		t.Errorf("expected 3 selectable nodes, got %d", len(sel))
	}
}

func TestBuildDAGLinesBranchingRoot(t *testing.T) {
	// A blocks both B and C  →  A has two children: B (not last), C (last)
	deps := []domain.TaskDependency{makeDep(2, 1), makeDep(3, 1)}
	tasks := []domain.Task{makeTask(1, "A"), makeTask(2, "B"), makeTask(3, "C")}
	lines := buildDAGLines(deps, tasks)

	texts := extractTexts(lines)
	out := strings.Join(texts, "\n")

	if !strings.Contains(out, "├── ") {
		t.Errorf("expected ├── for non-last child:\n%s", out)
	}
	if !strings.Contains(out, "└── ") {
		t.Errorf("expected └── for last child:\n%s", out)
	}

	sel := dagSelectableIndices(lines)
	if len(sel) != 3 {
		t.Errorf("expected 3 selectable nodes, got %d", len(sel))
	}
}

func TestBuildDAGLinesDiamond(t *testing.T) {
	// A blocks B and C; B and C both block D  →  diamond: D has 2 parents
	deps := []domain.TaskDependency{
		makeDep(2, 1), // B depends on A
		makeDep(3, 1), // C depends on A
		makeDep(4, 2), // D depends on B
		makeDep(4, 3), // D depends on C
	}
	tasks := []domain.Task{makeTask(1, "A"), makeTask(2, "B"), makeTask(3, "C"), makeTask(4, "D")}
	lines := buildDAGLines(deps, tasks)

	// D must appear twice: once fully rendered, once with a back-reference
	count, hasRef := 0, false
	for _, l := range lines {
		if l.taskID == 4 {
			count++
			if strings.Contains(l.text, "→ #") {
				hasRef = true
			}
		}
	}
	if count != 2 {
		t.Errorf("expected D to appear 2 times (full + back-ref), got %d", count)
	}
	if !hasRef {
		t.Error("expected back-reference annotation [→ #...] for D in diamond pattern")
	}
}

func TestBuildDAGLinesMultipleRoots(t *testing.T) {
	// Two independent chains: A→B and C→D
	deps := []domain.TaskDependency{makeDep(2, 1), makeDep(4, 3)}
	tasks := []domain.Task{makeTask(1, "A"), makeTask(2, "B"), makeTask(3, "C"), makeTask(4, "D")}
	lines := buildDAGLines(deps, tasks)

	// Blank separator line must exist between the two trees
	hasBlank := false
	for _, l := range lines {
		if l.taskID == 0 && l.text == "" {
			hasBlank = true
			break
		}
	}
	if !hasBlank {
		t.Error("expected blank separator line between root trees")
	}

	// Both roots (#1 and #3) must have no connector prefix
	rootsFound := 0
	for _, l := range lines {
		if (l.taskID == 1 || l.taskID == 3) && !strings.Contains(l.text, "──") {
			rootsFound++
		}
	}
	if rootsFound != 2 {
		t.Errorf("expected 2 root nodes without tree connector, got %d", rootsFound)
	}

	sel := dagSelectableIndices(lines)
	if len(sel) != 4 {
		t.Errorf("expected 4 selectable nodes, got %d", len(sel))
	}
}

func TestBuildDAGLinesNoDependencies(t *testing.T) {
	lines := buildDAGLines([]domain.TaskDependency{}, nil)
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines for empty deps, got %d", len(lines))
	}
}

func TestDAGSelectableIndicesSkipsBlanks(t *testing.T) {
	lines := []dagLine{
		{taskID: 1, text: "#1   A"},
		{taskID: 0, text: ""},
		{taskID: 2, text: "#2   B"},
	}
	sel := dagSelectableIndices(lines)
	if len(sel) != 2 {
		t.Fatalf("expected 2 selectable indices, got %d", len(sel))
	}
	if sel[0] != 0 || sel[1] != 2 {
		t.Errorf("unexpected selectable indices: %v", sel)
	}
}

func TestBuildDAGLinesContinuationMark(t *testing.T) {
	// A has two children B (not last) and C (last); B has child D.
	// D's line must have │ continuation (B is not the last child of A).
	deps := []domain.TaskDependency{
		makeDep(2, 1), // B depends on A
		makeDep(3, 1), // C depends on A
		makeDep(4, 2), // D depends on B
	}
	tasks := []domain.Task{makeTask(1, "A"), makeTask(2, "B"), makeTask(3, "C"), makeTask(4, "D")}
	lines := buildDAGLines(deps, tasks)

	var dLine string
	for _, l := range lines {
		if l.taskID == 4 {
			dLine = l.text
			break
		}
	}
	if !strings.Contains(dLine, "│") {
		t.Errorf("expected │ continuation in D's line (B has sibling C), got: %q", dLine)
	}
}

// extractTexts returns the text field from each dagLine.
func extractTexts(lines []dagLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

// assertOrder verifies that a, b, c appear in order within s.
func assertOrder(t *testing.T, s, a, b, c string) {
	t.Helper()
	pa := strings.Index(s, a)
	pb := strings.Index(s, b)
	pc := strings.Index(s, c)
	if pa < 0 || pb < 0 || pc < 0 {
		t.Fatalf("one of %q, %q, %q not found in:\n%s", a, b, c, s)
	}
	if pa >= pb || pb >= pc {
		t.Errorf("expected order %q < %q < %q; positions %d, %d, %d in:\n%s", a, b, c, pa, pb, pc, s)
	}
}
