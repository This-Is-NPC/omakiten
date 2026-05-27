package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type stubOrphanRepo struct {
	preview         domain.OrphanReport
	previewErr      error
	rebind          domain.OrphanReport
	rebindErr       error
	cascadePreview  domain.OrphanReport
	cascadeRebind   domain.OrphanReport
	cascadeErr      error
	previewCalls    int
	rebindCalls     int
	cascadePrevCall int
	cascadeRebCall  int
	lastCascadePlan domain.OrphanCascadePlan
}

func (s *stubOrphanRepo) PreviewOrphanedTasks(_ context.Context, _ int64, _ domain.BucketResolver, _ domain.BucketResolver) (domain.OrphanReport, error) {
	s.previewCalls++
	return s.preview, s.previewErr
}

func (s *stubOrphanRepo) RebindOrphanedTasks(_ context.Context, _ int64, _ domain.BucketResolver, _ domain.BucketResolver) (domain.OrphanReport, error) {
	s.rebindCalls++
	return s.rebind, s.rebindErr
}

func (s *stubOrphanRepo) PreviewOrphanedCascade(_ context.Context, _ int64, plan domain.OrphanCascadePlan) (domain.OrphanReport, error) {
	s.cascadePrevCall++
	s.lastCascadePlan = plan
	if s.cascadeErr != nil {
		return domain.OrphanReport{}, s.cascadeErr
	}
	return s.cascadePreview, nil
}

func (s *stubOrphanRepo) RebindOrphanedCascade(_ context.Context, _ int64, plan domain.OrphanCascadePlan) (domain.OrphanReport, error) {
	s.cascadeRebCall++
	s.lastCascadePlan = plan
	if s.cascadeErr != nil {
		return domain.OrphanReport{}, s.cascadeErr
	}
	return s.cascadeRebind, nil
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
	if repo.cascadePrevCall != 0 {
		t.Fatalf("legacy preview path fired cascade preview %d times (expected 0 — no sub-kit configured)", repo.cascadePrevCall)
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
	if repo.cascadeRebCall != 0 {
		t.Fatalf("legacy migrate path fired cascade rebind %d times (expected 0 — no sub-kit configured)", repo.cascadeRebCall)
	}
}

// TestOrphanService_MigrateCascadeRoutesAtomically pins the #281 contract +
// task #301 locked decision A7: when either snapshot in the pair carries a
// sub-task kit, Migrate routes through the atomic RebindOrphanedCascade so
// root + sub rebind land inside one transaction (NOT the legacy "all tasks"
// entrypoint or split RebindOrphanedRootTasks/RebindOrphanedSubtasks calls).
func TestOrphanService_MigrateCascadeRoutesAtomically(t *testing.T) {
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

	repo := &stubOrphanRepo{cascadeRebind: domain.OrphanReport{Total: 1, Groups: []domain.OrphanGroup{{Count: 1}}}}
	svc := NewOrphanService(repo, current, previous)

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if repo.rebindCalls != 0 {
		t.Fatalf("legacy RebindOrphanedTasks fired %d times; cascade must avoid it", repo.rebindCalls)
	}
	if repo.cascadeRebCall != 1 {
		t.Fatalf("RebindOrphanedCascade fired %d times, want 1 (atomic single call)", repo.cascadeRebCall)
	}
	if repo.lastCascadePlan.FromKit != "izakaya" || repo.lastCascadePlan.ToKit != "kaiseki" {
		t.Fatalf("cascade plan kit identities = (%q, %q), want (izakaya, kaiseki)",
			repo.lastCascadePlan.FromKit, repo.lastCascadePlan.ToKit)
	}
	if repo.lastCascadePlan.CurrentSub == nil {
		t.Fatalf("cascade plan missing CurrentSub resolver")
	}
}

// TestOrphanService_MigrateCascadeDisableRoutesAtomically pins the
// disable case: when the previous snapshot carried a sub-kit and the
// current one does NOT, Migrate still routes through the atomic cascade
// call so sub-tasks rebind against the root kit and emit
// task.bucket_orphaned with fromKit=<old sub-kit identity>,
// toKit=<current root kit identity>.
func TestOrphanService_MigrateCascadeDisableRoutesAtomically(t *testing.T) {
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

	repo := &stubOrphanRepo{cascadeRebind: domain.OrphanReport{Total: 1, Groups: []domain.OrphanGroup{{Count: 1}}}}
	svc := NewOrphanService(repo, current, previous)

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if repo.rebindCalls != 0 {
		t.Fatalf("legacy RebindOrphanedTasks fired %d times; disable cascade must avoid it", repo.rebindCalls)
	}
	if repo.cascadeRebCall != 1 {
		t.Fatalf("RebindOrphanedCascade fired %d times, want 1", repo.cascadeRebCall)
	}
	if repo.lastCascadePlan.FromKit != "izakaya" || repo.lastCascadePlan.ToKit != "root" {
		t.Fatalf("disable cascade plan kit identities = (%q, %q), want (izakaya, root)",
			repo.lastCascadePlan.FromKit, repo.lastCascadePlan.ToKit)
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
	if repo.cascadeRebCall != 0 {
		t.Fatalf("cascade fired without subtask_kit: cascade=%d", repo.cascadeRebCall)
	}
}

// TestOrphanService_PreviewCascadeWhenActive pins task #301 review
// §11557 finding A1: when a sub-task kit is configured, Preview routes
// through PreviewOrphanedCascade (the same plan Migrate uses) so the
// rows shown in the bundle-swap prompt match the rows Migrate would
// rewrite. Projects without subtask_kit keep the legacy path.
func TestOrphanService_PreviewCascadeWhenActive(t *testing.T) {
	currBundle := config.Bundle{
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
	current := config.BuildSnapshot(currBundle)
	previous := config.BuildSnapshot(prevBundle)

	repo := &stubOrphanRepo{cascadePreview: domain.OrphanReport{Total: 4, Groups: []domain.OrphanGroup{{Count: 4}}}}
	svc := NewOrphanService(repo, current, previous)

	got, err := svc.Preview(context.Background(), domain.ProjectContext{ID: 1})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if got.Total != 4 {
		t.Fatalf("Preview.Total = %d, want 4", got.Total)
	}
	if repo.cascadePrevCall != 1 {
		t.Fatalf("PreviewOrphanedCascade fired %d times, want 1 (cascade active)", repo.cascadePrevCall)
	}
	if repo.previewCalls != 0 {
		t.Fatalf("legacy PreviewOrphanedTasks fired %d times; cascade preview must replace it", repo.previewCalls)
	}
	if repo.lastCascadePlan.FromKit != "izakaya" || repo.lastCascadePlan.ToKit != "kaiseki" {
		t.Fatalf("preview plan kit identities = (%q, %q), want (izakaya, kaiseki)",
			repo.lastCascadePlan.FromKit, repo.lastCascadePlan.ToKit)
	}
}

func TestOrphanService_PreviewPropagatesError(t *testing.T) {
	repo := &stubOrphanRepo{previewErr: errors.New("boom")}
	svc := NewOrphanService(repo, nil, nil)
	if _, err := svc.Preview(context.Background(), domain.ProjectContext{ID: 1}); err == nil {
		t.Fatal("expected error, got nil")
	}
}
