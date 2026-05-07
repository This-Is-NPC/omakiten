package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"omakiten/internal/domain"
)

func TestCreateTaskIncludesActiveTemplate(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTaskTemplateLookup(func(projectSlug string) *TaskTemplateSummary {
		return &TaskTemplateSummary{
			Slug: "task-default",
			Name: "Default",
			Body: "**User Story**\n\nComo X.",
		}
	})

	resp, err := fixture.service.CreateTask(fixture.ctx, CreateTaskInput{Title: "Brand new direction", Description: "Unrelated"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if resp.Template == nil {
		t.Fatal("CreateTask().Template = nil, want active template")
	}
	if resp.Template.Slug != "task-default" {
		t.Fatalf("CreateTask().Template.Slug = %q, want task-default", resp.Template.Slug)
	}
	if resp.Template.Body == "" {
		t.Fatal("CreateTask().Template.Body empty, want scaffold body")
	}
}

func TestCreateTaskIntentIncludesTemplateOnSimilarityFork(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTaskTemplateLookup(func(projectSlug string) *TaskTemplateSummary {
		return &TaskTemplateSummary{Slug: "task-default", Body: "scaffold"}
	})

	// Title/description matching an existing fixture task triggers the
	// confirmation fork — template must still be returned so the agent has
	// the scaffold ready when the user confirms a separate task.
	resp, err := fixture.service.CreateTaskIntent(fixture.ctx, CreateTaskInput{Description: "Add MCP agent integration for AI harnesses"})
	if err != nil {
		t.Fatalf("CreateTaskIntent() error = %v", err)
	}
	if !resp.Confirmation.RequiresConfirmation {
		t.Fatal("expected similarity confirmation fork")
	}
	if resp.Template == nil || resp.Template.Body != "scaffold" {
		t.Fatalf("CreateTaskIntent().Template = %+v, want scaffold body even on similarity fork", resp.Template)
	}
}

func TestCreateTaskWithoutTemplateLookupOmitsField(t *testing.T) {
	fixture := newAgentFixture(t)
	// No SetTaskTemplateLookup call.

	resp, err := fixture.service.CreateTask(fixture.ctx, CreateTaskInput{Title: "Some unique work", Description: "x"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if resp.Template != nil {
		t.Fatalf("CreateTask().Template = %+v, want nil when lookup unset", resp.Template)
	}
}

func TestListTemplatesReturnsCatalog(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "task-default", Name: "Default", Default: "task", Body: "scaffold"},
			{Slug: "pr-default", Name: "PR", Default: "pr", IsCustom: true, Body: "checklist"},
		}
	})

	resp, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{})
	if err != nil {
		t.Fatalf("ListTemplates() error = %v", err)
	}
	if len(resp.Templates) != 2 {
		t.Fatalf("Templates len = %d, want 2", len(resp.Templates))
	}
	// Body omitted by default for compact responses.
	if resp.Templates[0].Body != "" {
		t.Fatalf("default response should omit body, got %q", resp.Templates[0].Body)
	}

	// IncludeBody=true returns the bodies.
	full, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{IncludeBody: true})
	if err != nil {
		t.Fatalf("ListTemplates() error = %v", err)
	}
	if full.Templates[0].Body != "scaffold" {
		t.Fatalf("IncludeBody=true should expose body, got %q", full.Templates[0].Body)
	}

	// Filter by kind (no project) — non-resolving, returns every match.
	prs, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{Kind: "pr"})
	if err != nil {
		t.Fatalf("ListTemplates(kind=pr) error = %v", err)
	}
	if len(prs.Templates) != 1 || prs.Templates[0].Slug != "pr-default" {
		t.Fatalf("ListTemplates(kind=pr) = %+v, want only pr-default", prs.Templates)
	}
}

func TestListTemplatesResolvesProjectScopedOverridesServerSide(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "pull-request", Name: "Global PR", Default: "pr"},
			{Slug: "pr-concise", Name: "Concise", Default: "pr", Project: "omakiten", IsCustom: true},
			{Slug: "user-story", Name: "Story", Default: "task"},
		}
	})

	// Project-aware request for kind=pr returns ONLY the project-scoped
	// override — global is dropped server-side so the agent does not have
	// to filter or pay tokens for the fallback.
	resp, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{Kind: "pr", Project: "omakiten"})
	if err != nil {
		t.Fatalf("ListTemplates() error = %v", err)
	}
	if len(resp.Templates) != 1 {
		t.Fatalf("project-aware list len = %d, want 1 (resolution should drop global)", len(resp.Templates))
	}
	if resp.Templates[0].Slug != "pr-concise" {
		t.Fatalf("expected pr-concise to win for project=omakiten, got %s", resp.Templates[0].Slug)
	}

	// Different project with no scoped override falls back to the global.
	other, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{Kind: "pr", Project: "another"})
	if err != nil {
		t.Fatalf("ListTemplates(another) error = %v", err)
	}
	if len(other.Templates) != 1 || other.Templates[0].Slug != "pull-request" {
		t.Fatalf("expected pull-request global fallback for project=another, got %+v", other.Templates)
	}

	// Project-only filter (no kind) collapses every kind to one resolved
	// entry — pr → pr-concise, task → user-story.
	all, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{Project: "omakiten"})
	if err != nil {
		t.Fatalf("ListTemplates(project=omakiten) error = %v", err)
	}
	if len(all.Templates) != 2 {
		t.Fatalf("project-only resolved set len = %d, want 2 (one per kind)", len(all.Templates))
	}
	bySlug := map[string]bool{}
	for _, t := range all.Templates {
		bySlug[t.Slug] = true
	}
	if !bySlug["pr-concise"] || !bySlug["user-story"] {
		t.Fatalf("project=omakiten should resolve to pr-concise and user-story, got %+v", all.Templates)
	}
	if bySlug["pull-request"] {
		t.Fatalf("global pull-request should be dropped when an override exists, got %+v", all.Templates)
	}
}

func TestShowTemplateReturnsBody(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "task-default", Name: "Default", Default: "task", Body: "scaffold"},
		}
	})

	resp, err := fixture.service.ShowTemplate(fixture.ctx, ShowTemplateInput{Slug: "task-default"})
	if err != nil {
		t.Fatalf("ShowTemplate() error = %v", err)
	}
	if resp.Template.Body != "scaffold" {
		t.Fatalf("ShowTemplate body = %q, want scaffold", resp.Template.Body)
	}

	if _, err := fixture.service.ShowTemplate(fixture.ctx, ShowTemplateInput{Slug: "missing"}); err == nil {
		t.Fatal("ShowTemplate(missing) error = nil, want not-found")
	}
}

// shadowCatalog mirrors the omakiten "pull-request" / "pr-concise" pair the
// rigid validation is designed to disambiguate. project-a is the fixture's
// default-resolved project (CWD = rootA), so the override applies whenever
// the test reuses newAgentFixture's service.
func shadowCatalog() func() []TemplateSummary {
	return func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "pull-request", Name: "Pull Request", Default: "pr", Body: "global"},
			{Slug: "pr-concise", Name: "Concise PR", Default: "pr", Project: "project-a", Body: "concise", IsCustom: true},
		}
	}
}

func TestShowTemplateRejectsShadowedSlug(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTemplateCatalog(shadowCatalog())

	_, err := fixture.service.ShowTemplate(fixture.ctx, ShowTemplateInput{Slug: "pull-request"})
	if err == nil {
		t.Fatal("ShowTemplate(shadowed) error = nil, want validation_error")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("error = %v (%T), want CodedError(validation_error)", err, err)
	}
	if got, want := coded.Details["requested_slug"], "pull-request"; got != want {
		t.Fatalf("details.requested_slug = %v, want %v", got, want)
	}
	if got, want := coded.Details["active_slug"], "pr-concise"; got != want {
		t.Fatalf("details.active_slug = %v, want %v", got, want)
	}
	if got, want := coded.Details["project"], "project-a"; got != want {
		t.Fatalf("details.project = %v, want %v", got, want)
	}
}

func TestShowTemplateReturnsProjectScopedSlug(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTemplateCatalog(shadowCatalog())

	resp, err := fixture.service.ShowTemplate(fixture.ctx, ShowTemplateInput{Slug: "pr-concise"})
	if err != nil {
		t.Fatalf("ShowTemplate(scoped) error = %v", err)
	}
	if resp.Template.Body != "concise" {
		t.Fatalf("Body = %q, want concise", resp.Template.Body)
	}
}

func TestShowTemplateReturnsGlobalWhenNoOverride(t *testing.T) {
	fixture := newAgentFixture(t)
	// Catalog has only a global "user-story" — no project-a override, so the
	// shadow check must fall through.
	fixture.service.SetTemplateCatalog(func() []TemplateSummary {
		return []TemplateSummary{
			{Slug: "user-story", Name: "User Story", Default: "task", Body: "task scaffold"},
		}
	})

	resp, err := fixture.service.ShowTemplate(fixture.ctx, ShowTemplateInput{Slug: "user-story"})
	if err != nil {
		t.Fatalf("ShowTemplate(unshadowed) error = %v", err)
	}
	if resp.Template.Body != "task scaffold" {
		t.Fatalf("Body = %q, want task scaffold", resp.Template.Body)
	}
}

func TestShowTemplateAllowsShadowedSlugWithoutProjectContext(t *testing.T) {
	// Service whose default selector points at a directory outside any
	// registered project root → resolveProject returns ErrProjectNotFound, but
	// because the failure came from CWD/default (not an explicit project_id /
	// project) the call tolerates it and falls back to the legacy slug lookup.
	ctx := context.Background()
	store := newAgentStore(t, ctx)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	service := NewService(store, ProjectSelector{CWD: outside})
	service.SetTemplateCatalog(shadowCatalog())

	resp, err := service.ShowTemplate(ctx, ShowTemplateInput{Slug: "pull-request"})
	if err != nil {
		t.Fatalf("ShowTemplate(no project) error = %v", err)
	}
	if resp.Template.Body != "global" {
		t.Fatalf("Body = %q, want global", resp.Template.Body)
	}
}

func TestShowTemplateRejectsExplicitMissingProject(t *testing.T) {
	// AC #5: an explicit project slug that does not resolve must propagate
	// ErrProjectNotFound rather than silently fall back to global lookup.
	fixture := newAgentFixture(t)
	fixture.service.SetTemplateCatalog(shadowCatalog())

	_, err := fixture.service.ShowTemplate(fixture.ctx, ShowTemplateInput{
		ProjectSelector: ProjectSelector{Project: "no-such-project"},
		Slug:            "pull-request",
	})
	if err == nil {
		t.Fatal("ShowTemplate(explicit missing project) error = nil, want project_not_found")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("error = %v, want CodedError(project_not_found)", err)
	}
}
