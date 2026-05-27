package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type stubOrphanRepo struct {
	preview          domain.OrphanReport
	previewErr       error
	rebind           domain.OrphanReport
	rebindErr        error
	previewCalls     int
	rebindCalls      int
	rebindRootCalls  int
	rebindSubCalls   int
	rebindSubFromKit string
	rebindSubToKit   string
}

func (s *stubOrphanRepo) PreviewOrphanedTasks(_ context.Context, _ int64, _ domain.BucketResolver, _ domain.BucketResolver) (domain.OrphanReport, error) {
	s.previewCalls++
	return s.preview, s.previewErr
}

func (s *stubOrphanRepo) RebindOrphanedTasks(_ context.Context, _ int64, _ domain.BucketResolver, _ domain.BucketResolver) (domain.OrphanReport, error) {
	s.rebindCalls++
	return s.rebind, s.rebindErr
}

func (s *stubOrphanRepo) RebindOrphanedRootTasks(_ context.Context, _ int64, _ domain.BucketResolver, _ domain.BucketResolver) (domain.OrphanReport, error) {
	s.rebindRootCalls++
	return s.rebind, s.rebindErr
}

func (s *stubOrphanRepo) RebindOrphanedSubtasks(_ context.Context, _ int64, _ domain.BucketResolver, _ domain.BucketResolver, fromKit, toKit string) (domain.OrphanReport, error) {
	s.rebindSubCalls++
	s.rebindSubFromKit = fromKit
	s.rebindSubToKit = toKit
	return s.rebind, s.rebindErr
}

func TestOrphanService_PreviewDelegates(t *testing.T) {
	repo := &stubOrphanRepo{preview: domain.OrphanReport{Total: 3, WorkflowKey: "omakase"}}
	svc := NewOrphanService(repo, nil, nil)

	got, err := svc.Preview(context.Background(), domain.ProjectContext{ID: 1})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if got.Total != 3 || got.WorkflowKey != "omakase" {
		t.Fatalf("Preview = %+v", got)
	}
	if repo.previewCalls != 1 || repo.rebindCalls != 0 {
		t.Fatalf("calls preview=%d rebind=%d", repo.previewCalls, repo.rebindCalls)
	}
}

func TestOrphanService_MigrateDelegates(t *testing.T) {
	repo := &stubOrphanRepo{rebind: domain.OrphanReport{Total: 2}}
	svc := NewOrphanService(repo, nil, nil)

	got, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got.Total != 2 {
		t.Fatalf("Migrate.Total = %d", got.Total)
	}
	if repo.previewCalls != 0 || repo.rebindCalls != 1 {
		t.Fatalf("calls preview=%d rebind=%d", repo.previewCalls, repo.rebindCalls)
	}
}

// TestOrphanService_MigrateCascadeSplitsByDepth pins the #285 contract:
// when either snapshot in the pair carries a sub-task kit, Migrate fans
// the rebind into the root + sub-task paths (NOT the legacy "all tasks"
// entrypoint) and threads kit identities into the sub-task payload.
func TestOrphanService_MigrateCascadeSplitsByDepth(t *testing.T) {
	rootBundle := config.Bundle{
		Kit:       config.Kit{Key: "root"},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Name: "Root", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
		SubtaskBundle: &config.Bundle{
			Kit:       config.Kit{Key: "kaiseki"},
			Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{ID: 2, Key: "sub", Name: "Sub", Buckets: []config.Bucket{{ID: 10, Key: "backlog", Name: "Backlog", Position: 1}}}},
		},
	}
	prevBundle := config.Bundle{
		Kit:       config.Kit{Key: "root"},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Name: "Root", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
		SubtaskBundle: &config.Bundle{
			Kit:       config.Kit{Key: "izakaya"},
			Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{ID: 2, Key: "sub", Name: "Sub", Buckets: []config.Bucket{{ID: 11, Key: "backlog", Name: "Backlog", Position: 1}}}},
		},
	}
	current := config.BuildSnapshot(rootBundle)
	previous := config.BuildSnapshot(prevBundle)

	repo := &stubOrphanRepo{rebind: domain.OrphanReport{Total: 1, Groups: []domain.OrphanGroup{{Count: 1}}}}
	svc := NewOrphanService(repo, current, previous)

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if repo.rebindCalls != 0 {
		t.Fatalf("legacy RebindOrphanedTasks fired %d times; cascade must avoid it", repo.rebindCalls)
	}
	if repo.rebindRootCalls != 1 {
		t.Fatalf("rebindRootCalls = %d, want 1", repo.rebindRootCalls)
	}
	if repo.rebindSubCalls != 1 {
		t.Fatalf("rebindSubCalls = %d, want 1", repo.rebindSubCalls)
	}
	if repo.rebindSubFromKit != "izakaya" || repo.rebindSubToKit != "kaiseki" {
		t.Fatalf("sub-kit identities = (%q, %q), want (izakaya, kaiseki)", repo.rebindSubFromKit, repo.rebindSubToKit)
	}
}

// TestOrphanService_MigrateCascadeDisableSplitsByDepth pins the
// disable case: when the previous snapshot carried a sub-kit and the
// current one does NOT, Migrate still routes through the depth-split
// path (so sub-tasks rebind against the root kit and emit
// task.bucket_orphaned where appropriate), and the sub-task call sees
// fromKit=<old sub-kit identity>, toKit=<current root kit identity>.
func TestOrphanService_MigrateCascadeDisableSplitsByDepth(t *testing.T) {
	currBundle := config.Bundle{
		Kit:       config.Kit{Key: "root"},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Name: "Root", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
	}
	prevBundle := config.Bundle{
		Kit:       config.Kit{Key: "root"},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Name: "Root", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
		SubtaskBundle: &config.Bundle{
			Kit:       config.Kit{Key: "izakaya"},
			Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{ID: 2, Key: "sub", Name: "Sub", Buckets: []config.Bucket{{ID: 10, Key: "backlog", Name: "Backlog", Position: 1}}}},
		},
	}
	current := config.BuildSnapshot(currBundle)
	previous := config.BuildSnapshot(prevBundle)

	repo := &stubOrphanRepo{rebind: domain.OrphanReport{Total: 1, Groups: []domain.OrphanGroup{{Count: 1}}}}
	svc := NewOrphanService(repo, current, previous)

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if repo.rebindCalls != 0 {
		t.Fatalf("legacy RebindOrphanedTasks fired %d times; disable cascade must avoid it", repo.rebindCalls)
	}
	if repo.rebindRootCalls != 1 {
		t.Fatalf("rebindRootCalls = %d, want 1 (disable still rebinds root tree)", repo.rebindRootCalls)
	}
	if repo.rebindSubCalls != 1 {
		t.Fatalf("rebindSubCalls = %d, want 1 (sub-tasks collapse back through root kit)", repo.rebindSubCalls)
	}
	if repo.rebindSubFromKit != "izakaya" || repo.rebindSubToKit != "root" {
		t.Fatalf("disable kit identities = (%q, %q), want (izakaya, root)", repo.rebindSubFromKit, repo.rebindSubToKit)
	}
}

// TestOrphanService_MigrateLegacyPathWhenNoSubtaskKit guards the no-op
// guarantee: projects without subtask_kit must keep calling the legacy
// "all tasks" entrypoint so pre-#281 behaviour stays byte-identical.
func TestOrphanService_MigrateLegacyPathWhenNoSubtaskKit(t *testing.T) {
	bundle := config.Bundle{
		Kit:       config.Kit{Key: "root"},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{ID: 1, Key: "root", Name: "Root", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
	}
	current := config.BuildSnapshot(bundle)
	previous := config.BuildSnapshot(bundle)

	repo := &stubOrphanRepo{rebind: domain.OrphanReport{Total: 0}}
	svc := NewOrphanService(repo, current, previous)

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if repo.rebindCalls != 1 {
		t.Fatalf("legacy RebindOrphanedTasks fired %d times, want 1", repo.rebindCalls)
	}
	if repo.rebindRootCalls != 0 || repo.rebindSubCalls != 0 {
		t.Fatalf("cascade fired without subtask_kit: root=%d sub=%d", repo.rebindRootCalls, repo.rebindSubCalls)
	}
}

func TestOrphanService_PreviewPropagatesError(t *testing.T) {
	repo := &stubOrphanRepo{previewErr: errors.New("boom")}
	svc := NewOrphanService(repo, nil, nil)
	if _, err := svc.Preview(context.Background(), domain.ProjectContext{ID: 1}); err == nil {
		t.Fatal("expected error, got nil")
	}
}
