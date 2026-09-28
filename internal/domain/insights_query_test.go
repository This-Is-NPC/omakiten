package domain

import "testing"

func TestStuckBucketIDsPreservesWorkflowTriState(t *testing.T) {
	if got := StuckBucketIDs(Workflow{}); got != nil {
		t.Fatalf("unresolved workflow = %v, want nil", got)
	}

	workflow := Workflow{Buckets: []Bucket{{ID: 1, Key: "backlog"}, {ID: 2, Key: "done"}}}
	got := StuckBucketIDs(workflow)
	if got == nil {
		t.Fatal("resolved workflow returned nil, want authoritative empty or populated ids")
	}
}
