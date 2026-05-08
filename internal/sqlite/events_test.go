package sqlite

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// fullTransitionBundle wires every consecutive transition (1->2->3) so
// MoveTask tests can walk a task all the way to the final bucket without
// hitting workflow_invalid_transition along the way.
func fullTransitionBundle(t *testing.T) config.Bundle {
	t.Helper()
	b := sqliteTestBundle(t)
	b.Workflows[0].Transitions = []config.Transition{
		{From: 1, To: 2},
		{From: 2, To: 3},
	}
	return b
}

func openStoreWithFullTransitions(ctx context.Context, t *testing.T) (*Store, domain.Project) {
	t.Helper()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ImportBundle(ctx, fullTransitionBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Test", "test", t.TempDir())
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	return store, project
}

func TestCreateTaskEmitsTaskCreatedEvent(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	task, err := store.CreateTask(ctx, project.ID, "first", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	events, err := store.ListTaskActivity(ctx, project.ID, task.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	got := events[0]
	if got.EventType != domain.EventTypeTaskCreated {
		t.Errorf("event_type = %q, want %q", got.EventType, domain.EventTypeTaskCreated)
	}
	if got.EntityType != domain.EventEntityTask {
		t.Errorf("entity_type = %q, want %q", got.EntityType, domain.EventEntityTask)
	}
	if got.EntityID != task.ID {
		t.Errorf("entity_id = %d, want %d", got.EntityID, task.ID)
	}
	if !strings.Contains(got.Payload, "backlog") {
		t.Errorf("payload = %q, want substring %q", got.Payload, "backlog")
	}
}

func TestMoveTaskEmitsTaskMovedAndCompleted(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithFullTransitions(ctx, t)
	workflow := app.NewWorkflowServiceFromStore(store)

	task, err := store.CreateTask(ctx, project.ID, "to move", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}

	if _, err := workflow.MoveTask(ctx, project.Context(), task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask(backlog->dev) = %v", err)
	}
	if _, err := workflow.MoveTask(ctx, project.Context(), task.ID, "done"); err != nil {
		t.Fatalf("MoveTask(dev->done) = %v", err)
	}

	events, err := store.ListTaskActivity(ctx, project.ID, task.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity = %v", err)
	}

	wantTypes := []string{
		domain.EventTypeTaskCreated,
		domain.EventTypeTaskMoved,
		domain.EventTypeTaskMoved,
		domain.EventTypeTaskCompleted,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("len(events) = %d, want %d (%v)", len(events), len(wantTypes), eventTypes(events))
	}
	for i, want := range wantTypes {
		if events[i].EventType != want {
			t.Errorf("events[%d].EventType = %q, want %q", i, events[i].EventType, want)
		}
	}

	// task.moved payload must record the from/to bucket pair so the activity
	// feed can render "task moved backlog → dev" without a follow-up query.
	moved := events[1]
	if !strings.Contains(moved.Payload, "backlog") || !strings.Contains(moved.Payload, "dev") {
		t.Errorf("first move payload = %q, want both %q and %q", moved.Payload, "backlog", "dev")
	}
}

func TestMoveTaskCompletedOnlyOnFinalBucket(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithFullTransitions(ctx, t)

	task, err := store.CreateTask(ctx, project.ID, "intermediate", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}

	events, err := store.ListTaskActivity(ctx, project.ID, task.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity = %v", err)
	}
	for _, ev := range events {
		if ev.EventType == domain.EventTypeTaskCompleted {
			t.Fatalf("unexpected task.completed event when moving to a non-final bucket: %+v", ev)
		}
	}
}

func TestListTaskActivityUnifiesCommentsAndEvents(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithFullTransitions(ctx, t)

	task, err := store.CreateTask(ctx, project.ID, "with comment", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "first comment", "agent", nil); err != nil {
		t.Fatalf("AddComment = %v", err)
	}
	if _, err := store.MoveTask(ctx, project.ID, task.ID, "dev"); err != nil {
		t.Fatalf("MoveTask = %v", err)
	}

	asc, err := store.ListTaskActivity(ctx, project.ID, task.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity asc = %v", err)
	}
	wantTypesAsc := []string{
		domain.EventTypeTaskCreated,
		domain.EventTypeComment,
		domain.EventTypeTaskMoved,
	}
	if got := eventTypes(asc); !equalStringsLocal(got, wantTypesAsc) {
		t.Fatalf("asc order = %v, want %v", got, wantTypesAsc)
	}

	desc, err := store.ListTaskActivity(ctx, project.ID, task.ID, "desc")
	if err != nil {
		t.Fatalf("ListTaskActivity desc = %v", err)
	}
	wantTypesDesc := []string{
		domain.EventTypeTaskMoved,
		domain.EventTypeComment,
		domain.EventTypeTaskCreated,
	}
	if got := eventTypes(desc); !equalStringsLocal(got, wantTypesDesc) {
		t.Fatalf("desc order = %v, want %v", got, wantTypesDesc)
	}
}

func TestCommentsAddRoutesThroughEvents(t *testing.T) {
	ctx := context.Background()
	store, project := openStoreWithProject(ctx, t)

	task, err := store.CreateTask(ctx, project.ID, "task", "", "", "backlog")
	if err != nil {
		t.Fatalf("CreateTask = %v", err)
	}
	comment, err := store.AddComment(ctx, project.ID, task.ID, "hello", "human", nil)
	if err != nil {
		t.Fatalf("AddComment = %v", err)
	}
	// Backward-compat: ListComments must still surface comments even though
	// the row physically lives in the events table now.
	comments, err := store.ListComments(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListComments = %v", err)
	}
	if len(comments) != 1 || comments[0].ID != comment.ID {
		t.Fatalf("ListComments mismatch: got %+v, want comment id %d", comments, comment.ID)
	}
}

func eventTypes(events []domain.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.EventType
	}
	return out
}

func equalStringsLocal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
