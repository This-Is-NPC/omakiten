package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
)

type entitiesFixture struct {
	store      *sqlite.Store
	editor     *app.BundleEditor
	configPath string
	configDir  string
}

func newEntitiesFixture(t *testing.T) entitiesFixture {
	t.Helper()
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config")
	configPath := filepath.Join(configDir, "omakiten.yaml")
	dbPath := filepath.Join(tmp, "omakiten.db")

	if err := config.SaveFullBundle(configPath, fixtureBundle()); err != nil {
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
		t.Fatalf("editor.Apply(seed) error = %v", err)
	}

	return entitiesFixture{store: store, editor: editor, configPath: configPath, configDir: configDir}
}

func fixtureBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default Kit"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills: []config.Skill{
			{Slug: "go", Name: "Go", Description: "Go language."},
			{Slug: "sqlite", Name: "SQLite", Description: "SQLite stack."},
		},
		Personas: []config.Persona{
			{Slug: "backend-agent", Name: "Backend Agent", Description: "Backend persona", Skills: []string{"go", "sqlite"}},
		},
		Laws: []config.Law{
			{Slug: "scope", Severity: "error", Body: "Stay in scope.", Scope: "global"},
		},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}

func TestLawServiceLifecycle(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		input   domain.LawInput
		wantErr domain.ErrorCode
	}{
		{name: "creates law", input: domain.LawInput{Key: "warn-only", Severity: domain.LawSeverityWarning, Body: "Be careful."}},
		{name: "rejects empty key", input: domain.LawInput{Key: " ", Severity: domain.LawSeverityInfo, Body: "anything"}, wantErr: domain.ErrValidation},
		{name: "rejects empty body", input: domain.LawInput{Key: "x", Severity: domain.LawSeverityInfo, Body: " "}, wantErr: domain.ErrValidation},
		{name: "rejects invalid severity", input: domain.LawInput{Key: "x", Severity: domain.LawSeverity("fatal"), Body: "anything"}, wantErr: domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newEntitiesFixture(t)
			service := app.NewLawService(fixture.store, fixture.editor)

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
	service := app.NewLawService(fixture.store, fixture.editor)

	if _, err := service.Add(ctx, domain.LawInput{Key: "scope", Severity: domain.LawSeverityError, Body: "anything"}); err == nil {
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
			service := app.NewSkillService(fixture.store, fixture.editor)
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
	service := app.NewSkillService(fixture.store, fixture.editor)

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
	service := app.NewPersonaService(fixture.store, fixture.editor)
	skillService := app.NewSkillService(fixture.store, fixture.editor)
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

	skillService := app.NewSkillService(fixture.store, fixture.editor)
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

	skillPath := config.EntityFilePath(fixture.configDir, config.EntityKindSkill, "go")
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
