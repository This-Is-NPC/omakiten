package guards

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// TestGuardViolationCarriesFix verifies an unmet guard names its type, its
// task, and the command that satisfies it, with the author matching the caller.
func TestGuardViolationCarriesFix(t *testing.T) {
	snap := cascadeIsolationSnap(t)
	eval := NewGuardEvaluator(snap, &subtasksRepo{}, nil)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"user", context.Background(), "okt comment add 5 -T root-only -b '<evidence>'"},
		{"agent", activity.WithAgent(context.Background(), "cli", "okt", "model-x", "s1"), "okt comment add 5 -a agent -T root-only -b '<evidence>'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := eval.EvaluateTransitionForTask(tc.ctx, 1, domain.Task{ID: 5}, 2, 3, "review", snap)
			var coded *domain.CodedError
			if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
				t.Fatalf("error = %v, want guard_violation", err)
			}
			if coded.Details["guard_type"] != "comments_tagged" || coded.Details["task_id"] != int64(5) || coded.Details["fix"] != tc.want {
				t.Fatalf("details = %+v, want guard_type comments_tagged, task_id 5, fix %q", coded.Details, tc.want)
			}
		})
	}
}

// TestEvaluateReportsEveryUnmetGuard verifies a move checks all guards: each
// unmet guard records its own event and the error lists them all, while a
// preview reports the same state without recording anything.
func TestEvaluateReportsEveryUnmetGuard(t *testing.T) {
	snap := cascadeIsolationSnap(t)
	specs := []domain.TransitionGuard{
		{Type: "comments_tagged", Tag: "self-branch", Count: 1},
		{Type: "blockers_in", Buckets: []string{"done"}},
		{Type: "comments_min", Count: 2},
	}
	recorder := &fakeViolatedRecorder{}
	eval := NewGuardEvaluator(snap, &subtasksRepo{}, recorder)
	task := domain.Task{ID: 5}

	err := eval.runGuardsWithSnapshot(context.Background(), 1, task, specs, OperationTaskTransition, nil, snap)
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrGuardViolation {
		t.Fatalf("error = %v, want guard_violation", err)
	}
	unmet, _ := coded.Details["unmet"].([]domain.GuardStatus)
	if len(unmet) != 2 || unmet[0].Type != "comments_tagged" || unmet[1].Type != "comments_min" {
		t.Fatalf("unmet = %+v, want comments_tagged and comments_min", unmet)
	}
	if coded.Details["guard_type"] != "comments_tagged" {
		t.Fatalf("guard_type = %v, want the first unmet guard", coded.Details["guard_type"])
	}
	if len(recorder.captured) != 2 {
		t.Fatalf("recorded %d guard.violated events, want one per unmet guard", len(recorder.captured))
	}

	recorder.captured = nil
	statuses, err := eval.evaluate(context.Background(), 1, task, specs, snap)
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if len(statuses) != 3 || statuses[0].Satisfied || !statuses[1].Satisfied || statuses[2].Satisfied {
		t.Fatalf("statuses = %+v, want unmet, met, unmet", statuses)
	}
	if statuses[1].Fix != "" || statuses[0].Fix == "" {
		t.Fatalf("fix must be set only on unmet guards: %+v", statuses)
	}
	if len(recorder.captured) != 0 {
		t.Fatalf("evaluate recorded %d events, want none", len(recorder.captured))
	}
}
