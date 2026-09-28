package plans

import (
	"errors"
	"strings"
	"testing"

	"omakiten/internal/domain"
	screenfixture "omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/screentest"
)

func planRollups() []domain.PlanRollup {
	return []domain.PlanRollup{
		{Plan: domain.Plan{ID: 1, Slug: "first", Name: "First", Status: domain.PlanStatusActive}, DoneCount: 1, TotalCount: 4, ActiveWaveName: "Build"},
		{Plan: domain.Plan{ID: 2, Slug: "second", Name: "Second", Status: domain.PlanStatusDone}, DoneCount: 3, TotalCount: 3},
	}
}

func TestListLifecycleNavigationAndOutcomes(t *testing.T) {
	frame := screentest.FrameAt(t, 100, 18)
	screen := New().Apply(planRollups(), nil).Lifecycle(frame, screenhost.LifecycleEnter).Screen.(Screen)
	assertPlansInitialState(t, screen)
	assertPlansRenderedColumns(t, screentest.StripANSI(screen.View(frame)))
	if len(screen.Rollups()) != 2 {
		t.Fatalf("rollups = %d, want 2", len(screen.Rollups()))
	}
	screen = assertPlansNavigationOutcomes(t, frame, screen)
	assertPlansResizeKeepsSelectionVisible(t, screen)
}

// assertPlansInitialState checks the list opens on the first row and claims
// only the keys plans owns, leaving shared navigation like tab alone.
func assertPlansInitialState(t *testing.T, screen Screen) {
	t.Helper()
	if screen.ID() != screenhost.TasksPlans || screen.Cursor() != 0 {
		t.Fatalf("initial id/cursor = %q/%d", screen.ID(), screen.Cursor())
	}
	if !screen.OwnsKey(screentest.Key("f")) || screen.OwnsKey(screentest.Key("tab")) {
		t.Fatal("plans did not claim f while leaving tab to shared navigation")
	}
}

// assertPlansRenderedColumns checks row ordering, progress/active-wave text
// and the SLUG header's shape survive the render.
func assertPlansRenderedColumns(t *testing.T, view string) {
	t.Helper()
	if strings.Index(view, "first") > strings.Index(view, "second") || !strings.Contains(view, "1/4") || !strings.Contains(view, "25%") || !strings.Contains(view, "Build") {
		t.Fatalf("list ordering/progress/active wave changed:\n%s", view)
	}
	if !strings.Contains(view, "SLUG") {
		t.Fatalf("plans column header missing SLUG:\n%s", view)
	}
	if strings.Contains(view, "// SLUG") {
		t.Fatalf("plans column header still uses the section kicker marker:\n%s", view)
	}
	if !strings.Contains(view, "│  SLUG") {
		t.Fatalf("plans column header lost the selection-column indent:\n%s", view)
	}
	if !strings.Contains(view, "│  second") {
		t.Fatalf("plans data rows lost the selection-column indent:\n%s", view)
	}
}

// assertPlansNavigationOutcomes moves the cursor and checks the goal,
// network and reload outcomes it unlocks, returning the moved screen.
func assertPlansNavigationOutcomes(t *testing.T, frame screenhost.Frame, screen Screen) Screen {
	t.Helper()
	screen = screen.Update(frame, screentest.Key("j")).Screen.(Screen)
	if screen.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", screen.Cursor())
	}
	goal := screen.Update(frame, screentest.Key("f"))
	if goal.Action.Kind != screenhost.ActionOpenPlanGoal || goal.Action.PlanSlug != "second" {
		t.Fatalf("goal outcome = %+v", goal.Action)
	}
	network := screen.Update(frame, screentest.Key("enter"))
	if network.Action.Kind != screenhost.ActionOpenPlanNetwork || network.Action.PlanSlug != "second" {
		t.Fatalf("network outcome = %+v", network.Action)
	}
	if screen.Update(frame, screentest.Key("r")).Action.Kind != screenhost.ActionReload {
		t.Fatal("r did not request reload")
	}
	return screen
}

// assertPlansResizeKeepsSelectionVisible checks a resize to a short frame
// keeps the selected row inside the visible scroll window.
func assertPlansResizeKeepsSelectionVisible(t *testing.T, screen Screen) {
	t.Helper()
	resized := screentest.FrameAt(t, 60, 8)
	screen = screen.Lifecycle(resized, screenhost.LifecycleResize).Screen.(Screen)
	if screen.Cursor() != 1 || screen.Scroll() > screen.Cursor() {
		t.Fatalf("resize lost visible selection: cursor/scroll = %d/%d", screen.Cursor(), screen.Scroll())
	}
}

func TestListNavigationVocabularyAndBounds(t *testing.T) {
	// Tall enough that PanelChrome still has a viewport: at ~14 rows the panel
	// border consumes the HostBox and ArrangeIn correctly paints nothing
	// (zero overdraw). Pre-migration this frame overdrew; post-migration the
	// truncation assertions need a real item window.
	frame := screentest.FrameAt(t, 80, 24)
	rollups := make([]domain.PlanRollup, 20)
	for i := range rollups {
		rollups[i] = domain.PlanRollup{Plan: domain.Plan{Slug: strings.Repeat("long-slug", 4), Name: strings.Repeat("long name ", 8), Status: domain.PlanStatusActive}, DoneCount: i, TotalCount: 0}
	}
	screen := New().Apply(rollups, nil)
	for _, key := range []string{"down", "pgdown", "ctrl+d", "end", "up", "pgup", "ctrl+u", "home", "G", "g"} {
		screen = screen.Update(frame, screentest.Key(key)).Screen.(Screen)
	}
	if screen.Cursor() != 0 {
		t.Fatalf("navigation ended at cursor %d, want 0", screen.Cursor())
	}
	if view := screentest.StripANSI(screen.View(frame)); !strings.Contains(view, "…") || !strings.Contains(view, "0%") {
		t.Fatalf("long/zero-total render = %q", view)
	}
	empty := New().Apply(nil, nil)
	if empty.Update(frame, screentest.Key("enter")).Action.Kind != screenhost.ActionNone || empty.Update(frame, screentest.Key("f")).Action.Kind != screenhost.ActionNone {
		t.Fatal("empty list emitted an open action")
	}
	if empty.Update(frame, struct{}{}).Action.Kind != screenhost.ActionNone {
		t.Fatal("non-key message emitted an action")
	}
}

func TestListLoadingEmptyErrorFooterAndHelp(t *testing.T) {
	frame := screentest.Frame(t, screenfixture.Options{})
	if view := New().Loading().View(frame); !strings.Contains(view, "Loading") {
		t.Fatalf("loading view = %q", view)
	}
	if view := New().Apply(nil, nil).View(frame); !strings.Contains(view, "No plans yet") {
		t.Fatalf("empty view = %q", view)
	}
	if view := New().Apply(nil, errors.New("plans unavailable")).View(frame); !strings.Contains(view, "plans unavailable") {
		t.Fatalf("error view = %q", view)
	}
	if len(New().Footer(frame)) == 0 || len(New().Help(frame)) == 0 {
		t.Fatal("list footer/help is empty")
	}
}

func TestGoalOwnsScrollToggleAndBackLifecycle(t *testing.T) {
	frame := screentest.FrameAt(t, 90, 24)
	show := domain.PlanShow{Plan: domain.Plan{Slug: "rollout", Name: "Rollout", Status: domain.PlanStatusActive, GoalBody: strings.Repeat("goal line\n", 30)}}
	goal := NewGoal().Apply(show, nil)
	goal = goal.Lifecycle(frame, screenhost.LifecycleEnter).Screen.(GoalScreen)
	if goal.ID() != screenhost.PlanGoal || !strings.Contains(screentest.StripANSI(goal.View(frame)), "goal line") {
		t.Fatalf("goal id/view mismatch: %q", goal.ID())
	}
	if !goal.OwnsKey(screentest.Key("j")) || !goal.BlocksHostInput() {
		t.Fatal("goal reader did not claim host input")
	}
	toggleOutcome := goal.Update(frame, screentest.Key("M"))
	if toggleOutcome.Action.Kind != screenhost.ActionSetStatus || toggleOutcome.Action.Status == "" {
		t.Fatalf("markdown toggle status = %+v", toggleOutcome.Action)
	}
	toggled := toggleOutcome.Screen.(GoalScreen)
	if toggled.MarkdownRendered() {
		t.Fatal("M did not switch to plain markdown")
	}
	scrolled := toggled.Update(frame, screentest.Key("j")).Screen.(GoalScreen)
	if scrolled.Scroll() == 0 {
		t.Fatal("j did not scroll reader")
	}
	if scrolled.Update(frame, screentest.Key("esc")).Action.Kind != screenhost.ActionBack {
		t.Fatal("esc did not pop reader")
	}
	if len(goal.Footer(frame)) == 0 || len(goal.Help(frame)) == 0 {
		t.Fatal("goal footer/help is empty")
	}
	if goal.Update(frame, struct{}{}).Action.Kind != screenhost.ActionNone {
		t.Fatal("non-key goal update emitted an action")
	}
	resized := scrolled.Lifecycle(screentest.FrameAt(t, 60, 16), screenhost.LifecycleResize).Screen.(GoalScreen)
	if resized.Scroll() != scrolled.Scroll() {
		t.Fatalf("resize changed reader scroll = %d, want %d", resized.Scroll(), scrolled.Scroll())
	}
}

func TestGoalLoadingEmptyAndError(t *testing.T) {
	frame := screentest.Frame(t, screenfixture.Options{})
	if view := NewGoal().Loading().View(frame); !strings.Contains(view, "Loading") {
		t.Fatalf("loading view = %q", view)
	}
	if view := NewGoal().Apply(domain.PlanShow{Plan: domain.Plan{Slug: "empty"}}, nil).View(frame); !strings.Contains(view, "No goal body") {
		t.Fatalf("empty goal view = %q", view)
	}
	if view := NewGoal().Apply(domain.PlanShow{}, errors.New("goal unavailable")).View(frame); !strings.Contains(view, "goal unavailable") {
		t.Fatalf("error goal view = %q", view)
	}
}
