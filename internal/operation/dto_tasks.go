package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// taskSummary projects a domain.Task into the delivery contract, resolving the
// priority label via the supplied registry. registry is nil-safe — Priority
// is left empty when no registry is available, so consumers can still parse
// the rest of the shape.
func taskSummary(task domain.Task, registry *domain.EnumRegistry) contract.TaskSummary {
	s := contract.TaskSummary{
		ID:          task.ID,
		Title:       task.Title,
		Description: task.Description,
		BucketKey:   task.BucketKey,
		Priority:    registry.PriorityLabel(task.Priority),
		ParentID:    task.ParentID,
	}
	if task.State != "" && task.State != domain.TaskStateActive {
		s.State = string(task.State)
	}
	return s
}
