package app

import (
	"context"
	"encoding/json"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestTaskServiceAddSubAttachesParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")
	if err != nil {
		t.Fatalf("AddSub = %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatalf("child.ParentID = %v, want %d", child.ParentID, parent.ID)
	}
	if !child.IsSubTask() {
		t.Fatal("IsSubTask = false after AddSub")
	}
}

func TestTaskServiceAddSubRejectsMissingParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	_, err := service.AddSub(ctx, project.Context(), 9999, "Orphan", "", "", "backlog")
	if err == nil {
		t.Fatal("AddSub(missing parent) error = nil, want task_not_found")
	}
	assertCodedError(t, err, domain.ErrTaskNotFound)
}

func TestTaskServiceEditReparent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	a, _ := service.Add(ctx, project.Context(), "A", "", "", "backlog")
	b, _ := service.Add(ctx, project.Context(), "B", "", "", "backlog")

	if _, err := service.Edit(ctx, project.Context(), b.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &a.ID}); err != nil {
		t.Fatalf("Edit(reparent) = %v", err)
	}

	children, err := store.ListDirectChildren(ctx, project.ID, a.ID, store.Snapshot())
	if err != nil {
		t.Fatalf("ListDirectChildren = %v", err)
	}
	if len(children) != 1 || children[0].ID != b.ID {
		t.Fatalf("A's children after reparent = %+v, want [B]", children)
	}
}

func TestTaskServiceEditRejectsCycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	root, _ := service.Add(ctx, project.Context(), "Root", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), root.ID, "Child", "", "", "backlog")

	_, err := service.Edit(ctx, project.Context(), root.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &child.ID})
	if err == nil {
		t.Fatal("Edit(cycle) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceEditClearsParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	child, _ := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")

	if _, err := service.Edit(ctx, project.Context(), child.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: nil}); err != nil {
		t.Fatalf("Edit(clear parent) = %v", err)
	}

	children, err := store.ListDirectChildren(ctx, project.ID, parent.ID, store.Snapshot())
	if err != nil {
		t.Fatalf("ListDirectChildren = %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("parent still has %d children after clear", len(children))
	}
}

func TestTaskServiceAddSubRejectsArchivedParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, _ := service.Add(ctx, project.Context(), "Parent", "", "", "backlog")
	if _, _, err := service.Archive(ctx, project.Context(), parent.ID); err != nil {
		t.Fatalf("Archive(parent) = %v", err)
	}

	_, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")
	if err == nil {
		t.Fatal("AddSub(archived parent) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceAddSubLandsInRootKitFirstBucket_WhenNoCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	// A new sub-task is new work and must land at the start of the
	// resolved workflow — the root kit's first bucket when no sub-kit
	// is configured. The pre-#281 "workflow herdado do pai" invariant
	// is gone: a fresh sub-task in done while the parent sits in dev
	// never made sense.
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(default bucket) = %v", err)
	}
	if child.BucketKey != "backlog" {
		t.Fatalf("child.BucketKey = %q, want backlog (root kit first bucket)", child.BucketKey)
	}
}

func TestTaskServiceAddSubAcceptsExplicitNonParentBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	// A caller that names an explicit bucket continues to work — the
	// cross-bucket validation of the pre-#281 invariant is removed
	// along with the parent-bucket inheritance. (The default fixture
	// only ships backlog + dev; landing in backlog explicitly proves
	// the caller picks the lane, not the parent's current bucket.)
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "backlog")
	if err != nil {
		t.Fatalf("AddSub(explicit backlog) = %v", err)
	}
	if child.BucketKey != "backlog" {
		t.Fatalf("child.BucketKey = %q, want backlog (explicit caller choice)", child.BucketKey)
	}
	if parent.BucketKey != "dev" {
		t.Fatalf("parent.BucketKey = %q (sanity); child lane must differ from parent", parent.BucketKey)
	}
}

func TestTaskServiceAddSubUsesSubtaskKitFirstBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t, 1000)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, nil)
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub = %v", err)
	}
	if child.BucketKey != "todo" {
		t.Fatalf("child.BucketKey = %q, want sub-kit first bucket todo", child.BucketKey)
	}
}

func TestTaskServiceMoveSubtaskUsesSubtaskKitTransitions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle, registry := testfixtures.LoadBundle(t, "subtasks_smoke.yaml")
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 2, Key: "dev", Name: "Development", Position: 1},
		{ID: 3, Key: "review", Name: "Review", Position: 2},
		{ID: 4, Key: "done", Name: "Done", Position: 3},
	}, []config.Transition{{From: 2, To: 4}})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, registry, store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub = %v", err)
	}

	_, err = service.Move(ctx, project.Context(), child.ID, "review")
	if err == nil {
		t.Fatal("Move(child dev->review) error = nil, want sub-kit transition rejection")
	}
	assertCodedError(t, err, domain.ErrWorkflowInvalidTransition)
}

func TestTaskServiceEventsCarryDepthMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bundle := appTestBundle(t, 1000)
	bundle.SubtaskBundle = subtaskRuntimeBundle("sub", []config.Bucket{
		{ID: 10, Key: "todo", Name: "Todo", Position: 1},
		{ID: 20, Key: "done", Name: "Done", Position: 2},
	}, []config.Transition{{From: 10, To: 20}})
	store, project := appTestStore(t, bundle)
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	parent, err := service.Add(ctx, project.Context(), "Parent", "", "", "dev")
	if err != nil {
		t.Fatalf("Add(parent) = %v", err)
	}
	child, err := service.AddSub(ctx, project.Context(), parent.ID, "Child", "", "", "")
	if err != nil {
		t.Fatalf("AddSub(child) = %v", err)
	}
	if _, err := service.Move(ctx, project.Context(), child.ID, "done"); err != nil {
		t.Fatalf("Move(child todo->done) = %v", err)
	}
	renamed := "Child renamed"
	if _, err := service.Edit(ctx, project.Context(), child.ID, domain.TaskUpdate{Title: &renamed}); err != nil {
		t.Fatalf("Edit(child title) = %v", err)
	}

	createdEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskCreated, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(created) = %v", err)
	}
	rootPayload := eventPayloadForTask(t, createdEvents, parent.ID)
	assertSubjectMetadata(t, rootPayload, parent.ID, nil, 0, "default")
	childPayload := eventPayloadForTask(t, createdEvents, child.ID)
	assertSubjectMetadata(t, childPayload, child.ID, &parent.ID, 1, "sub")

	movedEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskMoved, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(moved) = %v", err)
	}
	movePayload := eventPayloadForTask(t, movedEvents, child.ID)
	assertSubjectMetadata(t, movePayload, child.ID, &parent.ID, 1, "sub")

	editedEvents, err := store.ListRecentEvents(ctx, domain.EventTypeTaskEdited, 10)
	if err != nil {
		t.Fatalf("ListRecentEvents(edited) = %v", err)
	}
	editPayload := eventPayloadForTask(t, editedEvents, child.ID)
	assertSubjectMetadata(t, editPayload, child.ID, &parent.ID, 1, "sub")
}

func TestTaskServiceEditRejectsReparentUnderArchived(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	candidate, _ := service.Add(ctx, project.Context(), "candidate parent", "", "", "backlog")
	target, _ := service.Add(ctx, project.Context(), "target", "", "", "backlog")
	if _, _, err := service.Archive(ctx, project.Context(), candidate.ID); err != nil {
		t.Fatalf("Archive(candidate) = %v", err)
	}

	_, err := service.Edit(ctx, project.Context(), target.ID, domain.TaskUpdate{ChangeParent: true, NewParentID: &candidate.ID})
	if err == nil {
		t.Fatal("Edit(reparent under archived) error = nil, want validation")
	}
	assertCodedError(t, err, domain.ErrValidation)
}

func TestTaskServiceListParentMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()
	service := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot())

	root, _ := service.Add(ctx, project.Context(), "root", "", "", "backlog")
	sibling, _ := service.Add(ctx, project.Context(), "sibling", "", "", "backlog")
	c1, _ := service.AddSub(ctx, project.Context(), root.ID, "c1", "", "", "backlog")
	_, _ = service.AddSub(ctx, project.Context(), root.ID, "c2", "", "", "backlog")

	roots, err := service.List(ctx, project.Context(), domain.TaskFilter{ParentMode: domain.ParentRoots})
	if err != nil {
		t.Fatalf("List(roots) = %v", err)
	}
	rootIDs := map[int64]bool{}
	for _, r := range roots {
		rootIDs[r.ID] = true
	}
	if !rootIDs[root.ID] || !rootIDs[sibling.ID] {
		t.Fatalf("roots filter missed expected ids: %+v", rootIDs)
	}
	if rootIDs[c1.ID] {
		t.Fatal("roots filter leaked a sub-task")
	}

	children, err := service.List(ctx, project.Context(), domain.TaskFilter{ParentMode: domain.ParentChildren, ParentValue: root.ID})
	if err != nil {
		t.Fatalf("List(children) = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("children of root = %d, want 2", len(children))
	}
	for _, c := range children {
		if c.ParentID == nil || *c.ParentID != root.ID {
			t.Fatalf("child %d ParentID = %v", c.ID, c.ParentID)
		}
	}
}

func subtaskRuntimeBundle(key string, buckets []config.Bucket, transitions []config.Transition) *config.Bundle {
	return &config.Bundle{
		Kit:    config.Kit{ID: 900, Key: key, Name: key},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: key}},
		Workflows: []config.Workflow{{
			ID:          900,
			Key:         key,
			Name:        key,
			Buckets:     buckets,
			Transitions: transitions,
		}},
	}
}

func eventPayloadForTask(t *testing.T, events []domain.Event, taskID int64) map[string]any {
	t.Helper()
	for _, event := range events {
		if event.EntityID != taskID {
			continue
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
			t.Fatalf("event payload for task %d is not JSON: %v", taskID, err)
		}
		return payload
	}
	t.Fatalf("event for task %d not found in %+v", taskID, events)
	return nil
}

func assertSubjectMetadata(t *testing.T, payload map[string]any, taskID int64, parentID *int64, depth int, kit string) {
	t.Helper()
	if got := int64(payload["subject_task_id"].(float64)); got != taskID {
		t.Fatalf("subject_task_id = %d, want %d (payload %+v)", got, taskID, payload)
	}
	if parentID == nil {
		if payload["subject_parent_id"] != nil {
			t.Fatalf("subject_parent_id = %v, want nil (payload %+v)", payload["subject_parent_id"], payload)
		}
	} else if got := int64(payload["subject_parent_id"].(float64)); got != *parentID {
		t.Fatalf("subject_parent_id = %d, want %d (payload %+v)", got, *parentID, payload)
	}
	if got := int(payload["subject_depth"].(float64)); got != depth {
		t.Fatalf("subject_depth = %d, want %d (payload %+v)", got, depth, payload)
	}
	if got := payload["resolved_kit"]; got != kit {
		t.Fatalf("resolved_kit = %v, want %q (payload %+v)", got, kit, payload)
	}
}
