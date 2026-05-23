package guards

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// stubRepo lets evaluator tests inject specific counts without standing
// up a sqlite store. Only the counts the wave_gate path consults are
// implemented; every other method satisfies the Repository contract
// with zero-value answers.
type stubRepo struct {
	priorWaves int
}

func (r *stubRepo) ListTaskBlockerBuckets(context.Context, int64, int64, domain.BucketResolver) ([]domain.TaskBlocker, error) {
	return nil, nil
}
func (r *stubRepo) CountTaskComments(context.Context, int64, int64) (int, error) {
	return 0, nil
}
func (r *stubRepo) CountTaskCommentsTagged(context.Context, int64, int64, string) (int, error) {
	return 0, nil
}
func (r *stubRepo) CountPriorWavesPending(context.Context, int64, int64, domain.BucketResolver) (int, error) {
	return r.priorWaves, nil
}
func (r *stubRepo) FirstChildNotInBucket(context.Context, int64, int64, int64, domain.BucketResolver) (domain.Task, bool, error) {
	return domain.Task{}, false, nil
}

func snapWithWaveGateGuard(t *testing.T) *config.Snapshot {
	t.Helper()
	bundle := config.Bundle{
		Workflows: []config.Workflow{
			{
				ID:   1,
				Key:  "wf",
				Name: "WF",
				Buckets: []config.Bucket{
					{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
					{ID: 2, Key: "dev", Name: "Dev", Position: 2},
					{ID: 3, Key: "done", Name: "Done", Position: 3},
				},
				Transitions: []config.Transition{
					{From: 1, To: 2, Guards: []config.TransitionGuard{{Type: "wave_gate", Hint: "earlier wave still pending"}}},
				},
			},
		},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "wf"}},
	}
	return config.BuildSnapshot(bundle)
}

func TestEvaluateTransitionBlocksOnWaveGateWhenPriorWavesPending(t *testing.T) {
	snap := snapWithWaveGateGuard(t)
	repo := &stubRepo{priorWaves: 2}
	eval := NewGuardEvaluator(snap, repo, nil)

	err := eval.EvaluateTransition(context.Background(), 1, 99, 1, 2, "dev")
	if err == nil {
		t.Fatal("expected guard violation when prior waves pending")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("error = %v, want ErrGuardViolation", err)
	}
}

func TestEvaluateTransitionPassesWaveGateWhenPriorWavesEmpty(t *testing.T) {
	snap := snapWithWaveGateGuard(t)
	repo := &stubRepo{priorWaves: 0}
	eval := NewGuardEvaluator(snap, repo, nil)

	if err := eval.EvaluateTransition(context.Background(), 1, 99, 1, 2, "dev"); err != nil {
		t.Fatalf("unexpected error when wave gate clear: %v", err)
	}
}
