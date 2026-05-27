package guards

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// subtasksRepo lets the subtasks_complete tests inject the response of
// FirstChildNotInBucket without standing up a sqlite store.
type subtasksRepo struct {
	openChild     domain.Task
	hasOpen       bool
	finalBucketID int64
	workflowKey   string
}

func (r *subtasksRepo) ListTaskBlockerBuckets(context.Context, int64, int64, domain.BucketResolver) ([]domain.TaskBlocker, error) {
	return nil, nil
}
func (r *subtasksRepo) CountTaskComments(context.Context, int64, int64) (int, error) {
	return 0, nil
}
func (r *subtasksRepo) CountTaskCommentsTagged(context.Context, int64, int64, string) (int, error) {
	return 0, nil
}
func (r *subtasksRepo) CountPriorWavesPending(context.Context, int64, int64, domain.BucketResolver) (int, error) {
	return 0, nil
}
func (r *subtasksRepo) FirstChildNotInBucket(_ context.Context, _ int64, _ int64, finalBucketID int64, buckets domain.BucketResolver) (domain.Task, bool, error) {
	r.finalBucketID = finalBucketID
	if buckets != nil {
		r.workflowKey = buckets.Workflow().Key
	}
	return r.openChild, r.hasOpen, nil
}

func snapWithSubtasksGuard(t *testing.T) *config.Snapshot {
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
					{From: 2, To: 3, Guards: []config.TransitionGuard{{Type: "subtasks_complete"}}},
				},
			},
		},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "wf"}},
	}
	return config.BuildSnapshot(bundle)
}

func TestEvaluateTransitionBlocksOnOpenSubtask(t *testing.T) {
	snap := snapWithSubtasksGuard(t)
	repo := &subtasksRepo{
		hasOpen: true,
		openChild: domain.Task{
			ID:        123,
			Title:     "rename var",
			BucketKey: "dev",
		},
	}
	eval := NewGuardEvaluator(snap, repo, nil)

	err := eval.EvaluateTransition(context.Background(), 1, 1, 2, 3, "done")
	if err == nil {
		t.Fatal("expected guard violation when a subtask is still open")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("error = %v, want ErrGuardViolation", err)
	}
	if !contains(coded.Message, "#123") || !contains(coded.Message, "rename var") {
		t.Fatalf("message must name the open subtask, got %q", coded.Message)
	}
}

func TestEvaluateTransitionPassesWhenAllSubtasksDone(t *testing.T) {
	snap := snapWithSubtasksGuard(t)
	repo := &subtasksRepo{hasOpen: false}
	eval := NewGuardEvaluator(snap, repo, nil)

	if err := eval.EvaluateTransition(context.Background(), 1, 1, 2, 3, "done"); err != nil {
		t.Fatalf("unexpected error when no subtask is open: %v", err)
	}
}

func TestEvaluateTransitionPassesWhenTaskHasNoChildren(t *testing.T) {
	// hasOpen=false models "no children matched the query" — guard satisfied.
	snap := snapWithSubtasksGuard(t)
	repo := &subtasksRepo{hasOpen: false}
	eval := NewGuardEvaluator(snap, repo, nil)

	if err := eval.EvaluateTransition(context.Background(), 1, 1, 2, 3, "done"); err != nil {
		t.Fatalf("unexpected error with no children: %v", err)
	}
}

func TestEvaluateTransitionSubtasksCompleteUsesChildResolvedKit(t *testing.T) {
	snap := config.BuildSnapshot(config.Bundle{
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Dev", Position: 2},
				{ID: 3, Key: "review", Name: "Review", Position: 3},
				{ID: 4, Key: "done", Name: "Done", Position: 4},
			},
			Transitions: []config.Transition{
				{From: 2, To: 3, Guards: []config.TransitionGuard{{Type: "subtasks_complete"}}},
			},
		}},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		SubtaskBundle: &config.Bundle{
			Workflows: []config.Workflow{{
				ID:   2,
				Key:  "sub",
				Name: "Sub",
				Buckets: []config.Bucket{
					{ID: 10, Key: "todo", Name: "Todo", Position: 1},
					{ID: 30, Key: "closed", Name: "Closed", Position: 2},
				},
			}},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
		},
	})
	repo := &subtasksRepo{hasOpen: false}
	eval := NewGuardEvaluator(snap, repo, nil)

	if err := eval.EvaluateTransition(context.Background(), 1, 1, 2, 3, "review"); err != nil {
		t.Fatalf("EvaluateTransition = %v", err)
	}
	if repo.finalBucketID != 30 {
		t.Fatalf("finalBucketID = %d, want sub-kit final bucket 30", repo.finalBucketID)
	}
	if repo.workflowKey != "sub" {
		t.Fatalf("resolver workflow = %q, want sub", repo.workflowKey)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || stringIndex(haystack, needle) >= 0
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
