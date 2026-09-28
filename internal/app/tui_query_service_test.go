package app

import (
	"context"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

func TestTUIQueryPortSnapshotAggregatesRepos(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	cfgSnap := store.Snapshot()
	tasks := NewTaskServiceFromStore(store, testfixtures.CanonicalRegistry(), cfgSnap)
	active, err := tasks.Add(ctx, project.Context(), "active-task", "body", "", "backlog")
	if err != nil {
		t.Fatalf("Add(active) error = %v", err)
	}
	archived, err := tasks.Add(ctx, project.Context(), "archived-task", "", "", "backlog")
	if err != nil {
		t.Fatalf("Add(archived) error = %v", err)
	}
	if _, _, err := tasks.Archive(ctx, project.Context(), archived.ID); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	deps := NewDependencyService(store)
	if _, err := deps.Add(ctx, project.Context(), active.ID, archived.ID); err != nil {
		t.Fatalf("Dependency.Add() error = %v", err)
	}

	comments := NewCommentService(store, cfgSnap)
	if _, err := comments.Add(ctx, project.Context(), active.ID, "hello board", "human", nil); err != nil {
		t.Fatalf("Comment.Add() error = %v", err)
	}

	tags := NewTagServiceWithEvents(store, nil, cfgSnap)
	if _, err := tags.Add(ctx, project.Context(), TagEntityTask, active.ID, "board"); err != nil {
		t.Fatalf("Tag.Add() error = %v", err)
	}

	var query = NewTUIQueryService(store, cfgSnap, store, store, store)
	sort := domain.TaskSort{Field: "id", Order: "asc"}
	assertTUISnapshot(t, query, ctx, project.Context(), sort, active.ID)
}

func assertTUISnapshot(t *testing.T, query TUIQuery, ctx context.Context, project domain.ProjectContext, sort domain.TaskSort, activeID int64) {
	snap, err := query.Snapshot(ctx, project, sort)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snap.Tasks) != 1 || snap.Tasks[0].ID != activeID {
		t.Fatalf("Snapshot() tasks = %#v, want only active id=%d", snap.Tasks, activeID)
	}
	if snap.Workflow.Key == "" && len(snap.Workflow.Buckets) == 0 {
		t.Fatal("Snapshot() workflow is empty, want bundle workflow")
	}
	if len(snap.Dependencies) != 1 {
		t.Fatalf("Snapshot() deps = %d, want 1", len(snap.Dependencies))
	}
	if len(snap.Comments) != 1 || snap.Comments[0].Body != "hello board" {
		t.Fatalf("Snapshot() comments = %#v, want hello board", snap.Comments)
	}
	if len(snap.Laws) == 0 || len(snap.Skills) == 0 || len(snap.Personas) == 0 {
		t.Fatalf("Snapshot() entities laws=%d skills=%d personas=%d, want catalog from config.Snapshot", len(snap.Laws), len(snap.Skills), len(snap.Personas))
	}
	if len(snap.AllTags) == 0 {
		t.Fatal("Snapshot() AllTags empty")
	}
	if got := snap.TaskTagsByID[activeID]; len(got) != 1 {
		t.Fatalf("Snapshot() TaskTagsByID[%d] = %#v, want 1 tag", activeID, got)
	}

	withArchived, err := query.Snapshot(ctx, project, sort, SnapshotOptions{IncludeArchived: true})
	if err != nil {
		t.Fatalf("Snapshot(IncludeArchived) error = %v", err)
	}
	if len(withArchived.Tasks) != 2 {
		t.Fatalf("Snapshot(IncludeArchived) tasks = %d, want 2", len(withArchived.Tasks))
	}
}

func TestNewTUIQueryServiceReturnsPort(t *testing.T) {
	var query = NewTUIQueryService(nil, nil, nil, nil, nil)
	if query == nil {
		t.Fatal("NewTUIQueryService() = nil")
	}
	if _, ok := query.(*TUIQueryService); !ok {
		t.Fatalf("NewTUIQueryService() dynamic type = %T, want *TUIQueryService", query)
	}
}
