package sqlite

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

func openTestStore(t *testing.T) *storeFixture {
	t.Helper()
	store := openStoreFixture(t, t.TempDir()+"/omakiten.db")
	store.applyBundle(sqliteTestBundle(t))
	return store
}

func TestFindOrCreateTag(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	tag, err := store.FindOrCreateTag(ctx, "go", "Go")
	if err != nil {
		t.Fatalf("FindOrCreateTag() error = %v", err)
	}
	if tag.Name != "go" || tag.Label != "Go" {
		t.Fatalf("FindOrCreateTag() = %+v; want name=go label=Go", tag)
	}

	// Idempotent — same name returns same tag
	tag2, err := store.FindOrCreateTag(ctx, "go", "Golang")
	if err != nil {
		t.Fatalf("FindOrCreateTag() idempotent error = %v", err)
	}
	if tag2.ID != tag.ID {
		t.Fatalf("FindOrCreateTag() idempotent returned different ID: %d vs %d", tag2.ID, tag.ID)
	}
	if tag2.Label != "Go" {
		t.Fatalf("FindOrCreateTag() idempotent changed label: %q, want %q", tag2.Label, "Go")
	}
}

func TestListAllTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, err := store.UpsertProject(ctx, "P", "p", "/work/p")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")
	tsTag, _ := store.FindOrCreateTag(ctx, "ts", "Ts")

	_ = store.AddTaskTag(ctx, project.ID, task.ID, goTag.ID)
	_ = store.AddTaskTag(ctx, project.ID, task.ID, tsTag.ID)

	// Add orphan tag
	_, _ = store.FindOrCreateTag(ctx, "orphan", "Orphan")

	tags, err := store.ListAllTags(ctx)
	if err != nil {
		t.Fatalf("ListAllTags() error = %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("ListAllTags() len = %d, want 3", len(tags))
	}

	// Verify usage counts
	counts := map[string]int{}
	for _, tag := range tags {
		counts[tag.Name] = tag.UsageCount
	}
	if counts["go"] != 1 {
		t.Fatalf("ListAllTags() go usage = %d, want 1", counts["go"])
	}
	if counts["ts"] != 1 {
		t.Fatalf("ListAllTags() ts usage = %d, want 1", counts["ts"])
	}
	if counts["orphan"] != 0 {
		t.Fatalf("ListAllTags() orphan usage = %d, want 0", counts["orphan"])
	}
}

func TestAddRemoveListTaskTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.snap())

	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")

	// Add tag
	if err := store.AddTaskTag(ctx, project.ID, task.ID, goTag.ID); err != nil {
		t.Fatalf("AddTaskTag() error = %v", err)
	}

	// Idempotent
	if err := store.AddTaskTag(ctx, project.ID, task.ID, goTag.ID); err != nil {
		t.Fatalf("AddTaskTag() idempotent error = %v", err)
	}

	tags, err := store.ListTaskTags(ctx, project.ID, task.ID)
	if err != nil {
		t.Fatalf("ListTaskTags() error = %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Fatalf("ListTaskTags() = %+v; want [go]", tags)
	}

	// Remove
	if err := store.RemoveTaskTag(ctx, project.ID, task.ID, goTag.ID); err != nil {
		t.Fatalf("RemoveTaskTag() error = %v", err)
	}
	tags, _ = store.ListTaskTags(ctx, project.ID, task.ID)
	if len(tags) != 0 {
		t.Fatalf("ListTaskTags() after remove len = %d, want 0", len(tags))
	}
}

// TestAddTaskTag_UnknownTagIDReturnsValidationError pins the new
// behavior introduced by the MapSQLiteError migration: when callers
// pass a tag_id that no longer (or never did) exist, the storage layer
// surfaces a typed validation error instead of the raw FK message
// bubbling up as a generic internal-error envelope. Before the
// migration this path returned the driver error verbatim and callers
// mapped it to ErrInternal — the typed surface lets ErrorService
// classify it as ErrValidation with the field hint intact.
func TestAddTaskTag_UnknownTagIDReturnsValidationError(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.snap())

	const phantomTagID int64 = 9999
	err := store.AddTaskTag(ctx, project.ID, task.ID, phantomTagID)
	if err == nil {
		t.Fatalf("AddTaskTag(phantom tag) = nil, want FK violation")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("AddTaskTag(phantom tag) returned %T (%v); want *domain.CodedError", err, err)
	}
	if coded.Code != domain.ErrValidation {
		t.Fatalf("AddTaskTag(phantom tag) Code = %q, want %q", coded.Code, domain.ErrValidation)
	}
}

func TestListTaskTagsByProject(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	taskA, _ := store.CreateTask(ctx, project.ID, "A", "", domain.Priority(2), "backlog", nil, store.snap())
	taskB, _ := store.CreateTask(ctx, project.ID, "B", "", domain.Priority(2), "backlog", nil, store.snap())

	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")
	tsTag, _ := store.FindOrCreateTag(ctx, "ts", "Ts")

	_ = store.AddTaskTag(ctx, project.ID, taskA.ID, goTag.ID)
	_ = store.AddTaskTag(ctx, project.ID, taskA.ID, tsTag.ID)
	_ = store.AddTaskTag(ctx, project.ID, taskB.ID, goTag.ID)

	m, err := store.ListTaskTagsByProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListTaskTagsByProject() error = %v", err)
	}
	if len(m[taskA.ID]) != 2 {
		t.Fatalf("ListTaskTagsByProject() taskA len = %d, want 2", len(m[taskA.ID]))
	}
	if len(m[taskB.ID]) != 1 {
		t.Fatalf("ListTaskTagsByProject() taskB len = %d, want 1", len(m[taskB.ID]))
	}
}

func TestMergeTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.snap())

	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")
	golangTag, _ := store.FindOrCreateTag(ctx, "golang-alias", "Golang alias")

	_ = store.AddTaskTag(ctx, project.ID, task.ID, golangTag.ID)

	merged, err := store.MergeTags(ctx, golangTag.ID, goTag.ID)
	if err != nil {
		t.Fatalf("MergeTags() error = %v", err)
	}
	if merged.ID != goTag.ID {
		t.Fatalf("MergeTags() returned tag ID %d, want %d", merged.ID, goTag.ID)
	}

	// Task should now have go tag, not golang-alias
	tags, _ := store.ListTaskTags(ctx, project.ID, task.ID)
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Fatalf("MergeTags() task tags = %+v; want [go]", tags)
	}

	// Source tag should be gone
	allTags, _ := store.ListAllTags(ctx)
	for _, tag := range allTags {
		if tag.Name == "golang-alias" {
			t.Fatalf("MergeTags() source tag still exists")
		}
	}
}

func TestDeleteOrphanTags(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	task, _ := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.snap())

	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")
	orphan1, _ := store.FindOrCreateTag(ctx, "orphan1", "Orphan1")
	orphan2, _ := store.FindOrCreateTag(ctx, "orphan2", "Orphan2")

	_ = store.AddTaskTag(ctx, project.ID, task.ID, goTag.ID)

	n, err := store.DeleteOrphanTags(ctx)
	if err != nil {
		t.Fatalf("DeleteOrphanTags() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteOrphanTags() = %d, want 2", n)
	}

	allTags, _ := store.ListAllTags(ctx)
	if len(allTags) != 1 || allTags[0].Name != "go" {
		t.Fatalf("DeleteOrphanTags() remaining = %+v; want [go]", allTags)
	}

	// Verify orphan IDs are gone
	_ = orphan1
	_ = orphan2
}

func TestAddProjectTag(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	project, _ := store.UpsertProject(ctx, "P", "p", "/work/p")
	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")

	if err := store.AddProjectTag(ctx, project.ID, goTag.ID); err != nil {
		t.Fatalf("AddProjectTag() error = %v", err)
	}

	// Idempotent
	if err := store.AddProjectTag(ctx, project.ID, goTag.ID); err != nil {
		t.Fatalf("AddProjectTag() idempotent error = %v", err)
	}

	tags, err := store.ListProjectTags(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListProjectTags() error = %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Fatalf("ListProjectTags() = %+v; want [go]", tags)
	}

	// Remove
	if err := store.RemoveProjectTag(ctx, project.ID, goTag.ID); err != nil {
		t.Fatalf("RemoveProjectTag() error = %v", err)
	}
	tags, _ = store.ListProjectTags(ctx, project.ID)
	if len(tags) != 0 {
		t.Fatalf("ListProjectTags() after remove len = %d, want 0", len(tags))
	}
}

func TestRenameTag(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	goTag, _ := store.FindOrCreateTag(ctx, "go", "Go")

	renamed, err := store.RenameTag(ctx, goTag.ID, "Golang")
	if err != nil {
		t.Fatalf("RenameTag() error = %v", err)
	}
	if renamed.Label != "Golang" {
		t.Fatalf("RenameTag() label = %q, want %q", renamed.Label, "Golang")
	}
	if renamed.Name != "go" {
		t.Fatalf("RenameTag() name changed: %q, want %q", renamed.Name, "go")
	}
}
