package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
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

// deepLongGraphModel builds a Model whose dependency graph has a deep
// chain (forcing wide indentation) and very long task titles, rendered at
// a constrained terminal width. This is the layout that previously pushed
// the graph panel border off-screen (task #593).
func deepLongGraphModel(width int) Model {
	const depth = 8
	deps := make([]domain.TaskDependency, 0, depth)
	tasks := make([]domain.Task, 0, depth+1)
	longTitle := strings.Repeat("very-long-dependency-title-segment ", 6)
	tasks = append(tasks, makeTask(1, "root "+longTitle))
	for i := int64(2); i <= depth+1; i++ {
		tasks = append(tasks, makeTask(i, fmt.Sprintf("node-%d %s", i, longTitle)))
		deps = append(deps, makeDep(i, i-1)) // i depends on i-1: a straight chain
	}
	return Model{
		styles:       newStyles(config.Theme{}),
		width:        width,
		height:       40,
		tasks:        tasks,
		dependencies: deps,
	}
}

// TestRenderGraphRowsStayInsideTerminal pins task #593: every rendered
// graph line must fit inside the terminal width once ANSI styling is
// stripped, even with deep indentation and long titles. Before the fix the
// raw DAG text flowed past the panel border and the right edge fell off
// the screen.
func TestRenderGraphRowsStayInsideTerminal(t *testing.T) {
	for _, width := range []int{60, 80, 100} {
		width := width
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			m := deepLongGraphModel(width)
			view := m.renderGraph()
			for _, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(stripANSI(line)); w > width {
					t.Fatalf("graph line width %d exceeds terminal width %d:\n%q", w, width, line)
				}
			}
			// The fix must actually clip — confirm the long title surfaces an
			// ellipsis so we know truncation engaged rather than the rows
			// simply being short.
			if !strings.Contains(stripANSI(view), "…") {
				t.Fatalf("expected truncated rows to contain an ellipsis at width %d:\n%s", width, stripANSI(view))
			}
			// The leading task id must survive right-truncation so users can
			// still identify the node. Root is rendered first.
			if !strings.Contains(stripANSI(view), "#1") {
				t.Fatalf("expected root task id #1 to remain visible at width %d:\n%s", width, stripANSI(view))
			}
		})
	}
}

// TestGraphNavigationAndOpenSurviveWidthFix confirms that constraining row
// width did not disturb j/k/g/G movement or Enter-to-open (task #593, AC 3).
func TestGraphNavigationAndOpenSurviveWidthFix(t *testing.T) {
	m := deepLongGraphModel(70)
	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)
	if len(sel) < 3 {
		t.Fatalf("setup: expected >=3 selectable nodes, got %d", len(sel))
	}

	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	// j moves the cursor down by one selectable node.
	m.handleGraphKey(runes("j"))
	if got := m.graphCursor.Cursor(); got != 1 {
		t.Fatalf("after j: cursor=%d, want 1", got)
	}
	// k moves back up.
	m.handleGraphKey(runes("k"))
	if got := m.graphCursor.Cursor(); got != 0 {
		t.Fatalf("after k: cursor=%d, want 0", got)
	}
	// G jumps to the last selectable node.
	m.handleGraphKey(runes("G"))
	if got, want := m.graphCursor.Cursor(), len(sel)-1; got != want {
		t.Fatalf("after G: cursor=%d, want %d", got, want)
	}
	// g jumps back to the first.
	m.handleGraphKey(runes("g"))
	if got := m.graphCursor.Cursor(); got != 0 {
		t.Fatalf("after g: cursor=%d, want 0", got)
	}

	// Enter opens the task under the cursor.
	m.handleGraphKey(runes("j")) // move to second node
	wantID := lines[sel[m.graphCursor.Cursor()]].taskID
	m.handleGraphKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.taskScreen != taskScreenView {
		t.Fatalf("after enter: taskScreen=%v, want taskScreenView", m.taskScreen)
	}
	if m.taskID != wantID {
		t.Fatalf("after enter: opened task %d, want %d", m.taskID, wantID)
	}
}

// TestRenderGraphSurvivesStaleCursorAfterDepRemoval pins the crash where
// refresh() reassigned m.dependencies (e.g. after a dependency is removed
// while the graph subnav is open) but the graphCursor still pointed past the
// shrunk selectable-node set. The value-receiver render path indexed
// sel[graphCursor.Cursor()] without re-clamping, panicking with
// index-out-of-range. The fix clamps in clampGraphCursor and defensively
// guards the render-path index.
func TestRenderGraphSurvivesStaleCursorAfterDepRemoval(t *testing.T) {
	m := deepLongGraphModel(80)
	lines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	sel := dagSelectableIndices(lines)
	if len(sel) < 3 {
		t.Fatalf("setup: expected >=3 selectable nodes, got %d", len(sel))
	}

	// Park the cursor on the last selectable node with the FULL item count,
	// then shrink m.dependencies behind its back — exactly what refresh()
	// does when a dependency is removed with the graph subnav open. The
	// cursor's internal itemCount stays stale, so Cursor() now points past
	// the new selectable set.
	m.graphCursor = m.graphCursor.WithItemCount(len(sel)).SetCursor(len(sel) - 1)
	staleCursor := m.graphCursor.Cursor()
	m.dependencies = m.dependencies[:1] // collapse to a single dependency

	newLines := buildDAGLinesSorted(m.dependencies, m.tasks, m.graphRootLess())
	newSel := dagSelectableIndices(newLines)
	if staleCursor < len(newSel) {
		t.Fatalf("setup: stale cursor %d must exceed new selectable count %d", staleCursor, len(newSel))
	}

	// The render path must not panic even before the explicit clamp runs.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("renderGraph panicked with stale cursor: %v", r)
			}
		}()
		_ = m.renderGraph()
	}()

	// clampGraphCursor (wired into refresh) must pull the cursor back inside
	// the new selectable window.
	m.clampGraphCursor()
	if got := m.graphCursor.Cursor(); got >= len(newSel) || got < 0 {
		t.Fatalf("after clampGraphCursor: cursor=%d outside [0,%d)", got, len(newSel))
	}
}

// TestRenderGraphKickerStaysInsideTerminal pins FINDING 2: the kicker/header
// row must stay within the terminal width on a narrow terminal even when the
// translated dependency_graph label is long, mirroring the bound the DAG rows
// already enforce.
func TestRenderGraphKickerStaysInsideTerminal(t *testing.T) {
	for _, width := range []int{28, 36, 44} {
		width := width
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			m := deepLongGraphModel(width)
			view := m.renderGraph()
			// The kicker is the first non-blank content row in the panel body.
			for _, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(stripANSI(line)); w > width {
					t.Fatalf("graph line width %d exceeds terminal width %d:\n%q", w, width, stripANSI(line))
				}
			}
		})
	}
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
