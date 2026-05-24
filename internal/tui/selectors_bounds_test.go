package tui

import (
	"testing"

	"omakiten/internal/domain"
)

// TestSelectedTaskOnBoardGuardsNegativeColIdx pins the W8 #230 mirror
// from render_board.go's focusedBucketKey guard: a negative colIdx
// must not panic; the selectedTask + tasksInCurrentBucket paths
// previously only checked the upper bound, so a stale -1 from a focus
// reset would index the buckets slice with [-1] and crash.
func TestSelectedTaskOnBoardGuardsNegativeColIdx(t *testing.T) {
	cases := []struct {
		name   string
		colIdx int
	}{
		{"negative", -1},
		{"zero with empty buckets", 0},
		{"over-bound", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{
				top:    topTasks,
				sub:    subBoard,
				colIdx: tc.colIdx,
			}
			// "zero with empty buckets" leaves m.workflow.Buckets nil,
			// matching the existing len==0 guard branch.
			if tc.name == "over-bound" {
				m.workflow = domain.Workflow{Buckets: []domain.Bucket{{ID: 1, Key: "backlog"}}}
			}
			_, ok := m.selectedTask()
			if ok {
				t.Fatalf("selectedTask returned ok=true for colIdx=%d (empty/oob)", tc.colIdx)
			}
			if got := m.tasksInCurrentBucket(); got != nil {
				t.Fatalf("tasksInCurrentBucket returned %v for colIdx=%d, want nil", got, tc.colIdx)
			}
		})
	}
}
