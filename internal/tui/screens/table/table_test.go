package table

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func tableDeps() Deps {
	return Deps{
		Tasks: []domain.Task{
			{ID: 3, Title: "C", BucketKey: "dev", Priority: 3},
			{ID: 1, Title: "A", BucketKey: "backlog", Priority: 1},
			{ID: 2, Title: "B", BucketKey: "dev", Priority: 2},
		},
		Dependencies: []domain.TaskDependency{{TaskID: 2, DependsOnTaskID: 1}},
		Comments:     []domain.Comment{{TaskID: 2}},
		View: config.TableViewSettings{
			Sort: config.SortSettings{Field: "title", Order: "asc"},
		},
		Priorities: []config.PriorityDefinition{{ID: 1, Value: "low"}, {ID: 2, Value: "normal"}, {ID: 3, Value: "high"}},
	}
}

func TestScreenContractNavigationAndOutcomes(t *testing.T) {
	frame := screentest.Frame(t, screentest.Options{})
	screen := New().Bind(tableDeps(), frame)
	if screen.ID() != screenhost.TasksTable || len(screen.Rows()) != 3 {
		t.Fatalf("contract identity/rows = %q/%d", screen.ID(), len(screen.Rows()))
	}
	out := screen.Update(frame, screentest.Key("down"))
	screen = out.Screen.(Screen)
	if screen.Selected() != 1 {
		t.Fatalf("selected = %d, want 1", screen.Selected())
	}
	out = screen.Update(frame, tea.KeyMsg{Type: tea.KeyEnter})
	if out.Action.Kind != screenhost.ActionOpenTask || out.Action.TaskID != 2 {
		t.Fatalf("open outcome = %+v, want task 2", out.Action)
	}
	out = screen.Update(frame, screentest.Key("m"))
	if out.Action.Kind != screenhost.ActionMoveTask || out.Action.TaskID != 2 {
		t.Fatalf("move outcome = %+v, want task 2", out.Action)
	}
	out = screen.Update(frame, screentest.Key("r"))
	if out.Action.Kind != screenhost.ActionReload {
		t.Fatalf("refresh outcome = %+v", out.Action)
	}
}

func TestPrivateSelectedCursorSurvivesGridResync(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 18)
	screen := New().Bind(tableDeps(), frame)
	screen.selected = 2
	screen.grid = screen.grid.WithLayout(screen.grid.Layout().WithCursor(sectionRows, 0))

	screen = screen.syncRowsWindow(frame.Kit())

	if screen.Selected() != 2 {
		t.Fatalf("private selected cursor after Resync = %d, want 2", screen.Selected())
	}
	if got := screen.grid.Layout().Cursor(sectionRows); got != 2 {
		t.Fatalf("grid cursor after Resync = %d, want private cursor 2", got)
	}
}

func TestProjectionFilterSortAndClamp(t *testing.T) {
	frame := screentest.Frame(t, screentest.Options{})
	deps := tableDeps()
	deps.View.Filter.Bucket = []string{"dev"}
	deps.View.Sort = config.SortSettings{Field: "id", Order: "desc"}
	screen := New().Bind(deps, frame)
	if got := []int64{screen.Rows()[0].ID, screen.Rows()[1].ID}; got[0] != 3 || got[1] != 2 {
		t.Fatalf("rows = %v, want [3 2]", got)
	}
	screen = screen.Update(frame, screentest.Key("end")).Screen.(Screen)
	deps.Tasks = deps.Tasks[:1]
	screen = screen.Bind(deps, frame)
	if screen.Selected() != 0 {
		t.Fatalf("selected after shrink = %d, want 0", screen.Selected())
	}
}

func TestPageNavigationResizeAndLifecycle(t *testing.T) {
	deps := tableDeps()
	for i := int64(4); i <= 40; i++ {
		deps.Tasks = append(deps.Tasks, domain.Task{ID: i, Title: "task", BucketKey: "dev", Priority: 2})
	}
	frame := screentest.FrameAt(t, 100, 18)
	screen := New().Bind(deps, frame)
	screen = screen.Update(frame, screentest.Key("pgdown")).Screen.(Screen)
	if screen.Selected() == 0 {
		t.Fatal("page down did not move")
	}
	// 24 rows leave a real PanelChrome viewport; 14 collapsed under the
	// ViewportRows <4 floor and the pre-migration path overdrew by dumping
	// every row when the budget was zero.
	resized := screentest.FrameAt(t, 60, 24)
	screen = screen.Lifecycle(resized, screenhost.LifecycleResize).Screen.(Screen)
	if !strings.Contains(screentest.StripANSI(screen.View(resized)), "TASKS") {
		t.Fatal("resized compact view lost task kicker")
	}
}

func TestEmptyFilteredFooterHelpAndNoop(t *testing.T) {
	frame := screentest.Frame(t, screentest.Options{})
	empty := New().Bind(Deps{}, frame)
	if !strings.Contains(empty.View(frame), "No tasks") {
		t.Fatalf("empty view = %q", empty.View(frame))
	}
	deps := tableDeps()
	deps.View.Filter.Bucket = []string{"review"}
	filtered := New().Bind(deps, frame)
	if !strings.Contains(filtered.View(frame), "No tasks match") {
		t.Fatalf("filtered view = %q", filtered.View(frame))
	}
	if len(filtered.Footer(frame)) != 6 || len(filtered.Help(frame)) != 1 {
		t.Fatal("footer/help declarations missing")
	}
	out := filtered.Update(frame, struct{}{})
	if out.Action.Kind != screenhost.ActionNone {
		t.Fatalf("non-key action = %v", out.Action.Kind)
	}
}

func TestSelectTaskIDAndWideRendering(t *testing.T) {
	frame := screentest.FrameAt(t, 120, 40)
	screen := New().Bind(tableDeps(), frame).SelectTaskID(3, frame)
	task, ok := screen.SelectedTask()
	if !ok || task.ID != 3 {
		t.Fatalf("selected task = %+v/%v", task, ok)
	}
	view := screentest.StripANSI(screen.View(frame))
	assertWideViewHasExpectedColumns(t, view)
	assertIDColumnAligned(t, view)
}

// assertWideViewHasExpectedColumns checks the wide layout renders every
// extra column and keeps the header free of the section kicker marker.
func assertWideViewHasExpectedColumns(t *testing.T, view string) {
	t.Helper()
	for _, want := range []string{"ID", "DEP", "COMMENTS", "C"} {
		if !strings.Contains(view, want) {
			t.Fatalf("wide view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "// ID") {
		t.Fatalf("table column header still uses the section kicker marker:\n%s", view)
	}
	if !strings.Contains(view, "│  ID") {
		t.Fatalf("table column header lost the selection-column indent:\n%s", view)
	}
}

// assertIDColumnAligned checks the ID header sits directly above the ID data
// column, using the row that carries the "backlog" bucket as the data line.
func assertIDColumnAligned(t *testing.T, view string) {
	t.Helper()
	headerAt, dataAt := idColumnPositions(view)
	if headerAt < 0 || dataAt < 0 || headerAt != dataAt {
		t.Fatalf("ID header column %d does not match data column %d:\n%s", headerAt, dataAt, view)
	}
}

func idColumnPositions(view string) (headerAt, dataAt int) {
	headerAt, dataAt = -1, -1
	for _, line := range strings.Split(view, "\n") {
		if headerAt < 0 && strings.Contains(line, "BUCKET") && strings.Contains(line, "ID") {
			headerAt = strings.Index(line, "ID")
		}
		if dataAt < 0 && strings.Contains(line, "backlog") && !strings.Contains(line, "BUCKET") {
			dataAt = strings.IndexAny(line, "0123456789")
		}
	}
	return headerAt, dataAt
}
