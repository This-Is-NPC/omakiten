package domain

import "testing"

func TestTaskIsSubTask(t *testing.T) {
	t.Parallel()

	root := Task{ID: 1}
	if root.IsSubTask() {
		t.Fatalf("root task (ParentID nil) reported IsSubTask=true")
	}

	parentID := int64(42)
	child := Task{ID: 2, ParentID: &parentID}
	if !child.IsSubTask() {
		t.Fatalf("child task (ParentID=42) reported IsSubTask=false")
	}
}
