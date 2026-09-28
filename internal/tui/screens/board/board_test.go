package board

import (
	"math/rand"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	screenfixture "omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func boardDeps() Deps {
	return Deps{
		Tasks: []domain.Task{
			{ID: 1, Title: "one", BucketKey: "backlog", Priority: 1},
			{ID: 2, Title: "two", BucketKey: "backlog", Priority: 2},
			{ID: 3, Title: "three", BucketKey: "dev", Priority: 3},
		},
		Workflow:   domain.Workflow{Buckets: []domain.Bucket{{Key: "backlog", Name: "Backlog"}, {Key: "dev", Name: "Development"}, {Key: "done", Name: "Done"}}},
		View:       config.BoardViewSettings{},
		Priorities: []config.PriorityDefinition{{ID: 1, Value: "low"}, {ID: 2, Value: "normal"}, {ID: 3, Value: "high"}},
	}
}

func TestScreenContractAndSemanticOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 120, 30)
	screen := New().Bind(boardDeps(), frame)
	if screen.ID() != screenhost.TasksBoard {
		t.Fatalf("id = %q", screen.ID())
	}

	tests := []struct {
		key  string
		kind screenhost.ActionKind
		id   int64
	}{
		{"enter", screenhost.ActionOpenTask, 1},
		{"n", screenhost.ActionCreateTask, 0},
		{"e", screenhost.ActionEditTask, 1},
	}
	for _, tc := range tests {
		out := screen.Update(frame, screentest.Key(tc.key))
		if out.Action.Kind != tc.kind || out.Action.TaskID != tc.id {
			t.Fatalf("%s outcome = %+v", tc.key, out.Action)
		}
	}

	screen = screen.Update(frame, screentest.Key("m")).Screen.(Screen)
	if !screen.MoveMode() || !screen.BlocksHostInput() || !screen.OwnsFooter() {
		t.Fatal("m did not enter move mode")
	}
	if !screen.OwnsKey(screentest.Key("n")) || screen.OwnsKey(screentest.Key("A")) {
		t.Fatal("screen key ownership does not match board-local bindings")
	}
	out := screen.Update(frame, screentest.Key("right"))
	if out.Action.Kind != screenhost.ActionMoveTaskToBucket || out.Action.TaskID != 1 || out.Action.BucketKey != "dev" {
		t.Fatalf("move outcome = %+v", out.Action)
	}
	if out.Screen.(Screen).MoveMode() {
		t.Fatal("move outcome left move mode active")
	}
}

func TestSelectionFilterAndLifecycle(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 24)
	deps := boardDeps()
	screen := New().Bind(deps, frame).SelectTaskID(3, frame)
	if id, ok := screen.SelectedTaskID(); !ok || id != 3 {
		t.Fatalf("selected task id = %d/%v", id, ok)
	}
	screen = screen.Update(frame, screentest.Key("m")).Screen.(Screen)
	screen = screen.Lifecycle(frame, screenhost.LifecycleBlur).Screen.(Screen)
	if screen.MoveMode() {
		t.Fatal("blur did not cancel move mode")
	}
	screen = screen.Update(frame, screentest.Key("m")).Screen.(Screen).CancelMove()
	if screen.MoveMode() {
		t.Fatal("CancelMove did not clear move mode")
	}

	deps.View.Filter.Priority = []string{"high"}
	filtered := New().Bind(deps, frame).SelectTaskID(3, frame)
	if id, ok := filtered.SelectedTaskID(); !ok || id != 3 {
		t.Fatalf("filtered selection = %d/%v, want task 3", id, ok)
	}
}

func TestNavigationWrapsAndEmptyLanesStaySelectable(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 18)
	screen := New().Bind(boardDeps(), frame)
	screen = screen.Update(frame, screentest.Key("left")).Screen.(Screen)
	if screen.Column() != 2 || screen.Card() != 0 {
		t.Fatalf("left wrap = column/card %d/%d", screen.Column(), screen.Card())
	}
	if !strings.Contains(screen.View(frame), "DONE") {
		t.Fatalf("empty focused lane is not rendered:\n%s", screen.View(frame))
	}
	screen = screen.Update(frame, screentest.Key("right")).Screen.(Screen)
	screen = screen.Update(frame, screentest.Key("down")).Screen.(Screen)
	if screen.Column() != 0 || screen.Card() != 1 {
		t.Fatalf("navigation = column/card %d/%d", screen.Column(), screen.Card())
	}
	if !strings.Contains(screen.View(frame), "BACKLOG") {
		t.Fatalf("wrapped lane is not rendered:\n%s", screen.View(frame))
	}
}

func TestCursorVisibilityPropertyAcrossMoveResizeAndRefresh(t *testing.T) {
	rng := rand.New(rand.NewSource(1981))
	deps := boardDeps()
	for i := int64(4); i <= 80; i++ {
		bucket := deps.Workflow.Buckets[int(i)%len(deps.Workflow.Buckets)].Key
		deps.Tasks = append(deps.Tasks, domain.Task{ID: i, Title: strings.Repeat("task ", int(i%5)+1), BucketKey: bucket, Priority: domain.Priority(i%3 + 1)})
	}
	frame := screentest.FrameAt(t, 72, 16)
	screen := New().Bind(deps, frame)
	keys := []string{"left", "right", "up", "down", "pgup", "pgdown", "g", "G"}
	const steps = 500
	drawn := 0
	for step := 0; step < steps; step++ {
		screen = screen.Update(frame, screentest.Key(keys[rng.Intn(len(keys))])).Screen.(Screen)
		if step%7 == 0 {
			frame = screentest.FrameAt(t, 48+rng.Intn(100), 12+rng.Intn(30))
			screen = screen.Lifecycle(frame, screenhost.LifecycleResize).Screen.(Screen)
		}
		if step%11 == 0 {
			trim := rng.Intn(len(deps.Tasks) + 1)
			refresh := deps
			refresh.Tasks = append([]domain.Task(nil), deps.Tasks[:trim]...)
			screen = screen.Bind(refresh, frame)
		}
		visible, painted := screen.CursorVisible(frame)
		if !painted {
			// The random geometry reached a body too short to afford one lane's
			// declared floor, so the board painted its carousel hint and nothing
			// else. There is no window for the cursor to be outside of; the
			// invariant is not weakened here, it is counted below so this branch
			// cannot become the way the test passes.
			continue
		}
		drawn++
		if !visible {
			t.Fatalf("step %d: cursor not visible at column/card %d/%d", step, screen.Column(), screen.Card())
		}
	}
	// The old assertion ran unconditionally and passed at every geometry,
	// including the ones that painted no lane at all — it measured a viewport of
	// zero, got an empty range and called the cursor visible. Counting the steps
	// that really did paint a lane is what stops the honest version from being
	// the vacuous one.
	if drawn < steps*3/4 {
		t.Fatalf("only %d of %d steps painted a lane; the property is passing by never drawing one", drawn, steps)
	}
}

func TestFooterHelpAndEmptyBoard(t *testing.T) {
	frame := screentest.Frame(t, screenfixture.Options{})
	empty := New().Bind(Deps{}, frame)
	if !strings.Contains(empty.View(frame), "No workflow buckets") {
		t.Fatalf("empty view = %q", empty.View(frame))
	}
	if len(empty.Footer(frame)) != 13 || len(empty.Help(frame)) != 1 {
		t.Fatalf("footer/help = %d/%d", len(empty.Footer(frame)), len(empty.Help(frame)))
	}
	deps := boardDeps()
	deps.Tasks = nil
	if view := New().Bind(deps, frame).View(frame); !strings.Contains(view, "No tasks yet") {
		t.Fatalf("empty workflow guidance missing: %q", view)
	}
}

// TestViewIsAFunctionOfStateAndGeometry replaces the whole-view memo the board
// used to carry.
//
// That memo existed because the board composed every lane itself and had nowhere
// cheaper to put the result; its key was a hand-rolled fingerprint of the lane
// cursor, the card cursor, the column offset, the terminal size and the style
// table, and getting one term of it wrong showed a stale board. The grid removes
// the reason for it — a keystroke reuses the arranger's own measurement, and
// what a screen may not have is a second copy of the state that decides a frame.
//
// So the property that survives is the one the memo was only ever an
// optimisation of: View is a pure function of the state and the geometry. The
// same screen at the same size paints the same bytes; a refresh that adds a card
// and a resize that shortens the terminal each paint different ones.
func TestViewIsAFunctionOfStateAndGeometry(t *testing.T) {
	frame := screentest.FrameAt(t, 120, 30)
	deps := boardDeps()
	screen := New().Bind(deps, frame)

	first := screen.View(frame)
	if second := screen.View(frame); second != first {
		t.Fatal("two views of one state disagreed; the board render is not pure")
	}

	deps.Tasks = append(deps.Tasks, domain.Task{ID: 99, Title: "new", BucketKey: "dev", Priority: 2})
	screen = screen.Bind(deps, frame)
	refreshed := screen.View(frame)
	if refreshed == first {
		t.Fatal("a refresh that added a card repainted the board it had before")
	}
	if !strings.Contains(refreshed, "new") {
		t.Fatalf("the refreshed board does not carry the new card:\n%s", refreshed)
	}

	resized := screen.View(screentest.FrameAt(t, 120, 16))
	if resized == refreshed {
		t.Fatal("a vertical resize repainted the taller board unchanged")
	}
	if lines := strings.Count(resized, "\n"); lines >= strings.Count(refreshed, "\n") {
		t.Fatalf("the shorter terminal painted %d newlines against %d; the body was not recomposed", lines, strings.Count(refreshed, "\n"))
	}
}

// TestBoardMountsEachLaneAsAWindowedGridColumn is the inverse of the assertion
// that stood here through wave seven.
//
// It used to demand that the board's root be ONE cell — a leaf with no children
// — because the lanes were a private carousel the screen joined by hand inside
// it, and h/l were the board's own keys. That is what this migration removed.
// The root is now the carousel itself: a windowed row of columns with one leaf
// per bucket, sliding rather than stacking, and every lane a scroll surface the
// arranger windows.
func TestBoardMountsEachLaneAsAWindowedGridColumn(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Bind(boardGoldenDeps(), frame)
	root := screen.root(frame.Kit())
	if root.IsLeaf() {
		t.Fatal("board root is a leaf again; the lanes are back inside a private carousel")
	}
	if root.Spec.ID != sectionLanes {
		t.Fatalf("board root id = %q, want %q", root.Spec.ID, sectionLanes)
	}
	if !root.IsUnit() {
		t.Fatal("the board root is not a windowed container; a plain Cols would STACK four lanes at eighty columns instead of sliding")
	}
	assertBoardLanesAreScrollableAndUniform(t, root)

	for range 3 {
		screen = screen.Update(frame, screentest.Key("right")).Screen.(Screen)
	}
	if screen.Column() != 3 {
		t.Fatalf("three rights ended on lane %d, want the fourth", screen.Column())
	}
	if window := boardVisibleLanes(screen, frame); window[0] == 0 {
		t.Fatalf("the carousel is still showing lanes [%d,%d) after walking to the last one", window[0], window[1])
	}
	view := screen.View(frame)
	if !strings.Contains(view, "DONE") || strings.Contains(view, "BACKLOG") {
		t.Fatalf("slid carousel did not preserve its lane window:\n%s", view)
	}
}

// assertBoardLanesAreScrollableAndUniform is the per-lane half of the shape:
// one leaf per bucket, each a scroll surface, each pinned to the one width the
// allocator picked.
func assertBoardLanesAreScrollableAndUniform(t *testing.T, root screengrid.Node) {
	t.Helper()
	buckets := boardGoldenBuckets()
	if len(root.Children()) != len(buckets) {
		t.Fatalf("root has %d children, want one lane per bucket (%d)", len(root.Children()), len(buckets))
	}
	for i, child := range root.Children() {
		if !child.IsLeaf() || child.Spec.ID != laneID(buckets[i].Key) {
			t.Fatalf("child %d = leaf %v id %q, want the %q lane", i, child.IsLeaf(), child.Spec.ID, buckets[i].Key)
		}
		if !child.Spec.Scroll.Scrolls() {
			t.Fatalf("lane %q does not scroll; its cards would be clipped rather than windowed", child.Spec.ID)
		}
		if child.Spec.MinWidth != child.Spec.MaxWidth || child.Spec.MinWidth <= 0 {
			t.Fatalf("lane %q declared width [%d,%d]; lanes are uniform and pin both ends", child.Spec.ID, child.Spec.MinWidth, child.Spec.MaxWidth)
		}
	}
}

func boardVisibleLanes(screen Screen, frame screenhost.Frame) [2]int {
	kit := frame.Kit()
	place, ok := screengrid.Render(kit, screen.grid, screen.bodyBox(kit), screen.root(kit)).Placement(sectionLanes)
	if !ok {
		return [2]int{0, 0}
	}
	return [2]int{place.First, place.Last + 1}
}
