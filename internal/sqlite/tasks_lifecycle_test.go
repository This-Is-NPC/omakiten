package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func setupLifecycle(t *testing.T) (context.Context, *storeFixture, domain.ProjectContext) {
	t.Helper()
	ctx := context.Background()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	bundle, _ := testfixtures.LoadBundle(t, "lifecycle_policy.yaml")
	store.applyBundle(bundle)
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	return ctx, store, project.Context()
}

func TestTaskFilterIncludeArchived(t *testing.T) {
	ctx, store, project := setupLifecycle(t)

	keep, err := store.CreateTask(ctx, project.ID, "Active", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(active) = %v", err)
	}
	if keep.State != domain.TaskStateActive {
		t.Fatalf("default state = %q, want active", keep.State)
	}
	archived, err := store.CreateTask(ctx, project.ID, "Archive me", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask(archive) = %v", err)
	}
	if _, _, err := store.SetTaskState(ctx, project.ID, archived.ID, domain.TaskStateArchived, "done", store.snap()); err != nil {
		t.Fatalf("SetTaskState() = %v", err)
	}

	listed, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{}, store.snap())
	if err != nil {
		t.Fatalf("ListTasks() = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != keep.ID {
		t.Fatalf("default filter included archived: %+v", listed)
	}

	all, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{IncludeArchived: true}, store.snap())
	if err != nil {
		t.Fatalf("ListTasks(IncludeArchived) = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("IncludeArchived listed %d tasks, want 2", len(all))
	}
}

func TestArchiveBypassesTransitionGuardsButHonorsOperationGuards(t *testing.T) {
	ctx, store, project := setupLifecycle(t)
	tasks := app.NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.snap())

	task, err := store.CreateTask(ctx, project.ID, "Frozen", "", domain.Priority(2), "backlog", store.snap())
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
	tasks := app.NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.snap())

	task, err := store.CreateTask(ctx, project.ID, "Doomed", "", domain.Priority(2), "backlog", store.snap())
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
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev", store.snap()); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "done", store.snap()); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}

	// Add a comment + dependency to verify cascade.
	other, err := store.CreateTask(ctx, project.ID, "Other", "", domain.Priority(2), "backlog", store.snap())
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

func TestCompletedAtTracksFinalBucketTransitions(t *testing.T) {
	ctx, store, project := setupLifecycle(t)

	task, err := store.CreateTask(ctx, project.ID, "Track me", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	readCompletedAt := func() sql.NullString {
		t.Helper()
		var got sql.NullString
		row := store.db.QueryRowContext(ctx, "SELECT completed_at FROM tasks WHERE project_id = ? AND id = ?", project.ID, task.ID)
		if err := row.Scan(&got); err != nil {
			t.Fatalf("scan completed_at: %v", err)
		}
		return got
	}

	if got := readCompletedAt(); got.Valid {
		t.Fatalf("fresh backlog task completed_at = %q, want NULL", got.String)
	}

	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev", store.snap()); err != nil {
		t.Fatalf("MoveTask(dev) = %v", err)
	}
	if got := readCompletedAt(); got.Valid {
		t.Fatalf("dev completed_at = %q, want NULL", got.String)
	}

	if _, err := store.MoveTask(ctx, project.ID, task.ID, "done", store.snap()); err != nil {
		t.Fatalf("MoveTask(done) = %v", err)
	}
	first := readCompletedAt()
	if !first.Valid || first.String == "" {
		t.Fatalf("done completed_at = %+v, want non-NULL", first)
	}

	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev", store.snap()); err != nil {
		t.Fatalf("MoveTask(dev back) = %v", err)
	}
	if got := readCompletedAt(); got.Valid {
		t.Fatalf("re-opened dev completed_at = %q, want NULL", got.String)
	}

	// Sleep so the next CURRENT_TIMESTAMP can differ from `first` (SQLite
	// resolution is 1s). Without this, "new completion overwrites" is
	// indistinguishable from "old timestamp preserved" in fast tests.
	time.Sleep(1100 * time.Millisecond)

	if _, err := store.MoveTask(ctx, project.ID, task.ID, "done", store.snap()); err != nil {
		t.Fatalf("MoveTask(done again) = %v", err)
	}
	second := readCompletedAt()
	if !second.Valid {
		t.Fatalf("re-completed completed_at = NULL, want non-NULL")
	}
	if second.String <= first.String {
		t.Fatalf("re-completed completed_at = %q, want strictly after first %q", second.String, first.String)
	}
}

func TestCompletedAtSetOnArchiveIntoFinalBucket(t *testing.T) {
	ctx, store, project := setupLifecycle(t)

	task, err := store.CreateTask(ctx, project.ID, "Archive me", "", domain.Priority(2), "backlog", store.snap())
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	if _, _, err := store.SetTaskState(ctx, project.ID, task.ID, domain.TaskStateArchived, "done", store.snap()); err != nil {
		t.Fatalf("SetTaskState archive = %v", err)
	}

	var completedAt sql.NullString
	row := store.db.QueryRowContext(ctx, "SELECT completed_at FROM tasks WHERE project_id = ? AND id = ?", project.ID, task.ID)
	if err := row.Scan(&completedAt); err != nil {
		t.Fatalf("scan completed_at: %v", err)
	}
	if !completedAt.Valid || completedAt.String == "" {
		t.Fatalf("archived-into-done completed_at = %+v, want non-NULL", completedAt)
	}
}

func TestEditPolicyAndCommentInheritance(t *testing.T) {
	ctx, store, project := setupLifecycle(t)
	tasks := app.NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.snap())
	workflow := app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.snap())
	comments := app.NewCommentServiceWithWorkflow(store, workflow, store.snap())

	task, err := store.CreateTask(ctx, project.ID, "Title", "", domain.Priority(2), "backlog", store.snap())
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
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev", store.snap()); err != nil {
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
