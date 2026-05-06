package agent

import (
	"testing"
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

	// Filter by kind.
	prs, err := fixture.service.ListTemplates(fixture.ctx, ListTemplatesInput{Kind: "pr"})
	if err != nil {
		t.Fatalf("ListTemplates(kind=pr) error = %v", err)
	}
	if len(prs.Templates) != 1 || prs.Templates[0].Slug != "pr-default" {
		t.Fatalf("ListTemplates(kind=pr) = %+v, want only pr-default", prs.Templates)
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
