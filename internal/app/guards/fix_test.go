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

func TestShellWord(t *testing.T) {
	for in, want := range map[string]string{
		"self-branch": "self-branch",
		"a b":         "'a b'",
		"it's":        `'it'\''s'`,
		"":            "''",
	} {
		if got := shellWord(in); got != want {
			t.Fatalf("shellWord(%q) = %q, want %q", in, got, want)
		}
	}
}
