package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

// planGoalFixture seeds a single plan carrying a markdown goal_body and
// drives the model to the Tasks › plans list sub-tab, ready for the `f`
// fullscreen-goal binding. Mirrors the network-view fixtures so the two
// plan overlays share their setup shape.
func planGoalFixture(t *testing.T, goalBody string) Model {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()

	if _, err := store.CreatePlan(ctx, project.ID, "rollout", "Rollout", goalBody); err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Comments:     store,
		Dependencies: store,
		Plans:        store,
		Cache:        runtimecache.Install(0, snap),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), snap),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.height = 40
	model.width = 160

	// board → table → graph → plans.
	got := pressStringKey(t, model, "/")
	got = pressStringKey(t, got, "/")
	got = pressStringKey(t, got, "/")
	if got.sub != subPlans {
		t.Fatalf("third '/': sub = %d, want subPlans", got.sub)
	}
	if len(got.plans) != 1 {
		t.Fatalf("len(plans) = %d, want 1", len(got.plans))
	}
	return got
}

// TestPlansSubTabFOpensGoalScreen covers the list → goal overlay
// transition: `f` on the cursored plan loads PlanService.Show (the list
// rollup omits goal_body), flips planGoalScreenOpen, stores the fetched
// PlanShow, and the rendered overlay shows the goal kicker + the
// markdown goal body. esc and a second `f` both close it.
func TestPlansSubTabFOpensGoalScreen(t *testing.T) {
	got := planGoalFixture(t, "## Acceptance\n\nShip the goal overlay.")

	opened := pressRune(t, got, 'f')
	if !opened.planGoalScreenOpen {
		t.Fatalf("after 'f': planGoalScreenOpen = false, want true")
	}
	if opened.planGoalShow.Plan.Slug != "rollout" {
		t.Fatalf("planGoalShow.Plan.Slug = %q, want rollout", opened.planGoalShow.Plan.Slug)
	}
	if !strings.Contains(opened.planGoalShow.Plan.GoalBody, "Ship the goal overlay") {
		t.Fatalf("planGoalShow.Plan.GoalBody missing fetched body: %q", opened.planGoalShow.Plan.GoalBody)
	}

	view := ansi.Strip(opened.View())
	if !strings.Contains(view, "GOAL") {
		t.Fatalf("goal overlay missing GOAL kicker:\n%s", view)
	}
	// The kicker style uppercases the slug (// GOAL · ROLLOUT), so match
	// case-insensitively rather than pinning the casing.
	if !strings.Contains(strings.ToLower(view), "rollout") {
		t.Fatalf("goal overlay missing plan slug header:\n%s", view)
	}
	if !strings.Contains(view, "Ship the goal overlay") {
		t.Fatalf("goal overlay missing markdown body:\n%s", view)
	}

	// esc closes back to the list view.
	closed := pressKey(t, opened, tea.KeyEsc)
	if closed.planGoalScreenOpen {
		t.Fatalf("after esc: planGoalScreenOpen = true, want false")
	}

	// `f` is also a toggle close.
	reopened := pressRune(t, closed, 'f')
	if !reopened.planGoalScreenOpen {
		t.Fatalf("after second 'f': planGoalScreenOpen = false, want true")
	}
	toggled := pressRune(t, reopened, 'f')
	if toggled.planGoalScreenOpen {
		t.Fatalf("after 'f' toggle: planGoalScreenOpen = true, want false")
	}
}

// TestPlanGoalScreenEmptyBodyHint pins the empty-state: a plan with no
// goal_body renders the localized "no goal body" hint instead of a blank
// span, so the overlay never opens to an empty void.
func TestPlanGoalScreenEmptyBodyHint(t *testing.T) {
	got := planGoalFixture(t, "")

	opened := pressRune(t, got, 'f')
	if !opened.planGoalScreenOpen {
		t.Fatalf("after 'f': planGoalScreenOpen = false, want true")
	}
	view := ansi.Strip(opened.View())
	want := opened.t("tui.empty.plan_no_goal")
	if !strings.Contains(view, want) {
		t.Fatalf("empty goal overlay missing hint %q:\n%s", want, view)
	}
}

// TestOpenPlanGoalScreenNoopOnEmptyList guards the no-op path: pressing
// `f` with an empty plan list must not flip the overlay flag (no stray
// fetch / panic on an unpopulated project).
func TestOpenPlanGoalScreenNoopOnEmptyList(t *testing.T) {
	got := planGoalFixture(t, "irrelevant")
	got.plans = nil

	got.openPlanGoalScreen()
	if got.planGoalScreenOpen {
		t.Fatalf("openPlanGoalScreen flipped flag on empty plan list")
	}
}
