package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func setupPlans(t *testing.T) (context.Context, *storeFixture, domain.ProjectContext) {
	t.Helper()
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	bundle, _ := testfixtures.LoadBundle(t, "lifecycle_policy.yaml")
	store.applyBundle(bundle)
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	return ctx, store, project.Context()
}

func TestCreatePlanRoundTrip(t *testing.T) {
	ctx, store, project := setupPlans(t)

	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "Goal markdown")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.Slug != "plan-a" || plan.Name != "Plan A" || plan.GoalBody != "Goal markdown" {
		t.Fatalf("CreatePlan returned %+v", plan)
	}
	if plan.Status != domain.PlanStatusActive {
		t.Fatalf("plan status = %q, want active", plan.Status)
	}

	got, err := store.GetPlanBySlug(ctx, project.ID, "plan-a")
	if err != nil {
		t.Fatalf("GetPlanBySlug: %v", err)
	}
	if got.ID != plan.ID {
		t.Fatalf("GetPlanBySlug id = %d, want %d", got.ID, plan.ID)
	}

	all, err := store.ListPlans(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(all) != 1 || all[0].ID != plan.ID {
		t.Fatalf("ListPlans = %+v", all)
	}
}

func TestCreatePlanRejectsDuplicateSlug(t *testing.T) {
	ctx, store, project := setupPlans(t)
	if _, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", ""); err != nil {
		t.Fatalf("first CreatePlan: %v", err)
	}
	_, err := store.CreatePlan(ctx, project.ID, "plan-a", "Other", "")
	if err == nil {
		t.Fatal("expected duplicate slug error, got nil")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrPlanSlugConflict {
		t.Fatalf("duplicate error = %v, want ErrPlanSlugConflict", err)
	}
}

func TestCreatePlanEmitsPlanCreatedEvent(t *testing.T) {
	ctx, store, project := setupPlans(t)
	if _, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", ""); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	events, err := store.ListRecentEvents(ctx, domain.EventTypePlanCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("plan.created events = %d, want 1", len(events))
	}
	if events[0].EntityType != domain.EventEntityPlan {
		t.Fatalf("entity_type = %q, want %q", events[0].EntityType, domain.EventEntityPlan)
	}
}

func TestAddPlanWaveAutoIncrementsPosition(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	w1, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave 1: %v", err)
	}
	if w1.Position != 1 {
		t.Fatalf("first wave position = %d, want 1", w1.Position)
	}

	w2, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-two", 0)
	if err != nil {
		t.Fatalf("AddPlanWave 2: %v", err)
	}
	if w2.Position != 2 {
		t.Fatalf("second wave position = %d, want 2", w2.Position)
	}

	waves, err := store.ListPlanWaves(ctx, project.ID, plan.ID)
	if err != nil {
		t.Fatalf("ListPlanWaves: %v", err)
	}
	if len(waves) != 2 || waves[0].Position != 1 || waves[1].Position != 2 {
		t.Fatalf("ListPlanWaves = %+v", waves)
	}
}

func TestAddPlanWaveRejectsCollidingExplicitPosition(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if _, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 1); err != nil {
		t.Fatalf("AddPlanWave 1: %v", err)
	}
	_, err = store.AddPlanWave(ctx, project.ID, plan.ID, "wave-clash", 1)
	if err == nil {
		t.Fatal("expected collision, got nil")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("collision error = %v, want ErrValidation", err)
	}
}

func TestAssignTaskToPlanLinksAndScopes(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan: %v", err)
	}

	var planID, waveID int64
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(plan_id,0), COALESCE(wave_id,0) FROM tasks WHERE id = ?`, task.ID).Scan(&planID, &waveID); err != nil {
		t.Fatalf("scan tasks: %v", err)
	}
	if planID != plan.ID || waveID != wave.ID {
		t.Fatalf("task plan_id=%d wave_id=%d, want %d/%d", planID, waveID, plan.ID, wave.ID)
	}
}

func TestAssignTaskToPlanRejectsForeignWave(t *testing.T) {
	ctx, store, project := setupPlans(t)
	planA, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan A: %v", err)
	}
	planB, err := store.CreatePlan(ctx, project.ID, "plan-b", "Plan B", "")
	if err != nil {
		t.Fatalf("CreatePlan B: %v", err)
	}
	waveB, err := store.AddPlanWave(ctx, project.ID, planB.ID, "wave-b", 0)
	if err != nil {
		t.Fatalf("AddPlanWave B: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = store.AssignTaskToPlan(ctx, project.ID, task.ID, planA.ID, waveB.ID)
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrPlanWaveNotFound {
		t.Fatalf("mismatch error = %v, want ErrPlanWaveNotFound", err)
	}
}

func TestClaimNextPlanTaskReturnsEmptyWhenNothingClaimable(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if _, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0); err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}

	ctx = activity.WithAgent(ctx, "mcp", "plans.claim_next", "claude-opus-4-7", "")
	task, ok, err := store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("ClaimNextPlanTask: %v", err)
	}
	if ok || task.ID != 0 {
		t.Fatalf("ClaimNextPlanTask = (%+v, %v), want (zero, false) on empty plan", task, ok)
	}
}

func TestClaimNextPlanTaskMovesTaskAndStampsAssignee(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Claim me", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan: %v", err)
	}

	ctx = activity.WithAgent(ctx, "mcp", "plans.claim_next", "claude-opus-4-7", "")
	claimed, ok, err := store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("ClaimNextPlanTask: %v", err)
	}
	if !ok || claimed.ID != task.ID {
		t.Fatalf("ClaimNextPlanTask = (%+v, %v), want claim of task %d", claimed, ok, task.ID)
	}
	if claimed.BucketKey != "dev" {
		t.Fatalf("claimed bucket = %q, want dev", claimed.BucketKey)
	}

	var assignedTo string
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(assigned_to,'') FROM tasks WHERE id = ?`, task.ID).Scan(&assignedTo); err != nil {
		t.Fatalf("scan assigned_to: %v", err)
	}
	if assignedTo != "claude-opus-4-7" {
		t.Fatalf("assigned_to = %q, want claude-opus-4-7", assignedTo)
	}

	// Second call → nothing left.
	_, ok, err = store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("ClaimNextPlanTask second: %v", err)
	}
	if ok {
		t.Fatal("second claim succeeded; expected empty")
	}
}

func TestClaimNextPlanTaskRequiresAgentModel(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if _, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0); err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}

	_, _, err = store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err == nil {
		t.Fatal("expected validation error without _agent_model")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("missing-model error = %v, want ErrValidation", err)
	}
}

func TestClaimNextPlanTaskGatesAcrossWaves(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	w1, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave 1: %v", err)
	}
	w2, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-two", 0)
	if err != nil {
		t.Fatalf("AddPlanWave 2: %v", err)
	}

	t1, err := store.CreateTask(ctx, project.ID, "T1", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask 1: %v", err)
	}
	t2, err := store.CreateTask(ctx, project.ID, "T2", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask 2: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, t1.ID, plan.ID, w1.ID); err != nil {
		t.Fatalf("AssignTaskToPlan 1: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, t2.ID, plan.ID, w2.ID); err != nil {
		t.Fatalf("AssignTaskToPlan 2: %v", err)
	}

	ctx = activity.WithAgent(ctx, "mcp", "plans.claim_next", "claude-opus-4-7", "")

	first, ok, err := store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !ok || first.ID != t1.ID {
		t.Fatalf("first claim = (%+v, %v), want wave-1 task %d", first, ok, t1.ID)
	}

	// Wave 1 still pending (claimed task moved to dev, not done). Wave 2
	// must not be reachable yet — claim returns empty.
	_, ok, err = store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("blocked claim: %v", err)
	}
	if ok {
		t.Fatal("wave 2 claim succeeded while wave 1 still has dev work; expected gate")
	}

	// Move wave-1 task to final bucket → wave 2 becomes claimable.
	if _, err := store.MoveTask(ctx, project.ID, t1.ID, "done", store.snap()); err != nil {
		t.Fatalf("MoveTask t1→done: %v", err)
	}
	second, ok, err := store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if !ok || second.ID != t2.ID {
		t.Fatalf("second claim = (%+v, %v), want wave-2 task %d after wave-1 done", second, ok, t2.ID)
	}
}

func TestClaimNextPlanTaskIsAtomicUnderConcurrency(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}

	const n = 8
	taskIDs := map[int64]struct{}{}
	for i := 0; i < n; i++ {
		task, err := store.CreateTask(ctx, project.ID, "Race", "", domain.Priority(2), "backlog", store.snap())
		if err != nil {
			t.Fatalf("CreateTask %d: %v", i, err)
		}
		if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
			t.Fatalf("AssignTaskToPlan %d: %v", i, err)
		}
		taskIDs[task.ID] = struct{}{}
	}

	results := make(chan int64, n*2)
	var wg sync.WaitGroup
	for i := 0; i < n*2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			wctx := activity.WithAgent(ctx, "mcp", "plans.claim_next",
				"claude-opus-4-7-worker", "")
			claimed, ok, err := store.ClaimNextPlanTask(wctx, project.ID, plan.ID, store.snap())
			if err != nil {
				t.Errorf("worker %d claim: %v", workerID, err)
				results <- 0
				return
			}
			if !ok {
				results <- 0
				return
			}
			results <- claimed.ID
		}(i)
	}
	wg.Wait()
	close(results)

	seen := map[int64]int{}
	successes := 0
	for id := range results {
		if id == 0 {
			continue
		}
		seen[id]++
		successes++
	}
	if successes != n {
		t.Fatalf("successful claims = %d, want %d", successes, n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("task %d claimed %d times, want 1 (double-claim)", id, count)
		}
		if _, ok := taskIDs[id]; !ok {
			t.Fatalf("claim returned unknown task id %d", id)
		}
	}
	if len(seen) != n {
		t.Fatalf("unique tasks claimed = %d, want %d", len(seen), n)
	}
}

func TestCountPriorWavesPending(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	w1, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave 1: %v", err)
	}
	w2, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-two", 0)
	if err != nil {
		t.Fatalf("AddPlanWave 2: %v", err)
	}

	t1, err := store.CreateTask(ctx, project.ID, "T1", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask 1: %v", err)
	}
	t2, err := store.CreateTask(ctx, project.ID, "T2", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask 2: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, t1.ID, plan.ID, w1.ID); err != nil {
		t.Fatalf("AssignTaskToPlan 1: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, t2.ID, plan.ID, w2.ID); err != nil {
		t.Fatalf("AssignTaskToPlan 2: %v", err)
	}

	// t2 sits in wave 2; t1 is still pending in wave 1 → wave_gate=1.
	count, err := store.CountPriorWavesPending(ctx, project.ID, t2.ID, store.snap())
	if err != nil {
		t.Fatalf("CountPriorWavesPending: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1 (wave-1 still pending)", count)
	}

	// Move t1 to done → wave 1 empty → wave_gate clears.
	if _, err := store.MoveTask(ctx, project.ID, t1.ID, "done", store.snap()); err != nil {
		t.Fatalf("MoveTask t1: %v", err)
	}
	count, err = store.CountPriorWavesPending(ctx, project.ID, t2.ID, store.snap())
	if err != nil {
		t.Fatalf("CountPriorWavesPending after done: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 after prior wave done", count)
	}

	// Standalone task (no plan/wave) → guard is a no-op (count=0).
	bare, err := store.CreateTask(ctx, project.ID, "Bare", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask bare: %v", err)
	}
	count, err = store.CountPriorWavesPending(ctx, project.ID, bare.ID, store.snap())
	if err != nil {
		t.Fatalf("CountPriorWavesPending bare: %v", err)
	}
	if count != 0 {
		t.Fatalf("count for non-plan task = %d, want 0", count)
	}
}

func TestGetPlanByIDScopesByProject(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	otherProject, err := store.UpsertProject(ctx, "Other", "other", "/work/other")
	if err != nil {
		t.Fatalf("UpsertProject other: %v", err)
	}
	_, err = store.GetPlanByID(ctx, otherProject.ID, plan.ID)
	if err == nil {
		t.Fatal("expected cross-project leak guard")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrPlanNotFound {
		t.Fatalf("cross-project error = %v, want ErrPlanNotFound", err)
	}
}
