package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
)

func TestEditTaskHappyPath(t *testing.T) {
	f := newAgentFixture(t)

	newTitle := "Updated title"
	newDesc := "Updated description"
	out, err := f.service.EditTask(f.ctx, EditTaskInput{
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
	_, err := f.service.EditTask(f.ctx, EditTaskInput{TaskID: f.taskA1.ID})
	assertCodedError(t, err, domain.ErrValidation)
}

func TestEditTaskRejectsArchived(t *testing.T) {
	f := newAgentFixture(t)
	if _, err := f.service.ArchiveTask(f.ctx, ArchiveTaskInput{TaskID: f.taskA1.ID}); err != nil {
		t.Fatalf("ArchiveTask() error = %v", err)
	}
	title := "Will not stick"
	_, err := f.service.EditTask(f.ctx, EditTaskInput{TaskID: f.taskA1.ID, Title: &title})
	assertCodedError(t, err, domain.ErrValidation)
	failure := FailureFromError(err)
	if !strings.Contains(failure.Message, "archived") {
		t.Fatalf("EditTask(archived).Message = %q, want hint about archived state", failure.Message)
	}
}

func TestEditTaskRejectsUnknownPriorityLabel(t *testing.T) {
	f := newAgentFixture(t)
	bogus := "definitely-not-registered"
	_, err := f.service.EditTask(f.ctx, EditTaskInput{TaskID: f.taskA1.ID, Priority: &bogus})
	assertCodedError(t, err, domain.ErrValidation)
}

// TestEditTaskInLockedBucketReturnsGuardViolation builds a strict-policy
// bundle (workflow.defaults deny edit; backlog overrides allow), moves a
// task to dev, and confirms the agent surface propagates the
// ErrGuardViolation that TaskService.Edit raises when the resolver says
// no. The test exists to pin the contract that the MCP wrapper carries
// no policy of its own — it just relays whatever the service decides.
func TestEditTaskInLockedBucketReturnsGuardViolation(t *testing.T) {
	f := newAgentFixtureEditLockedToBacklog(t)

	// Move backlog → dev (the bundle keeps the standard backlog→dev transition).
	if _, err := f.service.MoveTask(f.ctx, MoveTaskInput{TaskID: f.taskID, BucketKey: "dev"}); err != nil {
		t.Fatalf("MoveTask(dev) error = %v", err)
	}

	title := "Should be denied"
	_, err := f.service.EditTask(f.ctx, EditTaskInput{TaskID: f.taskID, Title: &title})
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
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}}

	// Strict policy: deny task edit/delete at the workflow defaults
	// layer; backlog opts back in via a bucket override so the seed
	// task starts in an editable bucket.
	falseB := false
	trueB := true
	bundle.Workflows[0].Defaults = &config.WorkflowDefaults{
		Task:    &config.EntityPermission{Edit: &falseB, Delete: &falseB},
		Comment: &config.EntityPermission{Edit: &falseB, Delete: &falseB},
	}
	for i := range bundle.Workflows[0].Buckets {
		if bundle.Workflows[0].Buckets[i].Key == "backlog" {
			bundle.Workflows[0].Buckets[i].Permissions = &config.BucketPermissions{
				Task: &config.EntityPermission{Edit: &trueB, Delete: &trueB},
			}
			break
		}
	}

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "omakiten.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
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
	task, err := store.CreateTask(ctx, project.ID, "Seed", "Seed task", domain.Priority(2), "backlog")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	svc := NewService(store, ProjectSelector{CWD: root})
	svc.SetSettings(ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    true,
		CachePrompts:       true,
		RecentContextLimit: 3,
		NextWorkLimit:      5,
		SimilarTaskLimit:   5,
	})

	return editLockedFixture{ctx: ctx, service: svc, taskID: task.ID}
}
