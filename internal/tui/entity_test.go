package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
)

func newEntityModel(t *testing.T) (Model, *sqlite.Store, *app.BundleEditor) {
	t.Helper()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, tuiTestBundle()); err != nil {
		t.Fatalf("SaveFullBundle() error = %v", err)
	}

	ctx := context.Background()
	store, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	editor := app.NewBundleEditor(store, configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store, Comments: store, Dependencies: store, Entries: store, Config: store, Editor: editor,
	}, tuiTestTheme(), token.ApproxCounter{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	return model, store, editor
}

func TestRefreshEnrichesEntitiesWithBundleData(t *testing.T) {
	model, _, _ := newEntityModel(t)

	if len(model.skills) == 0 {
		t.Fatalf("model.skills is empty")
	}
	for _, skill := range model.skills {
		if skill.SourcePath == "" {
			t.Fatalf("skill %q has empty SourcePath after refresh", skill.Key)
		}
	}
	if path := model.entitySourcePath(entityKindSkill, "go"); path == "" {
		t.Fatalf("entitySourcePath(skill, go) = empty, want a real path")
	}
	for _, law := range model.laws {
		if law.SourcePath == "" {
			t.Fatalf("law %q has empty SourcePath after refresh", law.Key)
		}
	}
	for _, persona := range model.personas {
		if persona.SourcePath == "" {
			t.Fatalf("persona %q has empty SourcePath after refresh", persona.Key)
		}
	}
}

func TestEntityViewRendersFrontmatterAndBody(t *testing.T) {
	model, _, _ := newEntityModel(t)

	got := pressRune(t, model, '4')
	if got.view != 3 {
		t.Fatalf("view = %d, want 3", got.view)
	}

	got = pressKey(t, got, tea.KeyEnter)
	if got.entityScreen != entityScreenView {
		t.Fatalf("entityScreen = %v, want view", got.entityScreen)
	}
	view := got.View()
	for _, want := range []string{"Law", "Slug:", "Severity:", "Body", "Stay in scope"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestEntityDeleteRemovesEntity(t *testing.T) {
	model, _, _ := newEntityModel(t)

	got := pressRune(t, model, '4')
	got = pressRune(t, got, 'd')
	if len(got.laws) != 0 {
		t.Fatalf("laws len after delete = %d, want 0", len(got.laws))
	}
}

func TestEntityRefreshAfterEditorMessage(t *testing.T) {
	model, _, editor := newEntityModel(t)
	ctx := context.Background()

	// Simulate the editor flow: directly add a skill and dispatch the
	// editorFinishedMsg the way runExternalEditor would after $EDITOR returns.
	skillService := app.NewSkillService(model.repos.Config, editor)
	if _, err := skillService.Add(ctx, domain.SkillInput{Key: "tui", Name: "TUI"}); err != nil {
		t.Fatalf("SkillService.Add() error = %v", err)
	}

	updated, _ := model.Update(editorFinishedMsg{})
	got := updated.(Model)
	found := false
	for _, skill := range got.skills {
		if skill.Key == "tui" {
			found = true
		}
	}
	if !found {
		t.Fatalf("model did not pick up the new skill: %+v", got.skills)
	}
	if got.status != "Saved" {
		t.Fatalf("status = %q, want Saved", got.status)
	}
}

func TestPersonaPickerToggleAndSave(t *testing.T) {
	model, _, _ := newEntityModel(t)
	ctx := context.Background()

	// Add a second skill so the picker has two rows to toggle between.
	if _, err := app.NewSkillService(model.repos.Config, model.repos.Editor).Add(ctx, domain.SkillInput{Key: "sqlite", Name: "SQLite"}); err != nil {
		t.Fatalf("Add(skill) error = %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	got := pressRune(t, model, '4')
	got = pressStringKey(t, got, "right")
	if got.entityKind != entityKindPersona {
		t.Fatalf("entityKind = %v, want persona", got.entityKind)
	}
	got = pressRune(t, got, 'p')
	if got.entityForm.mode != entityScreenSkillPicker {
		t.Fatalf("picker mode = %v, want skill picker", got.entityForm.mode)
	}

	// The default persona starts with `go` checked. Toggle the focused row off.
	got = pressKey(t, got, tea.KeySpace)
	if got.entityForm.pickerChecks["go"] {
		t.Fatalf("toggle did not uncheck go")
	}
	// Move down and toggle sqlite on.
	got = pressStringKey(t, got, "down")
	got = pressKey(t, got, tea.KeySpace)
	if !got.entityForm.pickerChecks["sqlite"] {
		t.Fatalf("toggle did not check sqlite")
	}

	got = pressKey(t, got, tea.KeyCtrlS)
	if got.status != "Saved" {
		t.Fatalf("status = %q, want Saved", got.status)
	}
	persona, ok := got.findPersonaBySlug("agent")
	if !ok {
		t.Fatalf("persona not found in refreshed model")
	}
	if len(persona.SkillKeys) != 1 || persona.SkillKeys[0] != "sqlite" {
		t.Fatalf("persona.SkillKeys = %v, want [sqlite]", persona.SkillKeys)
	}
}
