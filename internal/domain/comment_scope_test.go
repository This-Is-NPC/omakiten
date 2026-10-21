package domain

import (
	"errors"
	"testing"
)

// TestValidateCommentScopeTarget pins the single scope→target rule the comment
// handlers delegate to: task scope needs a task id, plan scope needs a plan,
// project/universal reject both, and an unknown scope is rejected. hasTaskID
// distinguishes "no task id supplied" from "task id 0".
func TestValidateCommentScopeTarget(t *testing.T) {
	cases := []struct {
		name      string
		scope     string
		taskID    int64
		hasTaskID bool
		hasPlan   bool
		wantErr   bool
	}{
		{"task with id", CommentScopeTask, 7, true, false, false},
		{"task without id", CommentScopeTask, 0, false, false, true},
		{"task with zero id flagged present", CommentScopeTask, 0, true, false, true},
		{"task with plan rejected", CommentScopeTask, 7, true, true, true},
		{"plan with plan", CommentScopePlan, 0, false, true, false},
		{"plan without plan", CommentScopePlan, 0, false, false, true},
		{"plan with id rejected", CommentScopePlan, 7, true, true, true},
		{"project without id", CommentScopeProject, 0, false, false, false},
		{"project with id rejected", CommentScopeProject, 7, true, false, true},
		{"project with plan rejected", CommentScopeProject, 0, false, true, true},
		{"universal without id", CommentScopeUniversal, 0, false, false, false},
		{"universal with id rejected", CommentScopeUniversal, 7, true, false, true},
		{"unknown scope rejected", "bogus", 0, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertCommentScopeValidation(t, tc.scope, tc.taskID, tc.hasTaskID, tc.hasPlan, tc.wantErr)
		})
	}
}

func assertCommentScopeValidation(t *testing.T, scope string, taskID int64, hasTaskID, hasPlan, wantErr bool) {
	t.Helper()
	err := ValidateCommentScopeTarget(scope, taskID, hasTaskID, hasPlan)
	if wantErr && err == nil {
		t.Fatalf("ValidateCommentScopeTarget(%q, %d, %v, %v) = nil, want error", scope, taskID, hasTaskID, hasPlan)
	}
	if !wantErr && err != nil {
		t.Fatalf("ValidateCommentScopeTarget(%q, %d, %v, %v) = %v, want nil", scope, taskID, hasTaskID, hasPlan, err)
	}
	if !wantErr {
		return
	}
	var coded *CodedError
	if !errors.As(err, &coded) || coded.Code != ErrValidation {
		t.Fatalf("error = %v, want coded ErrValidation", err)
	}
}
