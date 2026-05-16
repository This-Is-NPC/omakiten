package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)


type entitiesFixture struct {
	store      *snapstore.Store
	editor     *app.BundleEditor
	files      *configstore.Adapter
	configPath string
	configDir  string
}

func newEntitiesFixture(t *testing.T) entitiesFixture {
	t.Helper()
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config")
	configPath := filepath.Join(configDir, "omakiten.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, fixtureBundle(t)); err != nil {
		t.Fatalf("SaveFullBundle() error = %v", err)
	}

	ctx := context.Background()
	store := snapstore.Open(t, dbPath)

	files := configstore.New()
	editor := app.NewBundleEditor(files, configPath)
	resolved, err := editor.Apply(ctx, nil)
	if err != nil {
		t.Fatalf("editor.Apply(seed) error = %v", err)
	}
	// Phase 2-bis: BundleEditor no longer writes through to the Store;
	// rotate the SnapStore's snapshot manually so entity services that
	// read through `store.Snapshot()` (test-only) see the seeded
	// skills / personas / laws.
	if err := store.ImportBundle(ctx, resolved, configPath, ""); err != nil {
		t.Fatalf("store.ImportBundle: %v", err)
	}

	return entitiesFixture{store: store, editor: editor, files: files, configPath: configPath, configDir: configDir}
}

// fixtureBundle loads the entities-flavored test bundle. testdata/entities.yaml
// supplies the workflow + kit shape; the inline skills/personas/laws cover
// the entity arrays that production loads from per-entity folders (and that
// config.Bundle marks `yaml:"-"`).
func fixtureBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "entities.yaml")
	bundle.Skills = []config.Skill{
		{Slug: "go", Name: "Go", Description: "Go language."},
		{Slug: "sqlite", Name: "SQLite", Description: "SQLite stack."},
	}
	bundle.Personas = []config.Persona{
		{Slug: "backend-agent", Name: "Backend Agent", Description: "Backend persona", Skills: []string{"go", "sqlite"}},
	}
	bundle.Laws = []config.Law{
		{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"},
	}
	return bundle
}

func TestLawServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		input   domain.LawInput
		wantErr domain.ErrorCode
	}{
		// Severity ids: 1=info, 2=warning, 3=error (canonical kit). 99 is
		// unregistered, exercising the validator's "id not in
		// config.severities" branch.
		{name: "creates law", input: domain.LawInput{Key: "warn-only", Severity: domain.Severity(2), Body: "Be careful."}},
		{name: "rejects empty key", input: domain.LawInput{Key: " ", Severity: domain.Severity(1), Body: "anything"}, wantErr: domain.ErrValidation},
		{name: "rejects empty body", input: domain.LawInput{Key: "x", Severity: domain.Severity(1), Body: " "}, wantErr: domain.ErrValidation},
		{name: "rejects invalid severity", input: domain.LawInput{Key: "x", Severity: domain.Severity(99), Body: "anything"}, wantErr: domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newEntitiesFixture(t)
service := app.NewLawService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files, testfixtures.CanonicalRegistry())

			law, err := service.Add(ctx, tt.input)
			if tt.wantErr != "" {
				assertCoded(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			if law.Key != tt.input.Key {
				t.Fatalf("Add().Key = %q, want %q", law.Key, tt.input.Key)
			}

			updatedBody := "Edited body."
			updated, err := service.Edit(ctx, law.Key, domain.LawUpdate{Body: &updatedBody})
			if err != nil {
				t.Fatalf("Edit() error = %v", err)
			}
			if updated.Body != updatedBody {
				t.Fatalf("Edit() = %#v, want body=%q", updated, updatedBody)
			}

			if err := service.Remove(ctx, updated.Key); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
			laws, err := service.List(ctx)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			for _, remaining := range laws {
				if remaining.Key == updated.Key {
					t.Fatalf("Remove did not delete law %#v", remaining)
				}
			}
		})
	}
}

func TestLawServiceRejectsDuplicateKey(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)
	service := app.NewLawService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files, testfixtures.CanonicalRegistry())

	if _, err := service.Add(ctx, domain.LawInput{Key: "scope", Severity: domain.Severity(3), Body: "anything"}); err == nil {
		t.Fatalf("Add(duplicate) error = nil, want validation error")
	}
}

func TestSkillServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		input   domain.SkillInput
		wantErr domain.ErrorCode
	}{
		{name: "creates skill", input: domain.SkillInput{Key: "tui", Name: "TUI"}},
		{name: "rejects empty name", input: domain.SkillInput{Key: "x", Name: " "}, wantErr: domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newEntitiesFixture(t)
			service := app.NewSkillService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)
			skill, err := service.Add(ctx, tt.input)
			if tt.wantErr != "" {
				assertCoded(t, err, tt.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}

			rename := skill.Name + " 2"
			updated, err := service.Edit(ctx, skill.Key, domain.SkillUpdate{Name: &rename})
			if err != nil {
				t.Fatalf("Edit() error = %v", err)
			}
			if updated.Name != rename {
				t.Fatalf("Edit().Name = %q, want %q", updated.Name, rename)
			}

			if err := service.Remove(ctx, updated.Key); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		})
	}
}

func TestSkillServiceRemovePrunesPersonaReferences(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)
	service := app.NewSkillService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)

	if err := service.Remove(ctx, "go"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	bundle, err := fixture.editor.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, persona := range bundle.Personas {
		for _, skill := range persona.Skills {
			if skill == "go" {
				t.Fatalf("persona %s still references removed skill go", persona.Slug)
			}
		}
	}
}

func TestPersonaServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)
	service := app.NewPersonaService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)
	skillService := app.NewSkillService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)
	skills, err := skillService.List(ctx)
	if err != nil {
		t.Fatalf("List(skills) error = %v", err)
	}
	if len(skills) < 2 {
		t.Fatalf("List(skills) len = %d, want >= 2", len(skills))
	}

	added, err := service.Add(ctx, domain.PersonaInput{Key: "frontend-agent", Name: "Frontend Agent", SkillIDs: []int64{skills[0].ID}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if len(added.SkillKeys) != 1 || added.SkillKeys[0] != skills[0].Key {
		t.Fatalf("Add().SkillKeys = %v, want %q", added.SkillKeys, skills[0].Key)
	}

	rename := "Frontend Agent v2"
	newSkillKeys := []string{skills[1].Key}
	updated, err := service.Edit(ctx, added.Key, domain.PersonaUpdate{Name: &rename, SkillKeys: &newSkillKeys})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if updated.Name != rename || len(updated.SkillKeys) != 1 || updated.SkillKeys[0] != skills[1].Key {
		t.Fatalf("Edit() = %#v, want skills [%s]", updated, skills[1].Key)
	}

	missing := []string{"missing"}
	if _, err := service.Edit(ctx, added.Key, domain.PersonaUpdate{SkillKeys: &missing}); err == nil {
		t.Fatalf("Edit(missing skill) error = nil, want skill_not_found")
	}

	if err := service.Remove(ctx, added.Key); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestBundleEditorRoundTripsValidation(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)

	skillService := app.NewSkillService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)
	if _, err := skillService.Add(ctx, domain.SkillInput{Key: "tui", Name: "TUI"}); err != nil {
		t.Fatalf("SkillService.Add() error = %v", err)
	}

	bundle, err := fixture.editor.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(bundle.Skills) != 3 {
		t.Fatalf("Skills len = %d, want 3", len(bundle.Skills))
	}
}

func TestBundleEditorApplyRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)

	skillPath := config.EntityFilePath(filepath.Dir(fixture.configDir), config.EntityKindSkill, "go")
	originalSkill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile(go.md) error = %v", err)
	}
	originalWiring, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("ReadFile(omakiten.yaml) error = %v", err)
	}

	// Inject an invalid wiring mutation that will fail validation. The file op
	// rewrites go.md; rollback must restore both files.
	updatedBytes := []byte("---\nname: Modified\n---\nbody\n")
	if _, err := fixture.editor.ApplyWithFiles(ctx, func(bundle *config.Bundle) error {
		bundle.Personas[0].Skills = []string{"missing-slug"}
		return nil
	}, []app.FileOp{{Op: app.OpWrite, Path: skillPath, Bytes: updatedBytes}}); err == nil {
		t.Fatalf("ApplyWithFiles() error = nil, want validation failure")
	}

	currentSkill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile(go.md after) error = %v", err)
	}
	if string(currentSkill) != string(originalSkill) {
		t.Fatalf("go.md was not restored after rollback\n  before: %q\n  after:  %q", originalSkill, currentSkill)
	}
	currentWiring, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("ReadFile(omakiten.yaml after) error = %v", err)
	}
	if string(currentWiring) != string(originalWiring) {
		t.Fatalf("omakiten.yaml was not restored after rollback")
	}
}

func TestLawServiceEditNoChange(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)
	service := app.NewLawService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files, testfixtures.CanonicalRegistry())

	// Edit with no changes should return current law
	law, err := service.Show(ctx, "scope")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	current, err := service.Edit(ctx, "scope", domain.LawUpdate{})
	if err != nil {
		t.Fatalf("Edit(no change) error = %v", err)
	}
	if current.Body != law.Body {
		t.Fatalf("Edit(no change).Body = %q, want %q", current.Body, law.Body)
	}
}

func TestSkillServiceEditNoChange(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)
	service := app.NewSkillService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)

	skill, err := service.Show(ctx, "go")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	current, err := service.Edit(ctx, "go", domain.SkillUpdate{})
	if err != nil {
		t.Fatalf("Edit(no change) error = %v", err)
	}
	if current.Name != skill.Name {
		t.Fatalf("Edit(no change).Name = %q, want %q", current.Name, skill.Name)
	}
}

func TestPersonaServiceEditNoChange(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)
	service := app.NewPersonaService(fixture.store.Snapshot(), fixture.editor, fixture.files, fixture.files)

	persona, err := service.Show(ctx, "backend-agent")
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	current, err := service.Edit(ctx, "backend-agent", domain.PersonaUpdate{})
	if err != nil {
		t.Fatalf("Edit(no change) error = %v", err)
	}
	if current.Name != persona.Name {
		t.Fatalf("Edit(no change).Name = %q, want %q", current.Name, persona.Name)
	}
}

func TestBundleEditorSetPathRepointsForSubsequentLoads(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)

	// Drop a second valid bundle alongside the first one so SetPath has a real
	// target to repoint at. The editor was already wired to fixture.configPath
	// via newEntitiesFixture, so this exercise mirrors a config-preset swap.
	altDir := filepath.Join(filepath.Dir(fixture.configDir), "alt")
	if err := os.MkdirAll(altDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(alt) error = %v", err)
	}
	altPath := filepath.Join(altDir, "omakiten.yaml")
	altBundle := fixtureBundle(t)
	altBundle.Skills = append(altBundle.Skills, config.Skill{Slug: "alt-only", Name: "Alt"})
	if err := config.SaveFullBundle(altPath, altBundle); err != nil {
		t.Fatalf("SaveFullBundle(alt) error = %v", err)
	}

	if fixture.editor.Path() != fixture.configPath {
		t.Fatalf("Path() = %q, want %q", fixture.editor.Path(), fixture.configPath)
	}

	fixture.editor.SetPath(altPath)
	if fixture.editor.Path() != altPath {
		t.Fatalf("Path() after SetPath = %q, want %q", fixture.editor.Path(), altPath)
	}

	bundle, err := fixture.editor.Load()
	if err != nil {
		t.Fatalf("Load() after SetPath error = %v", err)
	}
	foundAlt := false
	for _, s := range bundle.Skills {
		if s.Slug == "alt-only" {
			foundAlt = true
		}
	}
	if !foundAlt {
		t.Fatalf("Load() after SetPath did not return alt bundle; skills=%v", bundle.Skills)
	}
	_ = ctx
}

func TestBundleEditorNilMutator(t *testing.T) {
	ctx := context.Background()
	fixture := newEntitiesFixture(t)

	_, err := fixture.editor.ApplyWithFiles(ctx, nil, nil)
	if err != nil {
		t.Fatalf("ApplyWithFiles(nil mutator) error = %v", err)
	}
}

func TestResolveEditorFallsBackToNano(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if got := app.ResolveEditor(); got != "nano" {
		t.Fatalf("ResolveEditor() = %q, want nano", got)
	}
	t.Setenv("VISUAL", "vim")
	if got := app.ResolveEditor(); got != "vim" {
		t.Fatalf("ResolveEditor() = %q, want vim", got)
	}
	t.Setenv("EDITOR", "code --wait")
	if got := app.ResolveEditor(); got != "code --wait" {
		t.Fatalf("ResolveEditor() = %q, want code --wait", got)
	}
}

func assertCoded(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", want)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %v (%T), want *domain.CodedError", err, err)
	}
	if coded.Code != want {
		t.Fatalf("error code = %s, want %s", coded.Code, want)
	}
}
