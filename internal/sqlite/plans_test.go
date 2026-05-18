package sqlite

import (
	"context"
	"errors"
	"testing"

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
