package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

type stubOrphanRepo struct {
	preview      domain.OrphanReport
	previewErr   error
	rebind       domain.OrphanReport
	rebindErr    error
	previewCalls int
	rebindCalls  int
}

func (s *stubOrphanRepo) PreviewOrphanedTasks(_ context.Context, _ int64) (domain.OrphanReport, error) {
	s.previewCalls++
	return s.preview, s.previewErr
}

func (s *stubOrphanRepo) RebindOrphanedTasks(_ context.Context, _ int64) (domain.OrphanReport, error) {
	s.rebindCalls++
	return s.rebind, s.rebindErr
}

func TestOrphanService_PreviewDelegates(t *testing.T) {
	repo := &stubOrphanRepo{preview: domain.OrphanReport{Total: 3, WorkflowKey: "omakase"}}
	svc := NewOrphanService(repo)

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
	svc := NewOrphanService(repo)

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

func TestOrphanService_PreviewPropagatesError(t *testing.T) {
	repo := &stubOrphanRepo{previewErr: errors.New("boom")}
	svc := NewOrphanService(repo)
	if _, err := svc.Preview(context.Background(), domain.ProjectContext{ID: 1}); err == nil {
		t.Fatal("expected error, got nil")
	}
}
