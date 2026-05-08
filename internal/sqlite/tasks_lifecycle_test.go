package sqlite

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func boolPtr(b bool) *bool { return &b }

// lifecycleBundle wires permissions and operations to exercise:
//   - task.edit allowed in backlog, denied in dev, denied in done
//   - task.delete allowed in done only
//   - operations.delete.guards requires #justification
//   - operations.archive.guards requires #archive-reason
//   - comment policy partially overrides task policy in `dev`
func lifecycleBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1, Permissions: &config.BucketPermissions{
					Task: &config.EntityPermission{Edit: boolPtr(true), Delete: boolPtr(false)},
				}},
				{ID: 2, Key: "dev", Name: "Development", Position: 2, Permissions: &config.BucketPermissions{
					Task: &config.EntityPermission{Edit: boolPtr(false), Delete: boolPtr(false)},
					// Comments inherit edit=false but override delete=true.
					Comment: &config.EntityPermission{Delete: boolPtr(true)},
				}},
				{ID: 3, Key: "done", Name: "Done", Position: 3, Permissions: &config.BucketPermissions{
					Task: &config.EntityPermission{Edit: boolPtr(false), Delete: boolPtr(true)},
				}},
			},
			Transitions: []config.Transition{
				{From: 1, To: 2},
				{From: 2, To: 3},
				{From: 3, To: 2},
			},
			Operations: config.WorkflowOperations{
				Archive: config.OperationPolicy{Guards: []config.TransitionGuard{{
					Type: "comments_tagged", Tag: "archive-reason", Count: 1, Hint: "tag a #archive-reason comment first",
				}}},
				Delete: config.OperationPolicy{Guards: []config.TransitionGuard{{
					Type: "comments_tagged", Tag: "justification", Count: 1, Hint: "tag a #justification comment first",
				}}},
			},
		}},
	}
}

func setupLifecycle(t *testing.T) (context.Context, *Store, domain.ProjectContext) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ImportBundle(ctx, lifecycleBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	return ctx, store, project.Context()
}

func TestTaskFilterIncludeArchived(t *testing.T) {
	ctx, store, project := setupLifecycle(t)

	keep, err := store.CreateTask(ctx, project.ID, "Active", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(active) = %v", err)
	}
	if keep.State != domain.TaskStateActive {
		t.Fatalf("default state = %q, want active", keep.State)
	}
	archived, err := store.CreateTask(ctx, project.ID, "Archive me", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(archive) = %v", err)
	}
	if _, _, err := store.SetTaskState(ctx, project.ID, archived.ID, domain.TaskStateArchived, "done"); err != nil {
		t.Fatalf("SetTaskState() = %v", err)
	}

	listed, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks() = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != keep.ID {
		t.Fatalf("default filter included archived: %+v", listed)
	}

	all, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListTasks(IncludeArchived) = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("IncludeArchived listed %d tasks, want 2", len(all))
	}
}

func TestArchiveBypassesTransitionGuardsButHonorsOperationGuards(t *testing.T) {
	ctx, store, project := setupLifecycle(t)
	tasks := app.NewTaskServiceFromStore(store)

	task, err := store.CreateTask(ctx, project.ID, "Frozen", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	// Archive without the required guard tag should fail.
	if _, _, err := tasks.Archive(ctx, project, task.ID); err == nil {
		t.Fatalf("Archive without guard succeeded; expected guard violation")
	}

	if _, err := store.AddComment(ctx, project.ID, task.ID, "needs to wait", "agent", []domain.Tag{{Name: "archive-reason", Label: "archive-reason"}}); err != nil {
		t.Fatalf("AddComment = %v", err)
	}

	got, _, err := tasks.Archive(ctx, project, task.ID)
	if err != nil {
		t.Fatalf("Archive after tag = %v", err)
	}
	if got.State != domain.TaskStateArchived {
		t.Fatalf("Archive state = %q, want archived", got.State)
	}
	if got.BucketKey != "done" {
		t.Fatalf("Archive bucket = %q, want done (final)", got.BucketKey)
	}

	// MoveTask must reject archived rows even though backlog→done is no
	// stranger to transition guards (we never get there because state check
	// fires first).
	if _, err := tasks.Move(ctx, project, task.ID, "dev"); err == nil {
		t.Fatalf("Move on archived succeeded; expected validation error")
	}

	// Unarchive flips state back; bucket stays in `done`.
	restored, _, err := tasks.Unarchive(ctx, project, task.ID)
	if err != nil {
		t.Fatalf("Unarchive = %v", err)
	}
	if restored.State != domain.TaskStateActive {
		t.Fatalf("Unarchive state = %q, want active", restored.State)
	}
	if restored.BucketKey != "done" {
		t.Fatalf("Unarchive bucket = %q, want done", restored.BucketKey)
	}
}

func TestDeleteEnforcesPolicyAndCascades(t *testing.T) {
	ctx, store, project := setupLifecycle(t)
	tasks := app.NewTaskServiceFromStore(store)

	task, err := store.CreateTask(ctx, project.ID, "Doomed", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	// Default bucket=backlog disallows delete (task.delete=false).
	if _, err := tasks.Delete(ctx, project, task.ID); err == nil {
		t.Fatalf("Delete in backlog succeeded; expected policy violation")
	} else {
		var coded *domain.CodedError
		if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
			t.Fatalf("Delete error = %v, want guard_violation", err)
		}
	}

	// Move to done so policy permits delete.
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "done"); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}

	// Add a comment + dependency to verify cascade.
	other, err := store.CreateTask(ctx, project.ID, "Other", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask(other) = %v", err)
	}
	if _, err := store.AddTaskDependency(ctx, project.ID, other.ID, task.ID); err != nil {
		t.Fatalf("AddTaskDependency = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "context", "agent", []domain.Tag{{Name: "general", Label: "general"}}); err != nil {
		t.Fatalf("AddComment = %v", err)
	}

	// Still missing operations.delete.guards #justification.
	if _, err := tasks.Delete(ctx, project, task.ID); err == nil {
		t.Fatalf("Delete missing #justification succeeded; expected guard violation")
	}

	if _, err := store.AddComment(ctx, project.ID, task.ID, "won't fit", "agent", []domain.Tag{{Name: "justification", Label: "justification"}}); err != nil {
		t.Fatalf("AddComment(justification) = %v", err)
	}

	event, err := tasks.Delete(ctx, project, task.ID)
	if err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if event.EventType != domain.EventTypeTaskRemoved {
		t.Fatalf("Delete event type = %q, want task.removed", event.EventType)
	}

	// Cascade: comments and dependencies for the deleted task are gone.
	comments, err := store.ListComments(ctx, project.ID, 0)
	if err != nil {
		t.Fatalf("ListComments = %v", err)
	}
	for _, c := range comments {
		if c.TaskID == task.ID {
			t.Fatalf("comment for deleted task survived: %+v", c)
		}
	}
	deps, err := store.ListTaskDependencies(ctx, project.ID, other.ID)
	if err != nil {
		t.Fatalf("ListTaskDependencies = %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("dependency for deleted task survived: %+v", deps)
	}
}

func TestEditPolicyAndCommentInheritance(t *testing.T) {
	ctx, store, project := setupLifecycle(t)
	tasks := app.NewTaskServiceFromStore(store)
	workflow := app.NewWorkflowServiceFromStore(store)
	comments := app.NewCommentServiceWithWorkflow(store, workflow)

	task, err := store.CreateTask(ctx, project.ID, "Title", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	// Backlog allows task.edit + comment.edit (inherited).
	newTitle := "Renamed"
	if _, err := tasks.Edit(ctx, project, task.ID, domain.TaskUpdate{Title: &newTitle}); err != nil {
		t.Fatalf("Edit(backlog) = %v", err)
	}

	c, err := store.AddComment(ctx, project.ID, task.ID, "first", "agent", nil)
	if err != nil {
		t.Fatalf("AddComment = %v", err)
	}

	// Move to dev: task.edit=false, comment inherits but overrides delete=true.
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}

	otherTitle := "Blocked"
	if _, err := tasks.Edit(ctx, project, task.ID, domain.TaskUpdate{Title: &otherTitle}); err == nil {
		t.Fatalf("Edit(dev) succeeded; expected policy violation")
	}

	// Comment edit should be blocked (inherited edit=false).
	if _, err := comments.Edit(ctx, project, c.ID, "updated", nil); err == nil {
		t.Fatalf("Comment.Edit(dev) succeeded; expected policy violation")
	}

	// But comment delete IS allowed in dev (override).
	if _, err := comments.Remove(ctx, project, c.ID); err != nil {
		t.Fatalf("Comment.Remove(dev) = %v", err)
	}
}
