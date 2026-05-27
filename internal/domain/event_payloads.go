package domain

import "encoding/json"

// NewTaskSubjectPayload composes the task-scoped event JSON used across
// the storage and app layers. Subject metadata (task id, parent id,
// depth, resolved kit) is added on top of the caller-supplied
// event-specific fields. The shape is the contract every downstream
// audit consumer (events table queries, hook payload templates, MCP
// adapters) reads against, so the JSON keys live in one place here
// instead of being reassembled per call site.
//
// Helpers in `sqlite/events.go::taskEventPayload` and
// `app/workflow_service.go::taskSubjectPayload` previously duplicated
// this logic — #297 review opportunity §D.17 / #299 §B consolidates
// both into this single domain function.
//
// fields is shallow-copied before mutation so callers that reuse the
// map across calls do not see subject keys leak between invocations
// (preserves #297 review finding §B.7).
//
// task.Depth is read directly from the persisted column (migration
// 028 / #299 §A); pre-persistence call sites that did not have a
// loaded task should pass a zero-value depth — the helper does not
// recompute the chain.
func NewTaskSubjectPayload(task Task, kitKey string, fields map[string]any) (string, error) {
	merged := make(map[string]any, len(fields)+4)
	for k, v := range fields {
		merged[k] = v
	}
	merged["subject_task_id"] = task.ID
	if task.ParentID != nil {
		merged["subject_parent_id"] = *task.ParentID
	} else {
		merged["subject_parent_id"] = nil
	}
	merged["subject_depth"] = task.Depth
	merged["resolved_kit"] = kitKey
	body, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
