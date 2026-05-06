package tui

import (
	"context"
	"fmt"
	"os"
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
	for _, want := range []string{"// LAW · ", "// SLUG", "// SEVERITY", "// BODY", "Stay in scope"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestEntityDeleteRemovesEntity(t *testing.T) {
	model, _, _ := newEntityModel(t)

	got := pressRune(t, model, '4')
	got = pressRune(t, got, 'd')
	if len(got.laws) != 1 {
		t.Fatalf("laws len after first delete key = %d, want 1 before confirmation", len(got.laws))
	}
	if !got.deletePending || !strings.Contains(got.status, "Confirm delete") {
		t.Fatalf("delete confirmation not pending: pending=%v status=%q", got.deletePending, got.status)
	}
	got = pressRune(t, got, 'd')
	if len(got.laws) != 0 {
		t.Fatalf("laws len after delete = %d, want 0", len(got.laws))
	}
}

func TestEntityDeleteCanBeCancelled(t *testing.T) {
	model, _, _ := newEntityModel(t)

	got := pressRune(t, model, '4')
	got = pressRune(t, got, 'd')
	got = pressKey(t, got, tea.KeyEsc)
	if got.deletePending {
		t.Fatalf("deletePending = true, want false after cancel")
	}
	got = pressRune(t, got, 'd')
	if len(got.laws) != 1 {
		t.Fatalf("laws len after cancelled delete and new first key = %d, want 1", len(got.laws))
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

// newEntityModelWithTemplates writes a templates/ folder before constructing
// the model so refresh() picks up template files. The bundle's config slug
// becomes the active template.
func newEntityModelWithTemplates(t *testing.T) Model {
	t.Helper()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakiten.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	bundle := tuiTestBundle()
	if err := config.SaveFullBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveFullBundle() error = %v", err)
	}

	templatesDir := filepath.Join(tmp, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// task-default declares default: task in its own frontmatter (the new
	// binding model). task-bug stays unassigned for picker tests.
	if err := os.WriteFile(filepath.Join(templatesDir, "task-default.md"),
		[]byte("---\nname: Default Task Template\ndescription: Standard scaffold\nentity: task\ndefault: task\n---\n**User Story**\n\nComo X.\n"),
		0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "task-bug.md"),
		[]byte("---\nname: Bug Report\nentity: task\n---\n**Steps**\n\n1.\n"),
		0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
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
	return model
}

func TestRefreshLoadsTemplatesFromBundle(t *testing.T) {
	model := newEntityModelWithTemplates(t)

	if len(model.templates) != 2 {
		t.Fatalf("model.templates len = %d, want 2", len(model.templates))
	}
	var defaultTask *config.TaskTemplate
	for i := range model.templates {
		if model.templates[i].Slug == "task-default" {
			defaultTask = &model.templates[i]
		}
	}
	if defaultTask == nil || defaultTask.Default != "task" {
		t.Fatalf("task-default template should declare default: task, got %+v", defaultTask)
	}
	if path := model.entitySourcePath(entityKindTemplate, "task-default"); path == "" {
		t.Fatalf("entitySourcePath(template, task-default) empty, want a real path")
	}
}

func TestEntityCellRendersTemplatesWithActiveBadge(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	model.entityKind = entityKindTemplate

	cell := model.renderEntityCell(entityKindTemplate)
	for _, want := range []string{"// TEMPLATES · 2", "task-default", "task-bug", "DEFAULT:TASK"} {
		if !strings.Contains(cell, want) {
			t.Fatalf("renderEntityCell missing %q\n%s", want, cell)
		}
	}
}

func TestCustomBadgeAppearsOnUserOverride(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	// Drop a same-slug override into skills/custom/ so the loader merges it
	// with IsCustom=true. Refresh picks it up via the bundle.
	tmp := model.repos.Editor.RootDir()
	customPath := filepath.Join(tmp, "skills", "custom", "go.md")
	if err := os.MkdirAll(filepath.Dir(customPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(customPath, []byte("---\nname: Go (custom)\n---\noverride\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := model.repos.Editor.Apply(model.ctx, nil); err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	cell := model.renderEntityCell(entityKindSkill)
	if !strings.Contains(cell, "CUSTOM") {
		t.Fatalf("renderEntityCell missing CUSTOM badge for overridden go skill\n%s", cell)
	}
}

func TestConfigSlidesHorizontalWindowToKeepFocusedColumnVisible(t *testing.T) {
	model, _, _ := newEntityModel(t)
	// Width that fits exactly 3 columns (3*30 + 2 gaps = 92, plus ~4 padding).
	// allKinds = [Laws, Personas, Skills, Templates, Tags] — 5 total.
	model.width = 100
	model.height = 60

	got := pressRune(t, model, '4')
	view := got.View()
	// Initial focus is Laws (index 0) — visible window starts at 0.
	for _, want := range []string{"// LAWS", "// PERSONAS", "// SKILLS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial config missing %q\n%s", want, view)
		}
	}
	if strings.Contains(view, "// TEMPLATES") {
		t.Fatalf("initial window should not include Templates yet:\n%s", view)
	}

	// Press right twice to move focus to Skills (still in window), then right
	// again to move to Templates — window must slide so Templates is visible.
	got = pressStringKey(t, got, "right")
	got = pressStringKey(t, got, "right")
	got = pressStringKey(t, got, "right")
	view = got.View()
	if !strings.Contains(view, "// TEMPLATES") {
		t.Fatalf("Templates column should be visible after sliding right:\n%s", view)
	}
	if strings.Contains(view, "// LAWS") {
		t.Fatalf("Laws should have scrolled out of view:\n%s", view)
	}
}

func TestEntityCellShowsScrollHintsWhenColumnExceedsViewport(t *testing.T) {
	model, _, _ := newEntityModel(t)
	// Build many synthetic skills so the column is taller than any viewport.
	model.skills = nil
	for i := 0; i < 12; i++ {
		model.skills = append(model.skills, domain.Skill{Key: fmt.Sprintf("skill-%02d", i), Name: fmt.Sprintf("Skill %d", i)})
	}
	model.entityKind = entityKindSkill
	model.width = 200
	model.height = 50 // viewport math measures the runtime header live; need more headroom
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	model.entityCursors[entityKindSkill] = 8
	model.syncFocusedEntityScroll()

	cell := model.renderEntityCell(entityKindSkill)
	if !strings.Contains(cell, "▲") {
		t.Fatalf("expected '▲ N above' hint when cursor is past the top:\n%s", cell)
	}
	if !strings.Contains(cell, "▼") {
		t.Fatalf("expected '▼ N below' hint when more cards exist below:\n%s", cell)
	}
	// Only the cards near the cursor should be present, not all 12.
	if strings.Count(cell, "skill-00") > 0 {
		t.Fatalf("first card should be scrolled out of view:\n%s", cell)
	}
	if !strings.Contains(cell, "skill-08") {
		t.Fatalf("focused card skill-08 missing from column:\n%s", cell)
	}
}

func TestRenderConfigShowsTemplatesColumn(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	// Force a width that fits the 5-column layout; renderConfig branches on
	// availableWidth() so without a sized window the column might fall back
	// to the secondary-row treatment.
	model.width = 200
	model.height = 60

	out := model.renderConfig()
	if !strings.Contains(out, "// TEMPLATES") {
		t.Fatalf("renderConfig missing templates column\n%s", out)
	}
	// Sanity: existing columns still render alongside.
	for _, want := range []string{"// LAWS", "// PERSONAS", "// SKILLS", "// TAGS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderConfig missing %q\n%s", want, out)
		}
	}
}

func TestEntityViewRendersTemplateBody(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	model.entityKind = entityKindTemplate
	// Cursor at 0 points to the alphabetically-first template (task-bug);
	// move to task-default explicitly so the assertion is not order-coupled.
	if model.entityCursors == nil {
		model.entityCursors = map[entityKind]int{}
	}
	for i, tpl := range model.templates {
		if tpl.Slug == "task-default" {
			model.entityCursors[entityKindTemplate] = i
			break
		}
	}
	model.openSelectedEntityView()

	view := model.renderEntityView()
	for _, want := range []string{"// TEMPLATE", "// SLUG", "// NAME", "// BODY", "User Story"} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderEntityView missing %q\n%s", want, view)
		}
	}
}

func TestTemplateCreateAndDeleteAreNoOps(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	// Switch into config view (4) so 'n'/'d' route to handleConfigKey rather than
	// the table view's create-task / delete-task handlers.
	model = pressRune(t, model, '4')
	for model.entityKind != entityKindTemplate {
		model = pressStringKey(t, model, "right")
	}
	beforeLen := len(model.templates)

	got := pressRune(t, model, 'n')
	if len(got.templates) != beforeLen {
		t.Fatalf("templates len after 'n' = %d, want unchanged %d", len(got.templates), beforeLen)
	}
	if !strings.Contains(got.status, "auto-load") {
		t.Fatalf("status after 'n' = %q, want auto-load hint", got.status)
	}

	got = pressRune(t, got, 'd')
	if got.deletePending {
		t.Fatalf("deletePending = true after 'd' on template, want no delete confirmation")
	}
	if !strings.Contains(got.status, "auto-load") {
		t.Fatalf("status after 'd' = %q, want auto-load hint", got.status)
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
