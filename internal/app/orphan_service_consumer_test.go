package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// consumerStubOrphanRepo lets the consumer test drive Migrate without bringing
// up a real sqlite store. The error and report values are returned
// verbatim so the test can exercise both success + failure paths.
type consumerStubOrphanRepo struct {
	rebindErr error
}

func (s consumerStubOrphanRepo) PreviewOrphanedTasks(context.Context, int64, domain.BucketResolver, domain.BucketResolver) (domain.OrphanReport, error) {
	return domain.OrphanReport{}, nil
}

func (s consumerStubOrphanRepo) RebindOrphanedTasks(context.Context, int64, domain.BucketResolver, domain.BucketResolver) (domain.OrphanReport, error) {
	return domain.OrphanReport{}, s.rebindErr
}

func (s consumerStubOrphanRepo) PreviewOrphanedCascade(context.Context, int64, domain.OrphanCascadePlan) (domain.OrphanReport, error) {
	return domain.OrphanReport{}, nil
}

func (s consumerStubOrphanRepo) RebindOrphanedCascade(context.Context, int64, domain.OrphanCascadePlan) (domain.OrphanReport, error) {
	return domain.OrphanReport{}, s.rebindErr
}

// TestOrphanServiceMigrateFiresConsumerAndClearsPrevious pins the
// W7 #228 lifetime fix: after a successful Migrate the consumer
// callback runs exactly once and the previous-snapshot pointer on
// the service is nil so a follow-up Preview / Migrate degrades to
// the "no previous" path.
func TestOrphanServiceMigrateFiresConsumerAndClearsPrevious(t *testing.T) {
	current := config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{{ID: 1, Key: "w", Name: "W", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "w"}},
	})
	previous := config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{{ID: 1, Key: "w", Name: "W", Buckets: []config.Bucket{{ID: 1, Key: "todo", Name: "Todo", Position: 1}}}},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "w"}},
	})

	svc := NewOrphanService(consumerStubOrphanRepo{}, current, previous)
	calls := 0
	svc.SetMigrateConsumer(func() { calls++ })

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if calls != 1 {
		t.Fatalf("consumer fired %d times, want 1", calls)
	}
	if svc.previous != nil {
		t.Fatalf("previous snapshot not cleared after Migrate")
	}

	// Re-run with previous nil — should not fire the consumer again.
	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err != nil {
		t.Fatalf("Migrate second call: %v", err)
	}
	if calls != 2 {
		t.Fatalf("consumer fires on every successful Migrate; got %d after second call", calls)
	}
}

// TestOrphanServiceMigrateSkipsConsumerOnFailure asserts the
// consumer does not fire when the repository surfaces an error —
// the cache must not drop the previous snapshot if the rebind
// itself failed, so the operator can retry.
func TestOrphanServiceMigrateSkipsConsumerOnFailure(t *testing.T) {
	current := config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{{ID: 1, Key: "w", Name: "W", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}}}},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "w"}},
	})
	previous := config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{{ID: 1, Key: "w", Name: "W", Buckets: []config.Bucket{{ID: 1, Key: "todo", Name: "Todo", Position: 1}}}},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "w"}},
	})
	svc := NewOrphanService(consumerStubOrphanRepo{rebindErr: errors.New("boom")}, current, previous)
	calls := 0
	svc.SetMigrateConsumer(func() { calls++ })

	if _, err := svc.Migrate(context.Background(), domain.ProjectContext{ID: 1}); err == nil {
		t.Fatalf("Migrate should error")
	}
	if calls != 0 {
		t.Fatalf("consumer fired on failure (%d times); expected 0", calls)
	}
	if svc.previous == nil {
		t.Fatalf("previous snapshot cleared on failure; operator cannot retry")
	}
}
