package operation

import (
	"testing"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// newPlanCommentFixture is the agent fixture with plans "alpha" and "beta".
func newPlanCommentFixture(t *testing.T) agentFixture {
	t.Helper()
	fixture := newAgentFixture(t)
	for _, slug := range []string{"alpha", "beta"} {
		if _, err := fixture.service.CreatePlan(fixture.ctx, contract.CreatePlanInput{Slug: slug, Name: slug}); err != nil {
			t.Fatalf("CreatePlan(%s) error = %v", slug, err)
		}
	}
	return fixture
}

// TestPlanScopedCommentsNeedAKnownPlan covers the plan comment targets: plan
// scope needs a plan the project has, and only plan scope may name one.
func TestPlanScopedCommentsNeedAKnownPlan(t *testing.T) {
	fixture := newPlanCommentFixture(t)
	_, err := fixture.service.AddComment(fixture.ctx, contract.AddCommentInput{Scope: domain.CommentScopePlan, Body: "x"})
	assertCodedError(t, err, domain.ErrValidation)
	_, err = fixture.service.AddComment(fixture.ctx, contract.AddCommentInput{TaskID: fixture.taskA1.ID, PlanSlug: "alpha", Body: "x"})
	assertCodedError(t, err, domain.ErrValidation)
	_, err = fixture.service.AddComment(fixture.ctx, contract.AddCommentInput{Scope: domain.CommentScopePlan, PlanSlug: "missing", Body: "x"})
	assertCodedError(t, err, domain.ErrPlanNotFound)
}

// TestPlanScopedComments covers comments that hang off a plan: they list per
// plan apart from task comments, and leave with the plan.
func TestPlanScopedComments(t *testing.T) {
	fixture := newPlanCommentFixture(t)

	added, err := fixture.service.AddComment(fixture.ctx, contract.AddCommentInput{
		Scope: domain.CommentScopePlan, PlanSlug: "alpha", Body: "kickoff done", AuthorType: "agent", Tags: []string{"kickoff"},
	})
	if err != nil {
		t.Fatalf("AddComment(plan) error = %v", err)
	}
	if added.Comment.Scope != domain.CommentScopePlan || added.Comment.PlanID == 0 || added.Comment.TaskID != 0 {
		t.Fatalf("plan comment = %+v, want scope plan with plan_id and no task_id", added.Comment)
	}
	if _, err := fixture.service.AddComment(fixture.ctx, contract.AddCommentInput{
		Scope: domain.CommentScopePlan, PlanSlug: "beta", Body: "other plan", AuthorType: "agent",
	}); err != nil {
		t.Fatalf("AddComment(beta) error = %v", err)
	}

	listed, err := fixture.service.ListComments(fixture.ctx, contract.ListCommentsInput{PlanSlug: "alpha"})
	if err != nil {
		t.Fatalf("ListComments(plan) error = %v", err)
	}
	if len(listed.Comments) != 1 || listed.Comments[0].ID != added.Comment.ID {
		t.Fatalf("ListComments(alpha) = %+v, want only the alpha comment", listed.Comments)
	}
	if len(listed.Comments[0].Tags) != 1 || listed.Comments[0].Tags[0].Name != "kickoff" {
		t.Fatalf("plan comment tags = %+v, want kickoff", listed.Comments[0].Tags)
	}

	if _, err := fixture.service.DeletePlan(fixture.ctx, contract.DeletePlanInput{Slug: "alpha", Confirmed: true}); err != nil {
		t.Fatalf("DeletePlan() error = %v", err)
	}
	left, err := fixture.service.ListComments(fixture.ctx, contract.ListCommentsInput{Scope: domain.CommentScopePlan})
	if err != nil {
		t.Fatalf("ListComments(scope plan) error = %v", err)
	}
	if len(left.Comments) != 1 || left.Comments[0].Body != "other plan" {
		t.Fatalf("plan comments after delete = %+v, want only beta's", left.Comments)
	}
}
