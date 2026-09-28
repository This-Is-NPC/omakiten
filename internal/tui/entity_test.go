package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/configstore"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/bundleeditor"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/entitylist"
)

func newEntityModel(t *testing.T) (Model, *snapstore.Store, contract.BundleEditor) {
	t.Helper()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, tuiTestBundle(t)); err != nil {
		t.Fatalf("SaveFullBundle() error = %v", err)
	}

	ctx := context.Background()
	store := snapstore.Open(t, dbPath)

	files := configstore.New()
	editor := bundleeditor.New(files, configPath)
	resolved, err := bundledraft.ApplyPlanned(ctx, editor, nil)
	if err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	if err := store.ImportBundle(ctx, resolved, configPath, ""); err != nil {
		t.Fatalf("store.ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	cache := runtimecache.InstallWithStore(0, store)
	if rt := cache.Get(0); rt != nil {
		runtimecache.SetEntityRepos(rt.Service, editor, files, files)
		rt.Editor = editor
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: cache, Comments: store, Dependencies: store, Editor: editor,
		BundleStore: files, Catalog: newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	if got.navigationTop() != screenhost.TopSettings || got.navigation != screenhost.SettingsGeneral {
		t.Fatalf("(top, sub) = (%s, %s), want (screenhost.TopSettings, screenhost.SettingsGeneral)", got.navigationTop(), got.navigation)
	}
	// Cycle to Settings › Laws so the entity-detail flow under test still
	// has a list to operate on (general is read-only).
	got = pressStringKey(t, got, "/")
	if got.navigation != screenhost.SettingsLaws {
		t.Fatalf("after '/': sub = %s, want screenhost.SettingsLaws", got.navigation)
	}

	got = pressKey(t, got, tea.KeyEnter)
	if len(got.screenStack) != 1 || got.screenStack[0] != screenhost.EntityDetail {
		t.Fatalf("entity detail route stack = %v", got.screenStack)
	}
	view := got.View()
	plain := stripANSI(view)
	for _, want := range []string{"LAW · ", "SLUG", "SEVERITY", "BODY", "Stay in scope"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("View() missing %q\n%s", want, view)
		}
	}
}

func TestEntityDeleteRemovesEntity(t *testing.T) {
	model, _, _ := newEntityModel(t)

	got := pressRune(t, model, '4')
	got = pressStringKey(t, got, "/") // Settings › General → Settings › Laws
	got = pressRune(t, got, 'd')
	if len(got.laws) != 1 {
		t.Fatalf("laws len after first delete key = %d, want 1 before confirmation", len(got.laws))
	}
	list := got.entityListScreens[screenhost.SettingsLaws]
	if got.deletePending || len(list.Footer(got.screenFrame())) != 2 || !strings.Contains(got.status, "Confirm delete") {
		t.Fatalf("screen delete confirmation not pending: root=%v footer=%v status=%q", got.deletePending, list.Footer(got.screenFrame()), got.status)
	}
	got = pressRune(t, got, 'd')
	if len(got.laws) != 0 {
		t.Fatalf("laws len after delete = %d, want 0", len(got.laws))
	}
}

func TestEntityDeleteCanBeCancelled(t *testing.T) {
	model, _, _ := newEntityModel(t)

	got := pressRune(t, model, '4')
	got = pressStringKey(t, got, "/")
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
	model, _, _ := newEntityModel(t)
	ctx := context.Background()

	// Simulate the editor flow: directly add a skill and dispatch the
	// editorFinishedMsg the way runExternalEditor would after $EDITOR returns.
	skillServiceAdd := model.ops().AddSkill
	if _, err := skillServiceAdd(ctx, domain.SkillInput{Key: "tui", Name: "TUI"}); err != nil {
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
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	bundle := tuiTestBundle(t)
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
	store := snapstore.Open(t, dbPath)

	files := configstore.New()
	editor := bundleeditor.New(files, configPath)
	resolved, err := bundledraft.ApplyPlanned(ctx, editor, nil)
	if err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	if err := store.ImportBundle(ctx, resolved, configPath, ""); err != nil {
		t.Fatalf("store.ImportBundle: %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	cache := runtimecache.InstallWithStore(0, store)
	if rt := cache.Get(0); rt != nil {
		runtimecache.SetEntityRepos(rt.Service, editor, files, files)
		rt.Editor = editor
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: cache, Comments: store, Dependencies: store, Editor: editor,
		BundleStore: files, Catalog: newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
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
	cell := model.boundEntityListScreen(entitylist.New(entitylist.Templates())).View(model.screenFrame())
	for _, want := range []string{"TEMPLATES · 2", "task-default", "task-bug", "DEFAULT:TASK"} {
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
	if err := os.WriteFile(customPath, []byte("---\nname: Go (custom)\nschema_version: 2\n---\noverride\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := bundledraft.ApplyPlanned(model.ctx, model.repos.Editor, nil); err != nil {
		t.Fatalf("editor.Apply() error = %v", err)
	}
	if err := runtimecache.RefreshFromEditor(model.repos.Cache, model.repos.ProjectID, model.repos.Editor); err != nil {
		t.Fatalf("runtimecache.RefreshFromEditor: %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	cell := model.boundEntityListScreen(entitylist.New(entitylist.Skills())).View(model.screenFrame())
	if !strings.Contains(cell, "CUSTOM") {
		t.Fatalf("renderEntityCell missing CUSTOM badge for overridden go skill\n%s", cell)
	}
}

// T1's horizontal Settings/Config grid was retired in T2. Each entity
// kind now lives on its own Settings sub, so cycling between them no
// longer involves a sliding window — `,` / `/` swaps the active sub
// directly. The dedicated cycle test lives in model_test.go's
// `TestSubCycleBindings`; the per-sub render is covered by
// `TestSettingsSubsRenderIsolatedColumns`.

func TestSettingsGeneralRendersRuntimeCard(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.repos.Version = "0.9.0-test"
	model.repos.ConfigPath = "/tmp/omakiten.yaml"
	model.repos.DBPath = "/tmp/omakiten.db"
	model.width = 200
	model.height = 40
	model.navigation = screenhost.SettingsGeneral

	view := ansi.Strip(model.View())
	for _, want := range []string{"RUNTIME", "PROJECT", "OKT VERSION", "0.9.0-test", "SCOPE", "global", "/tmp/omakiten.yaml", "/tmp/omakiten.db", "THEME", "WORKFLOW"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Settings › General missing %q\n%s", want, view)
		}
	}
}

func TestSettingsGeneralScopeBadgeNamesRepoLocalDir(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.repos.Version = "0.9.0-test"
	model.repos.ConfigPath = "/tmp/myrepo/.omakiten/config/izakaya.yaml"
	model.repos.DBPath = "/tmp/omakiten.db"
	model.repos.RepoLocalDir = "/tmp/myrepo/.omakiten"
	model.width = 200
	model.height = 40
	model.navigation = screenhost.SettingsGeneral

	view := ansi.Strip(model.View())
	for _, want := range []string{"SCOPE", "local (/tmp/myrepo/.omakiten)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Settings › General missing %q\n%s", want, view)
		}
	}
}

func TestSettingsTemplatesSubRendersColumn(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	model.width = 200
	model.height = 60
	// Each entity kind owns its own Settings sub. Driving to Settings ›
	// Templates should land on a single templates column.
	model.navigation = screenhost.SettingsTemplates
	out := model.renderCurrentView()
	if !strings.Contains(out, "TEMPLATES") {
		t.Fatalf("Settings › Templates missing column header\n%s", out)
	}
	for _, leaked := range []string{"LAWS", "PERSONAS", "SKILLS", "TAGS"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("Settings › Templates leaked sibling kind %q (T2 split should isolate columns):\n%s", leaked, out)
		}
	}
}

func TestEntityViewRendersTemplateBody(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	model.openEntityDetail(entityKindTemplate, "task-default")
	view := model.renderCurrentView()
	for _, want := range []string{"TEMPLATE", "SLUG", "NAME", "BODY", "User Story"} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderEntityView missing %q\n%s", want, view)
		}
	}
}

func TestTemplateCreateAndDeleteAreNoOps(t *testing.T) {
	model := newEntityModelWithTemplates(t)
	// Drive into Settings › Templates so 'n'/'d' route to handleConfigKey
	// rather than the table view's create-task / delete-task handlers.
	model = pressRune(t, model, '4')
	for i := 0; model.navigation != screenhost.SettingsTemplates; i++ {
		if i >= len(subsByTop[screenhost.TopSettings]) {
			t.Fatalf("cycled %d times without reaching screenhost.SettingsTemplates (stuck on top=%s sub=%s)", i, model.navigationTop(), model.navigation)
		}
		model = pressStringKey(t, model, "/")
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
	if _, err := model.ops().AddSkill(ctx, domain.SkillInput{Key: "sqlite", Name: "SQLite"}); err != nil {
		t.Fatalf("Add(skill) error = %v", err)
	}
	if err := runtimecache.RefreshFromEditor(model.repos.Cache, model.repos.ProjectID, model.repos.Editor); err != nil {
		t.Fatalf("runtimecache.RefreshFromEditor: %v", err)
	}
	if err := model.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}

	got := pressRune(t, model, '4')
	for i := 0; got.navigation != screenhost.SettingsPersonas; i++ {
		if i >= len(subsByTop[screenhost.TopSettings]) {
			t.Fatalf("cycled %d times without reaching screenhost.SettingsPersonas (stuck on top=%s sub=%s)", i, got.navigationTop(), got.navigation)
		}
		got = pressStringKey(t, got, "/")
	}
	got = pressRune(t, got, 'p')
	if len(got.screenStack) == 0 || got.screenStack[len(got.screenStack)-1] != screenhost.PersonaSkills {
		t.Fatalf("picker route = %v, want persona skills", got.screenStack)
	}

	// The default persona starts with `go` checked. Toggle the focused row off.
	got = pressKey(t, got, tea.KeySpace)
	if selected := got.personaSkillsScreen.SelectedValues(); len(selected) != 0 {
		t.Fatalf("toggle did not uncheck go")
	}
	// Move down and toggle sqlite on.
	got = pressStringKey(t, got, "down")
	got = pressKey(t, got, tea.KeySpace)
	if selected := got.personaSkillsScreen.SelectedValues(); len(selected) != 1 || selected[0] != "sqlite" {
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
