package tui

import (
	"context"
	"omakiten/internal/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

// TestNewModelWithEmptyProjectOpensHome covers AC1/AC14: launching the TUI
// without a resolvable project must land on the multi-project Home view
// instead of erroring.
func TestNewModelWithEmptyProjectOpensHome(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject(alpha) error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Bravo", "bravo", "/work/bravo"); err != nil {
		t.Fatalf("UpsertProject(bravo) error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.top != topHome {
		t.Fatalf("top = %d, want topHome (%d)", model.top, topHome)
	}

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "// PROJECTS · 2") {
		t.Fatalf("home should list 2 projects:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Alpha") || !strings.Contains(rendered, "Bravo") {
		t.Fatalf("home missing project names:\n%s", rendered)
	}
}

// TestHomeHidesTabBar covers AC8/AC15: the per-view tab bar is suppressed
// while on Home so tab/digit navigation never lands on Home and the surface
// reads as chromeless.
func TestHomeHidesTabBar(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	rendered := ansi.Strip(model.View())
	for _, label := range []string{"01 // TASKS", "02 // STATS", "03 // SETTINGS"} {
		if strings.Contains(rendered, label) {
			t.Fatalf("home should hide nav bar but found %q:\n%s", label, rendered)
		}
	}
	if !strings.Contains(rendered, "00 // HOME") {
		t.Fatalf("home header kicker missing:\n%s", rendered)
	}
}

// TestCtrlHReturnsToHome covers AC7: the ctrl+h binding goes back to Home
// from any per-project view.
func TestCtrlHReturnsToHome(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.top != topTasks || model.sub != subBoard {
		t.Fatalf("(top, sub) = (%d, %d), want (topTasks, subBoard)", model.top, model.sub)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	got := updated.(Model)
	if got.top != topHome {
		t.Fatalf("top = %d after ctrl+h, want topHome (%d)", got.top, topHome)
	}
}

// TestHomeEnterSelectsProject covers AC6: pressing enter on a highlighted
// home card switches the model to the chosen project and lands on Board.
func TestHomeEnterSelectsProject(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject(alpha) error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.top != topTasks || got.sub != subBoard {
		t.Fatalf("(top, sub) = (%d, %d) after enter, want (topTasks, subBoard)", got.top, got.sub)
	}
	if got.project.Slug != "alpha" {
		t.Fatalf("project.Slug = %q, want %q", got.project.Slug, "alpha")
	}
	if got.LastProjectRoot() != "/work/alpha" {
		t.Fatalf("LastProjectRoot() = %q, want /work/alpha", got.LastProjectRoot())
	}
}

// TestCtrlHOnHomeReloads ensures ctrl+h while already on Home triggers a
// reload (refresh tags / pending counts) instead of being swallowed by
// the picker — the per-project handleCommonKey path is not reached when
// the model is already on viewHome, so home-side handling is required.
func TestCtrlHOnHomeReloads(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	got := updated.(Model)
	if got.top != topHome {
		t.Fatalf("top = %d after ctrl+h on home, want topHome (%d)", got.top, topHome)
	}
	if got.status != "Refreshed" {
		t.Fatalf("status = %q, want %q", got.status, "Refreshed")
	}
}

// TestHomeRendersProjectTagBadges covers AC4: project_tags become the badges
// on the Home cards, reusing the chip component.
func TestHomeRendersProjectTagBadges(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	tag, err := store.FindOrCreateTag(ctx, "go", "Go")
	if err != nil {
		t.Fatalf("FindOrCreateTag() error = %v", err)
	}
	if err := store.AddProjectTag(ctx, project.ID, tag.ID); err != nil {
		t.Fatalf("AddProjectTag() error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "GO") {
		t.Fatalf("home card should surface project_tags as upper-cased badges:\n%s", rendered)
	}
}
