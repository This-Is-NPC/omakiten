package sqlite

import (
	"context"
	"database/sql"
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
	task, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", nil, store.snap())
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
	task, err := store.CreateTask(ctx, project.ID, "Child", "", domain.Priority(2), "backlog", nil, store.snap())
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

// TestClaimNextPlanTaskStampsAssigneeWithoutMovingBucket pins the new
// contract: claim only sets assigned_to; bucket stays put so the
// preset's bucket guards (e.g. omakase's self-branch comment for
// backlog → dev) remain authoritative. A second call on the same plan
// returns empty because the first task is no longer "unassigned".
func TestClaimNextPlanTaskStampsAssigneeWithoutMovingBucket(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Claim me", "", domain.Priority(2), "backlog", nil, store.snap())
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
	if claimed.BucketKey != "backlog" {
		t.Fatalf("claimed bucket = %q, want backlog (claim must NOT move the task)", claimed.BucketKey)
	}

	var assignedTo string
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(assigned_to,'') FROM tasks WHERE id = ?`, task.ID).Scan(&assignedTo); err != nil {
		t.Fatalf("scan assigned_to: %v", err)
	}
	if assignedTo != "claude-opus-4-7" {
		t.Fatalf("assigned_to = %q, want claude-opus-4-7", assignedTo)
	}

	// No task.moved event should have been emitted for the claim — the
	// task did not change buckets. assigned event must be present.
	var movedCount, assignedCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE entity_type = 'task' AND entity_id = ? AND event_type = ?`, task.ID, domain.EventTypeTaskMoved).Scan(&movedCount); err != nil {
		t.Fatalf("count task.moved events: %v", err)
	}
	if movedCount != 0 {
		t.Fatalf("task.moved emitted %d times after claim, want 0 (claim does not move buckets)", movedCount)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE entity_type = 'task' AND entity_id = ? AND event_type = ?`, task.ID, domain.EventTypeTaskAssigned).Scan(&assignedCount); err != nil {
		t.Fatalf("count task.assigned events: %v", err)
	}
	if assignedCount != 1 {
		t.Fatalf("task.assigned emitted %d times, want 1", assignedCount)
	}

	// Second call → nothing left (first task no longer unassigned).
	_, ok, err = store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("ClaimNextPlanTask second: %v", err)
	}
	if ok {
		t.Fatal("second claim succeeded; expected empty (already-claimed task is no longer candidate)")
	}
}

// TestListPlanTaskDependenciesFiltersToInPlanEdges proves the new
// repo method only returns edges where BOTH endpoints belong to the
// queried plan. Cross-plan or unattached-task edges are filtered out
// so the network renderer never draws arrows that point off-canvas.
func TestListPlanTaskDependenciesFiltersToInPlanEdges(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "deps", "Deps", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	a, err := store.CreateTask(ctx, project.ID, "A", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	b, err := store.CreateTask(ctx, project.ID, "B", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}
	outsider, err := store.CreateTask(ctx, project.ID, "outsider", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask outsider: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, a.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan A: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, b.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan B: %v", err)
	}

	// In-plan edge B → A (B depends on A) and out-of-plan edge A → outsider.
	if _, err := store.AddTaskDependency(ctx, project.ID, b.ID, a.ID); err != nil {
		t.Fatalf("AddTaskDependency in-plan: %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, a.ID, outsider.ID); err != nil {
		t.Fatalf("AddTaskDependency out-of-plan: %v", err)
	}

	deps, err := store.ListPlanTaskDependencies(ctx, project.ID, plan.ID)
	if err != nil {
		t.Fatalf("ListPlanTaskDependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("deps len = %d, want 1 (in-plan only): %+v", len(deps), deps)
	}
	if deps[0].TaskID != b.ID || deps[0].DependsOnTaskID != a.ID {
		t.Fatalf("dep = %+v, want B(%d) → A(%d)", deps[0], b.ID, a.ID)
	}
}

// TestPeekNextClaimableMatchesClaimWithoutMutating proves the new
// peek helper returns the same candidate ClaimNext would pick, but
// without moving the task or stamping assigned_to. A follow-up
// claim must still pick up the same task — peek is read-only.
func TestPeekNextClaimableMatchesClaimWithoutMutating(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-peek", "Peek", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Pick me", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan: %v", err)
	}

	row, ok, err := store.PeekNextClaimable(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("PeekNextClaimable: %v", err)
	}
	if !ok || row.TaskID != task.ID {
		t.Fatalf("PeekNextClaimable = (%+v, %v), want preview of task %d", row, ok, task.ID)
	}
	if row.AssignedTo != "" {
		t.Fatalf("preview assigned_to = %q, want empty (peek must not assign)", row.AssignedTo)
	}

	// Confirm the task row in sqlite still untouched: bucket=first,
	// assigned_to empty. ClaimNext on the same plan still picks it.
	var bucketID int64
	var assignedTo string
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(bucket_id,0), COALESCE(assigned_to,'') FROM tasks WHERE id = ?`, task.ID).Scan(&bucketID, &assignedTo); err != nil {
		t.Fatalf("scan task: %v", err)
	}
	if assignedTo != "" {
		t.Fatalf("post-peek assigned_to = %q, want empty", assignedTo)
	}
	if bucketID != store.snap().Workflow().Buckets[0].ID {
		t.Fatalf("post-peek bucket_id = %d, want first bucket %d", bucketID, store.snap().Workflow().Buckets[0].ID)
	}

	ctx = activity.WithAgent(ctx, "mcp", "plans.claim_next", "claude-opus-4-7", "")
	claimed, ok, err := store.ClaimNextPlanTask(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("ClaimNextPlanTask after peek: %v", err)
	}
	if !ok || claimed.ID != task.ID {
		t.Fatalf("post-peek claim = (%+v, %v), want claim of task %d", claimed, ok, task.ID)
	}

	// Subsequent peek returns (zero, false) — task is now in dev with
	// a non-empty assigned_to, so no first-bucket candidate remains.
	post, ok, err := store.PeekNextClaimable(ctx, project.ID, plan.ID, store.snap())
	if err != nil {
		t.Fatalf("post-claim PeekNextClaimable: %v", err)
	}
	if ok || post.TaskID != 0 {
		t.Fatalf("post-claim peek = (%+v, %v), want empty", post, ok)
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

	t1, err := store.CreateTask(ctx, project.ID, "T1", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask 1: %v", err)
	}
	t2, err := store.CreateTask(ctx, project.ID, "T2", "", domain.Priority(2), "backlog", nil, store.snap())
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
		task, err := store.CreateTask(ctx, project.ID, "Race", "", domain.Priority(2), "backlog", nil, store.snap())
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

	t1, err := store.CreateTask(ctx, project.ID, "T1", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask 1: %v", err)
	}
	t2, err := store.CreateTask(ctx, project.ID, "T2", "", domain.Priority(2), "backlog", nil, store.snap())
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
	bare, err := store.CreateTask(ctx, project.ID, "Bare", "", domain.Priority(2), "backlog", nil, store.snap())
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

// TestMoveTaskClearsAssignedToOnBucketChange pins the SMART/Scope rule
// "tasks.assigned_to is cleared on every bucket transition" — claim
// ownership is scoped to "currently in this bucket". Once the task
// leaves the bucket it was claimed in, the next claim cycle must see a
// clean slot. Emits task.unassigned in the same transaction as
// task.moved so dashboards see both signals atomically.
func TestMoveTaskClearsAssignedToOnBucketChange(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Claimable", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan: %v", err)
	}

	// Claim populates assigned_to without moving the bucket — task stays in backlog.
	ctxClaim := activity.WithAgent(ctx, "mcp", "plans.claim_next", "claude-opus-4-7", "")
	claimed, ok, err := store.ClaimNextPlanTask(ctxClaim, project.ID, plan.ID, store.snap())
	if err != nil || !ok {
		t.Fatalf("ClaimNextPlanTask: ok=%v err=%v", ok, err)
	}

	var assignee sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT assigned_to FROM tasks WHERE id = ?`, claimed.ID).Scan(&assignee); err != nil {
		t.Fatalf("read assigned_to post-claim: %v", err)
	}
	if !assignee.Valid || assignee.String != "claude-opus-4-7" {
		t.Fatalf("post-claim assigned_to = %v, want claude-opus-4-7", assignee)
	}

	// Move backlog → dev (any bucket change). Should clear assigned_to.
	if _, err := store.MoveTask(ctx, project.ID, claimed.ID, "dev", store.snap()); err != nil {
		t.Fatalf("MoveTask backlog → dev: %v", err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT assigned_to FROM tasks WHERE id = ?`, claimed.ID).Scan(&assignee); err != nil {
		t.Fatalf("read assigned_to post-move: %v", err)
	}
	if assignee.Valid && assignee.String != "" {
		t.Fatalf("post-move assigned_to = %q, want NULL/empty", assignee.String)
	}

	// task.unassigned event must have been emitted.
	events, err := store.ListRecentEvents(ctx, domain.EventTypeTaskUnassigned, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no task.unassigned event emitted on bucket change")
	}
}

// TestMaybeFinalizePlanForTaskTransitionsWhenLastTaskCloses pins the
// SMART rule "Plan auto-transitions to done when the last task closes".
// Two-task plan: completing the first task is a no-op; completing the
// second triggers plan.done with status='done' and completed_at stamped.
func TestMaybeFinalizePlanForTaskTransitionsWhenLastTaskCloses(t *testing.T) {
	ctx, store, project := setupPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	t1, err := store.CreateTask(ctx, project.ID, "T1", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask t1: %v", err)
	}
	t2, err := store.CreateTask(ctx, project.ID, "T2", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask t2: %v", err)
	}
	for _, id := range []int64{t1.ID, t2.ID} {
		if err := store.AssignTaskToPlan(ctx, project.ID, id, plan.ID, wave.ID); err != nil {
			t.Fatalf("AssignTaskToPlan %d: %v", id, err)
		}
	}

	final := store.snap().Workflow().FinalBucketKey()

	// Move t1 to final; plan stays active (t2 still pending).
	if _, err := store.MoveTask(ctx, project.ID, t1.ID, final, store.snap()); err != nil {
		t.Fatalf("MoveTask t1 final: %v", err)
	}
	finalized, err := store.MaybeFinalizePlanForTask(ctx, project.ID, t1.ID, store.snap())
	if err != nil {
		t.Fatalf("MaybeFinalizePlanForTask t1: %v", err)
	}
	if finalized {
		t.Fatal("plan should NOT finalise while t2 still pending")
	}

	// Move t2 to final → plan auto-done.
	if _, err := store.MoveTask(ctx, project.ID, t2.ID, final, store.snap()); err != nil {
		t.Fatalf("MoveTask t2 final: %v", err)
	}
	finalized, err = store.MaybeFinalizePlanForTask(ctx, project.ID, t2.ID, store.snap())
	if err != nil {
		t.Fatalf("MaybeFinalizePlanForTask t2: %v", err)
	}
	if !finalized {
		t.Fatal("plan should finalise after last task closed")
	}

	got, err := store.GetPlanByID(ctx, project.ID, plan.ID)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("plan status = %q, want done", got.Status)
	}
	if got.CompletedAt == "" {
		t.Fatal("plan completed_at not stamped")
	}

	events, err := store.ListRecentEvents(ctx, domain.EventTypePlanDone, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents plan.done: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("plan.done events = %d, want 1", len(events))
	}

	// Idempotent: second call is a no-op.
	finalized, err = store.MaybeFinalizePlanForTask(ctx, project.ID, t2.ID, store.snap())
	if err != nil {
		t.Fatalf("MaybeFinalizePlanForTask idempotent: %v", err)
	}
	if finalized {
		t.Fatal("second finalise call should be no-op")
	}
}

// setupTwoBucketPlans installs a workflow whose only two buckets are
// `backlog` (first) and `done` (final). ClaimNextPlanTask resolves
// `dev` to "the bucket immediately above first by position" — with
// just two buckets that resolves to the final bucket, so a claim lands
// directly in the terminal bucket. Used by the landsInFinal-branch
// tests to pin the task.completed + plan auto-done behaviour.
func setupTwoBucketPlans(t *testing.T) (context.Context, *storeFixture, domain.ProjectContext) {
	t.Helper()
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	store.applyBundle(bundleWithKeys(t, "default", []string{"backlog", "done"}, []int{1, 2}))
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	return ctx, store, project.Context()
}

// TestClaimNextPlanTaskNeverCompletesEvenInTwoBucketWorkflow pins the
// new contract: ClaimNext only assigns. Even in a 2-bucket workflow
// (backlog → done) the claim does not move the task into the final
// bucket and therefore never emits task.completed. The bucket
// transition into done remains the responsibility of
// WorkflowService.MoveTask, which honours the preset's guards.
func TestClaimNextPlanTaskNeverCompletesEvenInTwoBucketWorkflow(t *testing.T) {
	ctx, store, project := setupTwoBucketPlans(t)
	plan, err := store.CreatePlan(ctx, project.ID, "plan-a", "Plan A", "")
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	wave, err := store.AddPlanWave(ctx, project.ID, plan.ID, "wave-one", 0)
	if err != nil {
		t.Fatalf("AddPlanWave: %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Direct-terminal", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.AssignTaskToPlan(ctx, project.ID, task.ID, plan.ID, wave.ID); err != nil {
		t.Fatalf("AssignTaskToPlan: %v", err)
	}

	ctxClaim := activity.WithAgent(ctx, "mcp", "plans.claim_next", "claude-opus-4-7", "")
	claimed, ok, err := store.ClaimNextPlanTask(ctxClaim, project.ID, plan.ID, store.snap())
	if err != nil || !ok {
		t.Fatalf("ClaimNextPlanTask: ok=%v err=%v", ok, err)
	}
	if claimed.BucketKey != "backlog" {
		t.Fatalf("claim bucket = %q, want backlog (claim must NOT auto-complete the task)", claimed.BucketKey)
	}

	var completedAt sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT completed_at FROM tasks WHERE id = ?`, claimed.ID).Scan(&completedAt); err != nil {
		t.Fatalf("read completed_at: %v", err)
	}
	if completedAt.Valid && completedAt.String != "" {
		t.Fatalf("completed_at = %q, want unset (claim does not complete)", completedAt.String)
	}

	completedEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskCompleted, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents task.completed: %v", err)
	}
	if len(completedEvents) != 0 {
		t.Fatalf("task.completed events = %d, want 0 (claim no longer transitions to final bucket)", len(completedEvents))
	}
	planDoneEvents, err := store.ListRecentEvents(ctx, domain.EventTypePlanDone, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents plan.done: %v", err)
	}
	if len(planDoneEvents) != 0 {
		t.Fatalf("plan.done events = %d, want 0 (no terminal move on claim path)", len(planDoneEvents))
	}

	gotPlan, err := store.GetPlanByID(ctx, project.ID, plan.ID)
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if gotPlan.Status == "done" {
		t.Fatalf("plan status = %q, want still-active (claim alone must not finalise a plan)", gotPlan.Status)
	}
}

// TestStoreBusyTimeoutTracksConfiguredValue pins the regression that
// ClaimNextPlanTask's per-connection PRAGMA reapply must honour the
// user-configured busy_timeout. The Store captures the resolved value at
// Open time and refreshes it on every ApplyConfig call that supplies a
// positive override. A zero override leaves the existing value intact —
// the canonical value never silently downgrades to the kit default after
// the user has chosen a custom one.
func TestStoreBusyTimeoutTracksConfiguredValue(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/omakiten.db"
	store, err := OpenWithOptions(ctx, path, Options{BusyTimeoutMs: 7777})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if store.busyTimeoutMs != 7777 {
		t.Fatalf("after Open: busyTimeoutMs = %d, want 7777", store.busyTimeoutMs)
	}

	if err := store.ApplyConfig(ctx, ConfigKnobs{BusyTimeoutMs: 9999}); err != nil {
		t.Fatalf("ApplyConfig 9999: %v", err)
	}
	if store.busyTimeoutMs != 9999 {
		t.Fatalf("after ApplyConfig override: busyTimeoutMs = %d, want 9999", store.busyTimeoutMs)
	}

	if err := store.ApplyConfig(ctx, ConfigKnobs{BusyTimeoutMs: 0}); err != nil {
		t.Fatalf("ApplyConfig 0: %v", err)
	}
	if store.busyTimeoutMs != 9999 {
		t.Fatalf("after zero ApplyConfig: busyTimeoutMs = %d, want 9999 preserved", store.busyTimeoutMs)
	}
}

// TestAssignTaskSetsThenClearsAssignee verifies the AssignTask repo
// method handles both directions (set → empty=clear) and emits
// task.assigned / task.unassigned accordingly. Same-value call is a no-op.
func TestAssignTaskSetsThenClearsAssignee(t *testing.T) {
	ctx, store, project := setupPlans(t)
	task, err := store.CreateTask(ctx, project.ID, "Assignable", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Set assignee.
	_, ev, err := store.AssignTask(ctx, project.ID, task.ID, "human-alice", "cli.assign", store.snap())
	if err != nil {
		t.Fatalf("AssignTask set: %v", err)
	}
	if ev.EventType != domain.EventTypeTaskAssigned {
		t.Fatalf("set event = %q, want task.assigned", ev.EventType)
	}

	var assignee sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT assigned_to FROM tasks WHERE id = ?`, task.ID).Scan(&assignee); err != nil {
		t.Fatalf("read assigned_to: %v", err)
	}
	if !assignee.Valid || assignee.String != "human-alice" {
		t.Fatalf("assigned_to = %v, want human-alice", assignee)
	}

	// Same-value call → no-op (zero event).
	_, ev, err = store.AssignTask(ctx, project.ID, task.ID, "human-alice", "cli.assign", store.snap())
	if err != nil {
		t.Fatalf("AssignTask idempotent: %v", err)
	}
	if ev.EventType != "" {
		t.Fatalf("idempotent call emitted event %q, want zero", ev.EventType)
	}

	// Clear via empty assignee.
	_, ev, err = store.AssignTask(ctx, project.ID, task.ID, "", "cli.assign", store.snap())
	if err != nil {
		t.Fatalf("AssignTask clear: %v", err)
	}
	if ev.EventType != domain.EventTypeTaskUnassigned {
		t.Fatalf("clear event = %q, want task.unassigned", ev.EventType)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT assigned_to FROM tasks WHERE id = ?`, task.ID).Scan(&assignee); err != nil {
		t.Fatalf("read assigned_to post-clear: %v", err)
	}
	if assignee.Valid && assignee.String != "" {
		t.Fatalf("post-clear assigned_to = %q, want NULL/empty", assignee.String)
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
