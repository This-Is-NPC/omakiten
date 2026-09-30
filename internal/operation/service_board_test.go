package operation

import (
	"testing"

	"omakiten/internal/contract"
)

func TestTaskBoardCountsRelationsWithinProject(t *testing.T) {
	fixture := newAgentFixture(t)
	if _, err := fixture.service.AddTag(fixture.ctx, contract.AddTagInput{EntityType: "task", EntityID: fixture.taskA1.ID, TagName: "layout"}); err != nil {
		t.Fatalf("AddTag() error = %v", err)
	}

	board, err := fixture.service.TaskBoard(fixture.ctx, contract.TaskBoardInput{})
	if err != nil {
		t.Fatalf("TaskBoard() error = %v", err)
	}
	if board.Project.ID != fixture.projectA.ID || len(board.Workflow.Buckets) == 0 {
		t.Fatalf("board project/workflow = %+v / %+v", board.Project, board.Workflow)
	}
	counts := map[int64]contract.BoardTask{}
	for _, task := range board.Tasks {
		counts[task.ID] = task
	}
	if _, leaked := counts[fixture.taskB.ID]; leaked || len(board.Tasks) != 2 {
		t.Fatalf("tasks = %+v, want only project A's two tasks", board.Tasks)
	}
	if got := counts[fixture.taskA1.ID]; got.Comments != 1 || got.Blockers != 0 {
		t.Fatalf("A1 counts = %+v, want one comment", got)
	}
	if got := counts[fixture.taskA2.ID]; got.Blockers != 1 || got.Comments != 0 {
		t.Fatalf("A2 counts = %+v, want one blocker", got)
	}
	if counts[fixture.taskA1.ID].Priority == "" {
		t.Fatalf("A1 priority label missing: %+v", counts[fixture.taskA1.ID])
	}
	// The board carries the label the TUI shows, not the normalised name.
	if got := counts[fixture.taskA1.ID].Tags; len(got) != 1 || got[0] != "Layout" {
		t.Fatalf("A1 tags = %v, want the label [Layout]", got)
	}
	// The priorities a task can take, so a client can offer them.
	if len(board.Priorities) == 0 {
		t.Fatal("board priorities missing")
	}
	var found bool
	for _, priority := range board.Priorities {
		found = found || priority.Value == counts[fixture.taskA1.ID].Priority
	}
	if !found {
		t.Fatalf("priorities %+v lack A1's priority %q", board.Priorities, counts[fixture.taskA1.ID].Priority)
	}
}
