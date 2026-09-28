package graph

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	depgraph "omakiten/internal/graph"
	screenfixture "omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func graphView() config.GraphViewSettings {
	return config.GraphViewSettings{Sort: config.SortSettings{Field: "id", Order: "asc"}}
}

func graphDeps() Deps {
	return projected(Deps{
		Tasks: []domain.Task{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}, {ID: 3, Title: "C"}, {ID: 4, Title: "D"}},
		Dependencies: []domain.TaskDependency{
			{TaskID: 2, DependsOnTaskID: 1},
			{TaskID: 3, DependsOnTaskID: 1},
			{TaskID: 4, DependsOnTaskID: 2},
			{TaskID: 4, DependsOnTaskID: 3},
		},
	}, graphView())
}

func TestScreenContractNavigationOpenAndRefresh(t *testing.T) {
	frame := screentest.Frame(t, screenfixture.Options{})
	screen := New().Bind(graphDeps(), frame)
	if screen.ID() != screenhost.TasksGraph || len(selectableIndices(screen.lines)) != 5 {
		t.Fatalf("identity/selectable = %q/%d", screen.ID(), len(selectableIndices(screen.lines)))
	}
	screen = screen.Update(frame, screentest.Key("down")).Screen.(Screen)
	if screen.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", screen.Cursor())
	}
	out := screen.Update(frame, tea.KeyMsg{Type: tea.KeyEnter})
	if out.Action.Kind != screenhost.ActionOpenTask || out.Action.TaskID != 2 {
		t.Fatalf("open outcome = %+v", out.Action)
	}
	out = screen.Update(frame, screentest.Key("r"))
	if out.Action.Kind != screenhost.ActionReload {
		t.Fatalf("reload outcome = %+v", out.Action)
	}
}

// TestEveryMotionKeyLandsOnANodeAndNeverOnASeparator is what the deleted
// ordinal cursor used to buy, restated against the one cursor that is left: the
// arranger's. The fixture's three roots are separated by blank lines, so a
// cursor stepping by ITEM would sit on one of them — and every key here is
// routed through the grid, not through a private table on the screen.
func TestEveryMotionKeyLandsOnANodeAndNeverOnASeparator(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Bind(graphGoldenDeps(graphGoldenView()), frame)
	root := screen.bodyRoot(frame.Kit())
	if !root.IsLeaf() || root.Spec.ID != sectionBody {
		t.Fatalf("graph root = leaf %v/id %q, want Cell(%q)", root.IsLeaf(), root.Spec.ID, sectionBody)
	}
	separators := 0
	for _, l := range screen.lines {
		if l.TaskID == 0 {
			separators++
		}
	}
	if separators == 0 {
		t.Fatal("the fixture projects no blank separators; this test would prove nothing about the mask")
	}

	// Walk far enough to cross every separator in the projection, in both
	// directions, with every key the screen advertises.
	keys := []string{"pgdn", "j", "j", "G", "k", "k", "pgup", "g", "j", "pgdn", "pgdn", "pgdn", "k", "G", "pgup", "pgup"}
	for i, key := range keys {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
		assertCursorIsOnANode(t, screen, fmt.Sprintf("key %d (%q)", i, key))
	}
	if screen.Cursor() == 0 {
		t.Fatal("the whole walk ended back on the first node; no key moved the cursor")
	}
}

// assertCursorIsOnANode is the mask's invariant read off the real screen: the
// cursor addresses a projected line, that line carries a node rather than a
// blank root separator, and SelectedTask agrees with it.
func assertCursorIsOnANode(t *testing.T, screen Screen, where string) {
	t.Helper()
	cursor := screen.Cursor()
	if cursor < 0 || cursor >= len(screen.lines) {
		t.Fatalf("%s left the cursor at %d, outside the %d projected lines", where, cursor, len(screen.lines))
	}
	if screen.lines[cursor].TaskID == 0 {
		t.Fatalf("%s parked the cursor on line %d, which is a blank root separator", where, cursor)
	}
	task, ok := screen.SelectedTask()
	if !ok || task.ID != screen.lines[cursor].TaskID {
		t.Fatalf("%s: selected task = %+v/%v against line %d", where, task, ok, cursor)
	}
}

// TestPageAndJumpReachBothEndsOfTheProjection pins that the mask's ends are the
// projection's ends: G reaches the last NODE, not the last line, and g the first.
func TestPageAndJumpReachBothEndsOfTheProjection(t *testing.T) {
	frame := screentest.FrameAt(t, 80, 24)
	screen := New().Bind(graphGoldenDeps(graphGoldenView()), frame)
	mask := selectableIndices(screen.lines)

	bottom := screen.Update(frame, screentest.Key("G")).Screen.(Screen)
	if got := bottom.Cursor(); got != mask[len(mask)-1] {
		t.Fatalf("G landed on line %d, want the last node at line %d of %d lines", got, mask[len(mask)-1], len(screen.lines))
	}
	top := bottom.Update(frame, screentest.Key("g")).Screen.(Screen)
	if got := top.Cursor(); got != mask[0] {
		t.Fatalf("g landed on line %d, want the first node at line %d", got, mask[0])
	}
	// Paging down repeatedly must also arrive at the last node rather than
	// stalling on a separator or short of the tail.
	paged := top
	for i := 0; i < 40; i++ {
		paged = paged.Update(frame, screentest.Key("pgdown")).Screen.(Screen)
	}
	if got := paged.Cursor(); got != mask[len(mask)-1] {
		t.Fatalf("forty page-downs stopped at line %d, want the last node at %d", got, mask[len(mask)-1])
	}
}

// TestSelectableMaskSkipsRootSeparators is the screen's half of what
// TestDAGProjectionDiamondAndSort used to assert; the projection half moved to
// internal/graph with the projection, as TestLinesProjectDiamondWithBackRef.
func TestSelectableMaskSkipsRootSeparators(t *testing.T) {
	if len(selectableIndices([]depgraph.Line{{TaskID: 1}, {}, {TaskID: 2}})) != 2 {
		t.Fatal("selectable projection included separator")
	}
}

func TestBindClampsAfterRefreshAndLifecycleResize(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 18)
	screen := New().Bind(graphDeps(), frame)
	screen = screen.Update(frame, screentest.Key("end")).Screen.(Screen)
	shrunk := graphDeps()
	shrunk.Dependencies = shrunk.Dependencies[:1]
	shrunk = projected(shrunk, graphView())
	screen = screen.Bind(shrunk, frame)
	if screen.Cursor() >= len(screen.lines) {
		t.Fatalf("cursor %d outside %d lines", screen.Cursor(), len(screen.lines))
	}
	if screen.lines[screen.Cursor()].TaskID == 0 {
		t.Fatalf("the refresh left the cursor on line %d, which no longer carries a node", screen.Cursor())
	}
	// 24 rows leave a real PanelChrome viewport; 14 collapsed under the
	// ViewportRows <4 floor and the pre-migration path overdrew by dumping
	// every row when the budget was zero.
	resized := screentest.FrameAt(t, 60, 24)
	screen = screen.Lifecycle(resized, screenhost.LifecycleResize).Screen.(Screen)
	if !strings.Contains(screentest.StripANSI(screen.View(resized)), "DEPENDENCY") {
		t.Fatal("resize lost graph kicker")
	}
}

func TestEmptyFooterHelpNoopAndMissingTask(t *testing.T) {
	frame := screentest.Frame(t, screenfixture.Options{})
	empty := New().Bind(Deps{}, frame)
	if !strings.Contains(empty.View(frame), "No task dependencies") {
		t.Fatalf("empty view = %q", empty.View(frame))
	}
	if len(empty.Footer(frame)) != 4 || len(empty.Help(frame)) != 1 {
		t.Fatal("footer/help declarations missing")
	}
	if out := empty.Update(frame, struct{}{}); out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("non-key action = %v", out.Action.Kind)
	}
	deps := graphDeps()
	deps.Tasks = deps.Tasks[1:]
	screen := New().Bind(deps, frame)
	if _, ok := screen.SelectedTask(); ok {
		t.Fatal("missing projected task resolved as selectable task")
	}
}

func TestPageNavigationSelectAndLongRowsStayBounded(t *testing.T) {
	deps := graphDeps()
	deps.Tasks[0].Title = strings.Repeat("long-title ", 30)
	deps = projected(deps, graphView())
	frame := screentest.FrameAt(t, 60, 24)
	screen := New().Bind(deps, frame)
	screen = screen.Update(frame, screentest.Key("pgdown")).Screen.(Screen)
	if screen.Cursor() == 0 {
		t.Fatal("page down did not move")
	}
	screen = screen.SelectTaskID(3, frame)
	task, ok := screen.SelectedTask()
	if !ok || task.ID != 3 {
		t.Fatalf("selected task = %+v/%v", task, ok)
	}
	// Park back on the long root so the truncated title is inside the window;
	// following the cursor used to be free only because a zero viewport dumped
	// every row.
	screen = screen.SelectTaskID(1, frame)
	if !strings.Contains(screentest.StripANSI(screen.View(frame)), "…") {
		t.Fatal("long row was not truncated")
	}
}

func TestGraphRouteSanitizesLineTextBeforeTruncationAndStyle(t *testing.T) {
	t.Parallel()
	const hostile = "root \x1b[8mhidden\x1b[31mred\x1b[0m\x1b]0;owned\a\u009b8m\u009dtitle\a漢字"
	for _, width := range []int{40, 120} {
		deps := graphDeps()
		deps.Tasks[0].Title = hostile
		deps = projected(deps, graphView())
		frame := screentest.FrameAt(t, width, 24)
		out := New().Bind(deps, frame).View(frame)
		for _, forbidden := range []string{"\x1b[8m", "\x1b]0;", "owned", "\u009b", "\u009d"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("width %d retained hostile graph sequence %q: %q", width, forbidden, out)
			}
		}
		plain := ansi.Strip(out)
		if !strings.Contains(plain, "hiddenred") || !strings.Contains(plain, "漢") {
			t.Fatalf("width %d lost printable graph text: %q", width, plain)
		}
	}
}
