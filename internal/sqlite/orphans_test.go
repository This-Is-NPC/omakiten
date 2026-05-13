package sqlite

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func TestPreviewOrphanedTasks_NoOrphans(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ImportBundle(ctx, sqliteTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project := mustUpsertProject(t, store, "p", "p", "/p")
	if _, err := store.CreateTask(ctx, project.ID, "T1", "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	report, err := store.PreviewOrphanedTasks(ctx, project.ID)
	if err != nil {
		t.Fatalf("PreviewOrphanedTasks: %v", err)
	}
	if report.Total != 0 || len(report.Groups) != 0 {
		t.Fatalf("expected empty report, got %+v", report)
	}
	if report.WorkflowKey != "default" {
		t.Fatalf("WorkflowKey = %q, want default", report.WorkflowKey)
	}
}

func TestPreviewOrphanedTasks_MissingKeyMapsToDefault(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"docs", "dev"}, []int{1, 2}), "a.yaml", "h1"); err != nil {
		t.Fatalf("ImportBundle A: %v", err)
	}
	project := mustUpsertProject(t, store, "p", "p", "/p")
	docsTask, err := store.CreateTask(ctx, project.ID, "doc task", "", domain.Priority(2), "docs")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"backlog", "dev"}, []int{3, 2}), "b.yaml", "h2"); err != nil {
		t.Fatalf("ImportBundle B: %v", err)
	}

	report, err := store.PreviewOrphanedTasks(ctx, project.ID)
	if err != nil {
		t.Fatalf("PreviewOrphanedTasks: %v", err)
	}
	if report.Total != 1 {
		t.Fatalf("Total = %d, want 1; report=%+v", report.Total, report)
	}
	if len(report.Groups) != 1 {
		t.Fatalf("Groups len = %d, want 1", len(report.Groups))
	}
	g := report.Groups[0]
	if g.FromBucketKey != "docs" || g.ToBucketKey != "backlog" {
		t.Fatalf("group from→to = %s→%s, want docs→backlog", g.FromBucketKey, g.ToBucketKey)
	}
	if len(g.Tasks) != 1 || g.Tasks[0].TaskID != docsTask.ID {
		t.Fatalf("group tasks = %+v, want one with task id %d", g.Tasks, docsTask.ID)
	}
}

func TestPreviewOrphanedTasks_PreservedKeyNotOrphan(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"backlog", "docs"}, []int{1, 2}), "a.yaml", "h1"); err != nil {
		t.Fatalf("ImportBundle A: %v", err)
	}
	project := mustUpsertProject(t, store, "p", "p", "/p")
	if _, err := store.CreateTask(ctx, project.ID, "alive", "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask backlog: %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "doomed", "", domain.Priority(2), "docs"); err != nil {
		t.Fatalf("CreateTask docs: %v", err)
	}

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"backlog", "dev"}, []int{1, 3}), "b.yaml", "h2"); err != nil {
		t.Fatalf("ImportBundle B: %v", err)
	}

	report, err := store.PreviewOrphanedTasks(ctx, project.ID)
	if err != nil {
		t.Fatalf("PreviewOrphanedTasks: %v", err)
	}
	if report.Total != 1 {
		t.Fatalf("Total = %d, want 1 (only docs is orphan; backlog preserved by key)", report.Total)
	}
	if report.Groups[0].FromBucketKey != "docs" {
		t.Fatalf("orphan FromBucketKey = %s, want docs", report.Groups[0].FromBucketKey)
	}
}

func TestRebindOrphanedTasks_UpdatesBucketAndEmitsEvent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"docs"}, []int{1}), "a.yaml", "h1"); err != nil {
		t.Fatalf("ImportBundle A: %v", err)
	}
	project := mustUpsertProject(t, store, "p", "p", "/p")
	task, err := store.CreateTask(ctx, project.ID, "doc", "", domain.Priority(2), "docs")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"backlog"}, []int{2}), "b.yaml", "h2"); err != nil {
		t.Fatalf("ImportBundle B: %v", err)
	}

	report, err := store.RebindOrphanedTasks(ctx, project.ID)
	if err != nil {
		t.Fatalf("RebindOrphanedTasks: %v", err)
	}
	if report.Total != 1 {
		t.Fatalf("Total = %d, want 1", report.Total)
	}

	tasks, err := store.ListTasks(ctx, project.ID, domain.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].BucketKey != "backlog" {
		t.Fatalf("task after rebind = %+v, want bucket backlog", tasks)
	}

	events, err := store.ListTaskActivity(ctx, project.ID, task.ID, "asc")
	if err != nil {
		t.Fatalf("ListTaskActivity: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.EventType == domain.EventTypeTaskMigrated {
			if !strings.Contains(ev.Payload, `"from":"docs"`) || !strings.Contains(ev.Payload, `"to":"backlog"`) {
				t.Fatalf("payload missing from/to: %q", ev.Payload)
			}
			if !strings.Contains(ev.Payload, `"reason":"workflow_swap"`) {
				t.Fatalf("payload missing reason: %q", ev.Payload)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("task.migrated event not emitted; events=%+v", events)
	}
}

func TestRebindOrphanedTasks_NoOpWhenEmpty(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.ImportBundle(ctx, sqliteTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	project := mustUpsertProject(t, store, "p", "p", "/p")

	report, err := store.RebindOrphanedTasks(ctx, project.ID)
	if err != nil {
		t.Fatalf("RebindOrphanedTasks: %v", err)
	}
	if report.Total != 0 {
		t.Fatalf("Total = %d, want 0", report.Total)
	}
}

func TestPreviewOrphanedTasks_IgnoresArchived(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"docs"}, []int{1}), "a.yaml", "h1"); err != nil {
		t.Fatalf("ImportBundle A: %v", err)
	}
	project := mustUpsertProject(t, store, "p", "p", "/p")
	task, err := store.CreateTask(ctx, project.ID, "archived doc", "", domain.Priority(2), "docs")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, _, err := store.SetTaskState(ctx, project.ID, task.ID, domain.TaskStateArchived, ""); err != nil {
		t.Fatalf("SetTaskState: %v", err)
	}

	if err := store.ImportBundle(ctx, bundleWithKeys(t, "default", []string{"backlog"}, []int{2}), "b.yaml", "h2"); err != nil {
		t.Fatalf("ImportBundle B: %v", err)
	}

	report, err := store.PreviewOrphanedTasks(ctx, project.ID)
	if err != nil {
		t.Fatalf("PreviewOrphanedTasks: %v", err)
	}
	if report.Total != 0 {
		t.Fatalf("archived task should be ignored; report=%+v", report)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustUpsertProject(t *testing.T, store *Store, name, slug, root string) domain.Project {
	t.Helper()
	p, err := store.UpsertProject(context.Background(), name, slug, root)
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}
	return p
}

// bundleWithKeys returns a copy of sqliteTestBundle whose single workflow has
// the supplied bucket keys (and matching local IDs). Used to simulate a
// workflow swap: two calls with disjoint key sets exercise the orphan logic.
func bundleWithKeys(t *testing.T, workflowKey string, keys []string, ids []int) config.Bundle {
	t.Helper()
	if len(keys) != len(ids) {
		t.Fatalf("bundleWithKeys: keys and ids length mismatch")
	}
	bundle := sqliteTestBundle(t)
	bundle.Config.Workflow.Active = workflowKey
	wf := bundle.Workflows[0]
	wf.Key = workflowKey
	wf.Name = workflowKey
	buckets := make([]config.Bucket, len(keys))
	for i, k := range keys {
		buckets[i] = config.Bucket{ID: ids[i], Key: k, Name: k, Position: i + 1}
	}
	wf.Buckets = buckets
	wf.Transitions = nil
	bundle.Workflows = []config.Workflow{wf}
	return bundle
}
