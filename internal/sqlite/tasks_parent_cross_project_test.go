package sqlite

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestSetTaskParentRejectsCrossProjectAtDBLayer pins migration 027's
// trigger guard: even if the app-layer guard in tasks_parent.go is
// bypassed (direct SQL, restored backup, future code path), the DB
// must reject a parent_id pointing at a task in a different project.
func TestSetTaskParentRejectsCrossProjectAtDBLayer(t *testing.T) {
	ctx, store, projectA := setupLifecycle(t)

	projectB, err := store.UpsertProject(ctx, "Other", "other", "/work/other")
	if err != nil {
		t.Fatalf("UpsertProject(other) = %v", err)
	}
	parent, err := store.CreateTask(ctx, projectA.ID, "Parent in A", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask(parent) = %v", err)
	}
	child, err := store.CreateTask(ctx, projectB.ID, "Child in B", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask(child) = %v", err)
	}

	// Direct SQL bypasses the app-layer guard; the trigger should still
	// abort the update.
	_, err = store.db.ExecContext(ctx, "UPDATE tasks SET parent_id = ? WHERE id = ? AND project_id = ?", parent.ID, child.ID, projectB.ID)
	if err == nil {
		t.Fatalf("direct cross-project parent update succeeded; trigger guard missing")
	}
	if !strings.Contains(err.Error(), "same project") {
		t.Fatalf("error %q does not name the trigger's message", err)
	}

	// Confirm same-project assignment still works (regression on the trigger's WHEN clause).
	siblingParent, err := store.CreateTask(ctx, projectB.ID, "Parent in B", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask(sibling parent) = %v", err)
	}
	if err := store.SetTaskParent(ctx, projectB.ID, child.ID, &siblingParent.ID); err != nil {
		t.Fatalf("same-project SetTaskParent should succeed: %v", err)
	}
}
