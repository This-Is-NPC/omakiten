package domain

import (
	"errors"
	"testing"
)

// TestValidateCommentScopeTaskID pins the single scope→task_id rule both the
// CLI and the agent delegate to: task scope needs a task id, project/universal
// reject one, and an unknown scope is rejected. hasTaskID distinguishes "no
// task id supplied" from "task id 0", reconciling the CLI's arg-presence check
// with the agent's TaskID>0 check.
func TestValidateCommentScopeTaskID(t *testing.T) {
	cases := []struct {
		name      string
		scope     string
		taskID    int64
		hasTaskID bool
		wantErr   bool
	}{
		{"task with id", CommentScopeTask, 7, true, false},
		{"task without id", CommentScopeTask, 0, false, true},
		{"task with zero id flagged present", CommentScopeTask, 0, true, true},
		{"project without id", CommentScopeProject, 0, false, false},
		{"project with id rejected", CommentScopeProject, 7, true, true},
		{"universal without id", CommentScopeUniversal, 0, false, false},
		{"universal with id rejected", CommentScopeUniversal, 7, true, true},
		{"unknown scope rejected", "bogus", 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCommentScopeTaskID(tc.scope, tc.taskID, tc.hasTaskID)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateCommentScopeTaskID(%q, %d, %v) = nil, want error", tc.scope, tc.taskID, tc.hasTaskID)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateCommentScopeTaskID(%q, %d, %v) = %v, want nil", tc.scope, tc.taskID, tc.hasTaskID, err)
			}
			if tc.wantErr {
				var coded *CodedError
				if !errors.As(err, &coded) || coded.Code != ErrValidation {
					t.Fatalf("error = %v, want coded ErrValidation", err)
				}
			}
		})
	}
}
