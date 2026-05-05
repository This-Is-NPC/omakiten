package agent

import (
	"testing"
)

func TestCreateTaskIncludesActiveTemplate(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.service.SetTaskTemplateLookup(func() *TaskTemplateSummary {
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
	fixture.service.SetTaskTemplateLookup(func() *TaskTemplateSummary {
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
