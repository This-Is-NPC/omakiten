package agent

import (
	"testing"

	"omakiten/internal/domain"
)

func TestCreatePlanRoundTripThroughAgentService(t *testing.T) {
	fixture := newAgentFixture(t)

	resp, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{
		Slug:     "ship-mcp",
		Name:     "Ship MCP",
		GoalBody: "Goal markdown",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if resp.Plan.Slug != "ship-mcp" || resp.Plan.Name != "Ship MCP" {
		t.Fatalf("CreatePlan plan = %+v", resp.Plan)
	}
	if resp.Plan.Status != string(domain.PlanStatusActive) {
		t.Fatalf("CreatePlan status = %q, want active", resp.Plan.Status)
	}
	if resp.Project.ID != fixture.projectA.ID {
		t.Fatalf("CreatePlan project = %d, want %d", resp.Project.ID, fixture.projectA.ID)
	}
}

func TestListPlansScopesByActiveProject(t *testing.T) {
	fixture := newAgentFixture(t)

	if _, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{Slug: "p-one", Name: "Plan One"}); err != nil {
		t.Fatalf("CreatePlan A1: %v", err)
	}
	if _, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{Slug: "p-two", Name: "Plan Two"}); err != nil {
		t.Fatalf("CreatePlan A2: %v", err)
	}
	// Plan on project B must not leak into a project A list.
	if _, err := fixture.store.CreatePlan(fixture.ctx, fixture.projectB.ID, "p-b", "Plan B", ""); err != nil {
		t.Fatalf("CreatePlan B: %v", err)
	}

	resp, err := fixture.service.ListPlans(fixture.ctx, ListPlansInput{})
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(resp.Plans) != 2 {
		t.Fatalf("ListPlans got %d entries, want 2: %+v", len(resp.Plans), resp.Plans)
	}
	for _, p := range resp.Plans {
		if p.GoalBody != "" {
			t.Fatalf("ListPlans entry %s exposed goal_body in list view: %q", p.Slug, p.GoalBody)
		}
	}
}

func TestCreatePlanRejectsDuplicateSlugAtAgentLayer(t *testing.T) {
	fixture := newAgentFixture(t)

	if _, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{Slug: "dup", Name: "First"}); err != nil {
		t.Fatalf("CreatePlan first: %v", err)
	}
	_, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{Slug: "dup", Name: "Second"})
	assertCodedError(t, err, domain.ErrPlanSlugConflict)
}
