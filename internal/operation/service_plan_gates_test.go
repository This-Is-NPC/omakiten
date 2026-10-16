package operation

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/snapstore"
)

type planGateFixture struct {
	ctx     context.Context
	service *Service
	tasks   []int64
}

// newPlanGateFixture opens a workflow backlog→dev→done whose plans block
// gates start on #kickoff, passing 50% on #midpoint, and completion on
// #retro, with four tasks of plan "p" in backlog.
func newPlanGateFixture(t *testing.T) planGateFixture {
	t.Helper()
	ctx := context.Background()
	bundle := agentTestBundle(t)
	gate := func(tag string) []config.TransitionGuard {
		return []config.TransitionGuard{{Type: domain.GuardPlanCommentsTagged, Tag: tag}}
	}
	workflow := &bundle.Workflows[0]
	workflow.Transitions = []config.Transition{{From: 1, To: 2}, {From: 2, To: 3}}
	workflow.Plans = config.PlanGates{
		Start:    config.OperationPolicy{Guards: gate("kickoff")},
		Progress: []config.PlanProgressGate{{Percent: 50, Guards: gate("midpoint")}},
		Complete: config.OperationPolicy{Guards: gate("retro")},
	}
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	root := t.TempDir()
	if _, err := store.UpsertProject(ctx, "Gates", "gates", root); err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	svc := NewService(store, contract.ProjectSelector{CWD: root})
	svc.SetSnapshot(store.Snapshot())
	f := planGateFixture{ctx: ctx, service: svc}

	plan := f.createPlan(t, "p")
	wave, err := svc.AddPlanWave(ctx, contract.AddPlanWaveInput{PlanID: plan, Name: "one"})
	if err != nil {
		t.Fatalf("AddPlanWave() error = %v", err)
	}
	for _, title := range []string{"Gate one", "Gate two", "Gate three", "Gate four"} {
		id := f.createTask(t, title)
		if _, err := svc.AssignPlanTask(ctx, contract.AssignPlanTaskInput{TaskID: id, Slug: "p", WaveID: wave.Wave.ID}); err != nil {
			t.Fatalf("AssignPlanTask() error = %v", err)
		}
		f.tasks = append(f.tasks, id)
	}
	return f
}

func (f planGateFixture) createPlan(t *testing.T, slug string) int64 {
	t.Helper()
	plan, err := f.service.CreatePlan(f.ctx, contract.CreatePlanInput{Slug: slug, Name: slug})
	if err != nil {
		t.Fatalf("CreatePlan(%s) error = %v", slug, err)
	}
	return plan.Plan.ID
}

func (f planGateFixture) createTask(t *testing.T, title string) int64 {
	t.Helper()
	task, err := f.service.CreateTask(f.ctx, contract.CreateTaskInput{Title: title, Description: title + " for the plan gates", SkipSimilarityCheck: true})
	if err != nil {
		t.Fatalf("CreateTask(%s) error = %v", title, err)
	}
	return task.Task.ID
}

func (f planGateFixture) move(taskID int64, bucket string) error {
	_, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: taskID, BucketKey: bucket})
	return err
}

func (f planGateFixture) comment(t *testing.T, tag string) {
	t.Helper()
	if _, err := f.service.AddComment(f.ctx, contract.AddCommentInput{Scope: domain.CommentScopePlan, PlanSlug: "p", Body: tag + " evidence", Tags: []string{tag}}); err != nil {
		t.Fatalf("AddComment(%s) error = %v", tag, err)
	}
}

// assertPlanGate checks err is the guard violation of the plan gate at stage
// waiting on a comment tagged tag, with the fix that adds it.
func assertPlanGate(t *testing.T, err error, stage domain.PlanStage, tag string) {
	t.Helper()
	assertCodedError(t, err, domain.ErrGuardViolation)
	var coded *domain.CodedError
	errors.As(err, &coded)
	if coded.Details["guard_type"] != domain.GuardPlanCommentsTagged || coded.Details["stage"] != string(stage) || coded.Details["tag"] != tag {
		t.Fatalf("violation details = %v, want %s at %s on #%s", coded.Details, domain.GuardPlanCommentsTagged, stage, tag)
	}
	if fix, _ := coded.Details["fix"].(string); !strings.HasPrefix(fix, "okt comment add --scope plan --plan p ") || !strings.Contains(fix, " -T "+tag+" ") {
		t.Fatalf("fix = %q, want a plan comment tagged %s", fix, tag)
	}
}

func TestPlanGatesHoldStartUntilKickoff(t *testing.T) {
	f := newPlanGateFixture(t)
	claimCtx := activity.WithAgent(f.ctx, "cli", "plans.claim_next", "claude-test", "")

	_, err := f.service.ClaimNextPlanTask(claimCtx, contract.ClaimNextPlanTaskInput{Slug: "p"})
	assertPlanGate(t, err, domain.PlanStageStart, "kickoff")
	assertPlanGate(t, f.move(f.tasks[0], "dev"), domain.PlanStageStart, "kickoff")
	preview, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.tasks[0], BucketKey: "dev", DryRun: true})
	if err != nil || preview.Ready == nil || *preview.Ready {
		t.Fatalf("dry run = %+v, %v; want not ready", preview, err)
	}

	f.comment(t, "kickoff")
	if claim, err := f.service.ClaimNextPlanTask(claimCtx, contract.ClaimNextPlanTaskInput{Slug: "p"}); err != nil || !claim.Claimed {
		t.Fatalf("claim after kickoff = %+v, %v; want claimed", claim, err)
	}
	if err := f.move(f.tasks[0], "dev"); err != nil {
		t.Fatalf("move after kickoff error = %v", err)
	}
}

func TestPlanGatesHoldProgressAndCompletion(t *testing.T) {
	f := newPlanGateFixture(t)
	f.comment(t, "kickoff")
	for _, id := range f.tasks {
		if err := f.move(id, "dev"); err != nil {
			t.Fatalf("move %d to dev error = %v", id, err)
		}
	}
	// 1 and 2 of 4 done reach 50% without passing it.
	for _, id := range f.tasks[:2] {
		if err := f.move(id, "done"); err != nil {
			t.Fatalf("move %d to done error = %v", id, err)
		}
	}
	assertPlanGate(t, f.move(f.tasks[2], "done"), domain.PlanStageProgress, "midpoint")
	f.comment(t, "midpoint")
	if err := f.move(f.tasks[2], "done"); err != nil {
		t.Fatalf("move past 50%% after midpoint error = %v", err)
	}

	assertPlanGate(t, f.move(f.tasks[3], "done"), domain.PlanStageComplete, "retro")
	done := string(domain.PlanStatusDone)
	_, err := f.service.EditPlan(f.ctx, contract.EditPlanInput{Slug: "p", Status: &done})
	assertPlanGate(t, err, domain.PlanStageComplete, "retro")

	f.comment(t, "retro")
	if err := f.move(f.tasks[3], "done"); err != nil {
		t.Fatalf("last move after retro error = %v", err)
	}
	shown, err := f.service.ShowPlan(f.ctx, contract.ShowPlanInput{Slug: "p"})
	if err != nil || shown.Plan.Status != done {
		t.Fatalf("plan after last move = %+v, %v; want done", shown.Plan, err)
	}
}

func TestPlanGatesSkipAbandonedPlansAndLooseTasks(t *testing.T) {
	f := newPlanGateFixture(t)
	abandoned := string(domain.PlanStatusAbandoned)
	if _, err := f.service.EditPlan(f.ctx, contract.EditPlanInput{Slug: "p", Status: &abandoned}); err != nil {
		t.Fatalf("abandon error = %v", err)
	}
	if err := f.move(f.tasks[0], "dev"); err != nil {
		t.Fatalf("move in an abandoned plan error = %v", err)
	}
	loose := f.createTask(t, "Loose task")
	if err := f.move(loose, "dev"); err != nil {
		t.Fatalf("move outside a plan error = %v", err)
	}
}
