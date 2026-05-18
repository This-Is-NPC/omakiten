package agent

import (
	"testing"

	"omakiten/internal/domain"
)

// moveTaskToBucket walks the workflow buckets in ascending position
// order, calling repo.MoveTask once per hop until the target is
// reached. We bypass app.WorkflowService.MoveTask (which enforces
// the per-transition allowlist) because tests only need the bucket
// state change, not the policy gate — the test fixture's workflow
// declares a single transition (1→2) so the policy path would refuse
// the second hop.
func moveTaskToBucket(t *testing.T, f agentFixture, taskID int64, targetKey string) {
	t.Helper()
	snap := f.store.Snapshot()
	target, ok := snap.BucketByKey(targetKey)
	if !ok {
		t.Fatalf("bucket %q not in active workflow", targetKey)
	}
	buckets := append([]domain.Bucket(nil), snap.Workflow().Buckets...)
	for _, b := range buckets {
		if b.Position <= 0 || b.Position > target.Position {
			continue
		}
		// Skip the starting backlog hop — task is already there.
		if b.Position == 1 {
			continue
		}
		if _, err := f.store.MoveTask(f.ctx, f.projectA.ID, taskID, b.Key, snap); err != nil {
			t.Fatalf("MoveTask(%s): %v", b.Key, err)
		}
	}
}

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

func TestAddPlanWaveAndShowPlanRoundTrip(t *testing.T) {
	fixture := newAgentFixture(t)

	plan, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{Slug: "ship", Name: "Ship", GoalBody: "Goal"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	if _, err := fixture.service.AddPlanWave(fixture.ctx, AddPlanWaveInput{Slug: "ship", Name: "alpha"}); err != nil {
		t.Fatalf("AddPlanWave alpha: %v", err)
	}
	if _, err := fixture.service.AddPlanWave(fixture.ctx, AddPlanWaveInput{PlanID: plan.Plan.ID, Name: "beta"}); err != nil {
		t.Fatalf("AddPlanWave beta: %v", err)
	}

	show, err := fixture.service.ShowPlan(fixture.ctx, ShowPlanInput{Slug: "ship"})
	if err != nil {
		t.Fatalf("ShowPlan: %v", err)
	}
	if len(show.Waves) != 2 {
		t.Fatalf("waves len = %d, want 2: %+v", len(show.Waves), show.Waves)
	}
	if show.Waves[0].Position != 1 || show.Waves[1].Position != 2 {
		t.Fatalf("wave positions = %d/%d, want 1/2", show.Waves[0].Position, show.Waves[1].Position)
	}
	if show.Plan.GoalBody != "Goal" {
		t.Fatalf("show plan goal_body = %q, want Goal", show.Plan.GoalBody)
	}
	if show.TotalCount != 0 || show.Percent != 0 {
		t.Fatalf("show counts = %+v, want zero (no tasks yet)", show)
	}
	if show.ActiveWaveID != show.Waves[0].ID {
		t.Fatalf("active wave = %d, want %d (first wave when nothing done)", show.ActiveWaveID, show.Waves[0].ID)
	}
}

func TestShowPlanComputesProgressAcrossWaves(t *testing.T) {
	fixture := newAgentFixture(t)

	plan, err := fixture.service.CreatePlan(fixture.ctx, CreatePlanInput{Slug: "prog", Name: "Progress"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	w1, err := fixture.service.AddPlanWave(fixture.ctx, AddPlanWaveInput{PlanID: plan.Plan.ID, Name: "wave-1"})
	if err != nil {
		t.Fatalf("AddPlanWave 1: %v", err)
	}
	w2, err := fixture.service.AddPlanWave(fixture.ctx, AddPlanWaveInput{PlanID: plan.Plan.ID, Name: "wave-2"})
	if err != nil {
		t.Fatalf("AddPlanWave 2: %v", err)
	}

	// Two tasks in wave 1, one in wave 2. Move one of the wave-1 tasks
	// into the final bucket (the test bundle's last bucket).
	ta, err := fixture.store.CreateTask(fixture.ctx, fixture.projectA.ID, "T-a", "", domain.Priority(2), "backlog", fixture.store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask a: %v", err)
	}
	tb, err := fixture.store.CreateTask(fixture.ctx, fixture.projectA.ID, "T-b", "", domain.Priority(2), "backlog", fixture.store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask b: %v", err)
	}
	tc, err := fixture.store.CreateTask(fixture.ctx, fixture.projectA.ID, "T-c", "", domain.Priority(2), "backlog", fixture.store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask c: %v", err)
	}

	if err := fixture.store.AssignTaskToPlan(fixture.ctx, fixture.projectA.ID, ta.ID, plan.Plan.ID, w1.Wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan ta: %v", err)
	}
	if err := fixture.store.AssignTaskToPlan(fixture.ctx, fixture.projectA.ID, tb.ID, plan.Plan.ID, w1.Wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan tb: %v", err)
	}
	if err := fixture.store.AssignTaskToPlan(fixture.ctx, fixture.projectA.ID, tc.ID, plan.Plan.ID, w2.Wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan tc: %v", err)
	}

	final := fixture.store.Snapshot().Workflow().FinalBucketKey()

	// Move ta to final bucket via the configured transition path:
	// (backlog → dev → review → done in the agentTestBundle workflow).
	// We can't assume the path length, so iterate transitions.
	moveTaskToBucket(t, fixture, ta.ID, final)

	show, err := fixture.service.ShowPlan(fixture.ctx, ShowPlanInput{Slug: "prog"})
	if err != nil {
		t.Fatalf("ShowPlan: %v", err)
	}
	if show.TotalCount != 3 {
		t.Fatalf("TotalCount = %d, want 3", show.TotalCount)
	}
	if show.DoneCount != 1 {
		t.Fatalf("DoneCount = %d, want 1", show.DoneCount)
	}
	if show.Percent != 33 {
		t.Fatalf("Percent = %d, want 33", show.Percent)
	}
	if show.ActiveWaveID != w1.Wave.ID {
		t.Fatalf("ActiveWaveID = %d, want wave 1 (%d) — still has pending work", show.ActiveWaveID, w1.Wave.ID)
	}
}
