package operation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)

// TestCreateTaskIntentRejectsOverCapBeforeSimilarity pins audit #65782: an
// over-cap title/description submitted to tasks.create_intent must be rejected
// with ErrValidation BEFORE the similarity scan runs — not after loading the
// whole task list and tokenizing the oversized text. The fixture seeds
// "Add agent agent integration" (taskA1), so a matching title on the similarity
// path would otherwise return a RequiresConfirmation response; getting
// ErrValidation instead proves the cap short-circuits ahead of that work.
func TestCreateTaskIntentRejectsOverCapBeforeSimilarity(t *testing.T) {
	t.Run("over-cap description rejected ahead of similarity", func(t *testing.T) {
		f := newAgentFixture(t)
		resp, err := f.service.CreateTaskIntent(f.ctx, contract.CreateTaskInput{
			Title:       "Add agent agent integration", // matches taskA1 → would trigger similarity
			Description: strings.Repeat("d", domain.MaxTaskDescriptionBytes+1),
		})
		assertCodedError(t, err, domain.ErrValidation)
		if resp.Confirmation.RequiresConfirmation {
			t.Fatal("over-cap intent returned a similarity confirmation; cap must reject before the scan")
		}
		if len(resp.SimilarTasks) != 0 {
			t.Fatalf("over-cap intent surfaced %d similar tasks; similarity ran before the cap", len(resp.SimilarTasks))
		}
	})

	t.Run("over-cap title rejected ahead of similarity", func(t *testing.T) {
		f := newAgentFixture(t)
		_, err := f.service.CreateTaskIntent(f.ctx, contract.CreateTaskInput{
			Title: strings.Repeat("t", domain.MaxTaskTitleRunes+1),
		})
		assertCodedError(t, err, domain.ErrValidation)
	})
}

func TestEditTaskHappyPath(t *testing.T) {
	f := newAgentFixture(t)

	newTitle := "Updated title"
	newDesc := "Updated description"
	out, err := f.service.EditTask(f.ctx, contract.EditTaskInput{
		TaskID:      f.taskA1.ID,
		Title:       &newTitle,
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("EditTask() error = %v", err)
	}
	if out.Task.Title != newTitle {
		t.Fatalf("EditTask().Task.Title = %q, want %q", out.Task.Title, newTitle)
	}
	if out.Task.Description != newDesc {
		t.Fatalf("EditTask().Task.Description = %q, want %q", out.Task.Description, newDesc)
	}
	if out.Project.ID != f.projectA.ID {
		t.Fatalf("EditTask().Project.ID = %d, want %d", out.Project.ID, f.projectA.ID)
	}
}

func TestEditTaskRequiresAtLeastOneField(t *testing.T) {
	f := newAgentFixture(t)
	_, err := f.service.EditTask(f.ctx, contract.EditTaskInput{TaskID: f.taskA1.ID})
	assertCodedError(t, err, domain.ErrValidation)
}

func TestEditTaskRejectsArchived(t *testing.T) {
	f := newAgentFixture(t)
	if _, err := f.service.ArchiveTask(f.ctx, contract.ArchiveTaskInput{TaskID: f.taskA1.ID}); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	title := "Will not stick"
	_, err := f.service.EditTask(f.ctx, contract.EditTaskInput{TaskID: f.taskA1.ID, Title: &title})
	assertCodedError(t, err, domain.ErrValidation)
	failure := FailureFromError(err)
	if !strings.Contains(failure.Message, "archived") {
		t.Fatalf("EditTask(archived).Message = %q, want hint about archived state", failure.Message)
	}
}

func TestEditTaskRejectsUnknownPriorityLabel(t *testing.T) {
	f := newAgentFixture(t)
	bogus := "definitely-not-registered"
	_, err := f.service.EditTask(f.ctx, contract.EditTaskInput{TaskID: f.taskA1.ID, Priority: &bogus})
	assertCodedError(t, err, domain.ErrValidation)
}

// TestEditTaskInLockedBucketReturnsGuardViolation builds a strict-policy
// bundle (workflow.defaults deny edit; backlog overrides allow), moves a
// task to dev, and confirms the agent surface propagates the
// ErrGuardViolation that TaskService.Edit raises when the resolver says
// no. The test exists to pin the contract that the agent wrapper carries
// no policy of its own — it just relays whatever the service decides.
func TestEditTaskInLockedBucketReturnsGuardViolation(t *testing.T) {
	f := newAgentFixtureEditLockedToBacklog(t)

	// Move backlog → dev (the bundle keeps the standard backlog→dev transition).
	if _, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.taskID, BucketKey: "dev"}); err != nil {
		t.Fatalf("MoveTask(dev) error = %v", err)
	}

	title := "Should be denied"
	_, err := f.service.EditTask(f.ctx, contract.EditTaskInput{TaskID: f.taskID, Title: &title})
	assertCodedError(t, err, domain.ErrGuardViolation)
	failure := FailureFromError(err)
	if failure.Message == "" {
		t.Fatalf("EditTask(locked bucket).Message empty; want resolver hint")
	}
}

// editLockedFixture is the trimmed fixture used by the policy-violation
// test. It seeds a single project + task in backlog with a strict-policy
// bundle so we don't carry the full agentFixture surface for one assertion.
type editLockedFixture struct {
	ctx     context.Context
	service *Service
	taskID  int64
}

func newAgentFixtureEditLockedToBacklog(t *testing.T) editLockedFixture {
	t.Helper()
	ctx := context.Background()

	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", SkillRepertoire: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}}

	// Strict policy: deny task edit/delete at the workflow defaults
	// layer; backlog opts back in via a bucket override so the seed
	// task starts in an editable bucket.
	falseB := false
	trueB := true
	deny := &config.CommentOpPolicy{Allow: &falseB}
	allow := &config.CommentOpPolicy{Allow: &trueB}
	bundle.Workflows[0].Defaults = &config.WorkflowDefaults{
		Task:    &config.EntityPermission{Edit: deny, Delete: deny},
		Comment: &config.EntityPermission{Edit: deny, Delete: deny},
	}
	for i := range bundle.Workflows[0].Buckets {
		if bundle.Workflows[0].Buckets[i].Key == "backlog" {
			bundle.Workflows[0].Buckets[i].Permissions = &config.BucketPermissions{
				Task: &config.EntityPermission{Edit: allow, Delete: allow},
			}
			break
		}
	}

	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}

	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Locked", "locked", root)
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Seed", "Seed task", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	svc := NewService(store, contract.ProjectSelector{CWD: root})
	svc.SetSnapshot(store.Snapshot())
	svc.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    true,
		NextWorkLimit:      5,
		SimilarTaskLimit:   5,
	})

	return editLockedFixture{ctx: ctx, service: svc, taskID: task.ID}
}

// TestContinueTaskShowsSubtaskKitWorkflow verifies a sub-task's continue
// response carries the workflow of the kit that governs it, not the root's.
func TestContinueTaskShowsSubtaskKitWorkflow(t *testing.T) {
	ctx := context.Background()
	bundle := agentTestBundle(t)
	bundle.SubtaskKit = "sub.yaml"
	bundle.SubtaskBundle = &config.Bundle{
		Kit: config.Kit{ID: 200, Key: "sub", Name: "Sub"},
		Workflows: []config.Workflow{{
			ID:   200,
			Key:  "sub",
			Name: "Sub",
			Buckets: []config.Bucket{
				{ID: 901, Key: "todo", Name: "Todo", Position: 1},
				{ID: 902, Key: "finished", Name: "Finished", Position: 2},
			},
			Transitions: []config.Transition{{From: 901, To: 902}},
		}},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
	}
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Kits", "kits", root)
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	snap := store.Snapshot()
	parent, err := store.CreateTask(ctx, project.ID, "Parent", "Root task", domain.Priority(2), "backlog", nil, snap)
	if err != nil {
		t.Fatalf("CreateTask(parent) error = %v", err)
	}
	subSnap, _ := snap.SubtaskKit()
	child, err := store.CreateTask(ctx, project.ID, "Child", "Sub-task", domain.Priority(2), "todo", &parent.ID, subSnap)
	if err != nil {
		t.Fatalf("CreateTask(child) error = %v", err)
	}
	svc := NewService(store, contract.ProjectSelector{CWD: root})
	svc.SetSnapshot(snap)
	svc.SetSettings(ServiceSettings{RecentCommentLimit: 5, IncludeWorkflow: true, NextWorkLimit: 5, SimilarTaskLimit: 5})

	for _, tc := range []struct {
		task domain.Task
		want string
	}{{parent, snap.Workflow().Key}, {child, "sub"}} {
		resp, err := svc.ContinueTask(ctx, contract.ContinueTaskInput{TaskID: tc.task.ID})
		if err != nil {
			t.Fatalf("ContinueTask(%d) error = %v", tc.task.ID, err)
		}
		if resp.Workflow.Key != tc.want {
			t.Fatalf("ContinueTask(%d).Workflow.Key = %q, want %q", tc.task.ID, resp.Workflow.Key, tc.want)
		}
	}
}

type guardedTaskFixture struct {
	ctx     context.Context
	store   *snapstore.Store
	service *Service
	project domain.Project
	task    domain.Task
	fix     string
}

// newGuardedTaskFixture seeds one backlog task whose backlog→dev transition
// requires a comment tagged self-branch.
func newGuardedTaskFixture(t *testing.T) guardedTaskFixture {
	t.Helper()
	ctx := context.Background()
	bundle := agentTestBundle(t)
	bundle.Workflows[0].Transitions[0].Guards = []config.TransitionGuard{{Type: "comments_tagged", Tag: "self-branch", Count: 1}}
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Guards", "guards", root)
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Guarded", "Needs evidence", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	svc := NewService(store, contract.ProjectSelector{CWD: root})
	svc.SetSnapshot(store.Snapshot())
	svc.SetSettings(ServiceSettings{RecentCommentLimit: 5, IncludeWorkflow: true, NextWorkLimit: 5, SimilarTaskLimit: 5})
	return guardedTaskFixture{ctx: ctx, store: store, service: svc, project: project, task: task,
		fix: fmt.Sprintf("okt comment add %d -T self-branch -b '<evidence>'", task.ID)}
}

// TestMoveDryRunReportsGuardsWithoutRecording verifies a dry-run move reports
// the unmet guard with its fix, leaves the task in place, and records no
// guard.violated event.
func TestMoveDryRunReportsGuardsWithoutRecording(t *testing.T) {
	f := newGuardedTaskFixture(t)
	moved, err := f.service.MoveTask(f.ctx, contract.MoveTaskInput{TaskID: f.task.ID, BucketKey: "dev", DryRun: true})
	if err != nil {
		t.Fatalf("MoveTask(dry run) error = %v", err)
	}
	if !moved.DryRun || moved.Ready == nil || *moved.Ready || moved.Task.BucketKey != "backlog" {
		t.Fatalf("dry run = %+v, want unchanged backlog task and ready=false", moved)
	}
	if len(moved.Guards) != 1 || moved.Guards[0].Satisfied || moved.Guards[0].Fix != f.fix {
		t.Fatalf("dry run guards = %+v, want one unmet guard with fix %q", moved.Guards, f.fix)
	}
	events, err := f.store.ListEvents(f.ctx, domain.EventFilter{ProjectID: f.project.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.EventType == domain.EventTypeGuardViolated {
			t.Fatalf("dry run recorded guard.violated: %+v", event)
		}
	}
}

// TestContinueTaskReportsOutgoingGuards verifies the continue workflow block
// carries guard state and readiness only on transitions leaving the task's
// current bucket.
func TestContinueTaskReportsOutgoingGuards(t *testing.T) {
	f := newGuardedTaskFixture(t)
	resp, err := f.service.ContinueTask(f.ctx, contract.ContinueTaskInput{TaskID: f.task.ID})
	if err != nil {
		t.Fatalf("ContinueTask() error = %v", err)
	}
	var outgoing []contract.TransitionSummary
	for _, transition := range resp.Workflow.Transitions {
		if transition.From == "backlog" {
			outgoing = append(outgoing, transition)
			continue
		}
		if transition.Ready != nil || transition.Guards != nil {
			t.Fatalf("transition %s→%s carries guard state, want only outgoing ones", transition.From, transition.To)
		}
	}
	if len(outgoing) != 1 || outgoing[0].Ready == nil || *outgoing[0].Ready || len(outgoing[0].Guards) != 1 || outgoing[0].Guards[0].Fix != f.fix {
		t.Fatalf("outgoing transitions = %+v, want one with ready=false and the unmet guard", outgoing)
	}
}
