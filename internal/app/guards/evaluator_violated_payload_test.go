package guards

import (
	"context"
	"encoding/json"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// fakeViolatedRecorder captures guard.violated emissions so the
// tests can inspect the payload shape. The test only exercises
// metadata wiring — there is no SQL persistence involved.
type fakeViolatedRecorder struct {
	captured []domain.Event
}

func (r *fakeViolatedRecorder) RecordEntityEvent(_ context.Context, entityType string, entityID, projectID int64, eventType, payload string) error {
	r.captured = append(r.captured, domain.Event{
		EntityType: entityType,
		EntityID:   entityID,
		ProjectID:  projectID,
		EventType:  eventType,
		Payload:    payload,
	})
	return nil
}

// TestEmitViolatedForTask_CarriesSubjectMetadata pins task #301 review
// §11557 finding A4: every task-scoped guard.violated payload must
// carry `subject_task_id`, `subject_parent_id`, `subject_depth`, and
// `resolved_kit` so the hooks engine can route the event to the right
// depth-specific hook (root vs sub-kit) and notification catalog.
func TestEmitViolatedForTask_CarriesSubjectMetadata(t *testing.T) {
	snap := config.BuildSnapshot(config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			ID:      1,
			Key:     "root",
			Name:    "Root",
			Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
		}},
		SubtaskBundle: &config.Bundle{
			Kit:    config.Kit{Key: "sub"},
			Config: config.Settings{Workflow: config.WorkflowSettings{Active: "sub"}},
			Workflows: []config.Workflow{{
				ID:      2,
				Key:     "sub",
				Name:    "Sub",
				Buckets: []config.Bucket{{ID: 10, Key: "todo", Name: "Todo", Position: 1}},
			}},
		},
	})

	parentID := int64(42)
	subTask := domain.Task{ID: 7, ParentID: &parentID, Depth: 1, BucketKey: "todo"}
	recorder := &fakeViolatedRecorder{}
	eval := NewGuardEvaluator(snap, nil, recorder)

	eval.EmitViolatedForTask(context.Background(), 1, subTask, snap.For(subTask),
		OperationTaskTransition, RuleTransition, "transition not allowed",
		map[string]any{"task_id": subTask.ID, "from_bucket_id": int64(10), "to_bucket_id": int64(20)})

	if len(recorder.captured) != 1 {
		t.Fatalf("expected 1 captured emission, got %d", len(recorder.captured))
	}
	ev := recorder.captured[0]
	if ev.EventType != domain.EventTypeGuardViolated {
		t.Fatalf("EventType = %q, want guard.violated", ev.EventType)
	}
	if ev.EntityType != domain.EventEntityTask || ev.EntityID != subTask.ID {
		t.Fatalf("entity = (%s,%d), want (task,%d)", ev.EntityType, ev.EntityID, subTask.ID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if got := int64(payload["subject_task_id"].(float64)); got != subTask.ID {
		t.Fatalf("subject_task_id = %d, want %d (payload %+v)", got, subTask.ID, payload)
	}
	if got := int(payload["subject_depth"].(float64)); got != 1 {
		t.Fatalf("subject_depth = %d, want 1 (sub-task) (payload %+v)", got, payload)
	}
	if got := int64(payload["subject_parent_id"].(float64)); got != parentID {
		t.Fatalf("subject_parent_id = %d, want %d (payload %+v)", got, parentID, payload)
	}
	if got := payload["resolved_kit"]; got != "sub" {
		t.Fatalf("resolved_kit = %v, want sub (sub-task resolves to sub-kit) (payload %+v)", got, payload)
	}
	if got := payload["operation"]; got != OperationTaskTransition {
		t.Fatalf("operation = %v, want %q (payload %+v)", got, OperationTaskTransition, payload)
	}
	if got := payload["rule"]; got != RuleTransition {
		t.Fatalf("rule = %v, want %q (payload %+v)", got, RuleTransition, payload)
	}
}

// TestEmitViolatedForTask_RootTaskDepthZero ensures the helper also
// stamps root-task emissions with depth=0 + the root kit identity, so
// depth-aware hooks scoped to `SubjectDepthRoot` continue to match.
func TestEmitViolatedForTask_RootTaskDepthZero(t *testing.T) {
	snap := config.BuildSnapshot(config.Bundle{
		Kit:    config.Kit{Key: "root"},
		Config: config.Settings{Workflow: config.WorkflowSettings{Active: "root"}},
		Workflows: []config.Workflow{{
			ID:      1,
			Key:     "root",
			Name:    "Root",
			Buckets: []config.Bucket{{ID: 1, Key: "backlog", Name: "Backlog", Position: 1}},
		}},
	})
	rootTask := domain.Task{ID: 5, Depth: 0, BucketKey: "backlog"}
	recorder := &fakeViolatedRecorder{}
	eval := NewGuardEvaluator(snap, nil, recorder)

	eval.EmitViolatedForTask(context.Background(), 1, rootTask, snap.For(rootTask),
		OperationTaskArchive, RulePermissions, "policy denied", nil)

	if len(recorder.captured) != 1 {
		t.Fatalf("emissions=%d, want 1", len(recorder.captured))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(recorder.captured[0].Payload), &payload); err != nil {
		t.Fatalf("payload not JSON: %v", err)
	}
	if got := int(payload["subject_depth"].(float64)); got != 0 {
		t.Fatalf("subject_depth = %d, want 0 (root task)", got)
	}
	if payload["subject_parent_id"] != nil {
		t.Fatalf("subject_parent_id = %v, want nil (root task)", payload["subject_parent_id"])
	}
	if got := payload["resolved_kit"]; got != "root" {
		t.Fatalf("resolved_kit = %v, want root", got)
	}
}
