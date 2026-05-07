package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type TaskService struct {
	repo     TaskRepository
	workflow *WorkflowService
}

// NewTaskService wires the validation/orchestration layer for tasks. workflow
// owns the policy bits (default-bucket selection on Add, transition+guards on
// Move) so the task service stays focused on input validation and delegation.
func NewTaskService(repo TaskRepository, workflow *WorkflowService) *TaskService {
	return &TaskService{repo: repo, workflow: workflow}
}

// CompositeWorkflowStore is the adapter contract a single backing store (in
// production: *sqlite.Store) must satisfy to back both the task service and
// its embedded workflow service. Defining the composite here lets callers
// avoid passing the same adapter five times.
type CompositeWorkflowStore interface {
	ConfigRepository
	WorkflowRepository
	GuardEvaluationRepository
	TaskRepository
	EventRepository
}

// NewTaskServiceFromStore is the production-path sugar: it wires WorkflowService
// against the composite store and returns a TaskService ready for use.
func NewTaskServiceFromStore(store CompositeWorkflowStore) *TaskService {
	return NewTaskService(store, NewWorkflowServiceFromStore(store))
}

func (s *TaskService) Add(ctx context.Context, project domain.ProjectContext, title, description, priority, bucketKey string) (task domain.Task, err error) {
	finish := activity.Track(ctx, "app.TaskService.Add", project, map[string]any{"title": title, "priority": priority, "bucket": bucketKey})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	title = strings.TrimSpace(title)
	if title == "" {
		err = domain.NewError(domain.ErrValidation, "task title is required", nil)
		return
	}
	priorityValue := domain.Priority(strings.TrimSpace(priority))
	if priorityValue == "" {
		priorityValue = domain.PriorityNormal
	}
	switch priorityValue {
	case domain.PriorityLow, domain.PriorityNormal, domain.PriorityHigh:
	default:
		err = domain.NewError(domain.ErrValidation, "priority must be low, normal, or high", map[string]any{"priority": priority})
		return
	}

	task, err = s.workflow.CreateTask(ctx, project.ID, title, strings.TrimSpace(description), string(priorityValue), strings.TrimSpace(bucketKey))
	return
}

func (s *TaskService) List(ctx context.Context, project domain.ProjectContext, filter domain.TaskFilter) (tasks []domain.Task, err error) {
	finish := activity.Track(ctx, "app.TaskService.List", project, map[string]any{"bucket": filter.BucketKey})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	tasks, err = s.repo.ListTasks(ctx, project.ID, filter)
	return
}

func (s *TaskService) Move(ctx context.Context, project domain.ProjectContext, taskID int64, targetBucketKey string) (domain.Task, error) {
	return s.workflow.MoveTask(ctx, project, taskID, targetBucketKey)
}

func (s *TaskService) Edit(ctx context.Context, project domain.ProjectContext, taskID int64, update domain.TaskUpdate) (task domain.Task, err error) {
	finish := activity.Track(ctx, "app.TaskService.Edit", project, map[string]any{"task_id": taskID})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if taskID <= 0 {
		err = domain.NewError(domain.ErrValidation, "task id must be positive", nil)
		return
	}

	changed := false
	if update.Title != nil {
		title := strings.TrimSpace(*update.Title)
		if title == "" {
			err = domain.NewError(domain.ErrValidation, "task title is required", nil)
			return
		}
		update.Title = &title
		changed = true
	}
	if update.Description != nil {
		description := strings.TrimSpace(*update.Description)
		update.Description = &description
		changed = true
	}
	if update.Priority != nil {
		priority := domain.Priority(strings.TrimSpace(string(*update.Priority)))
		switch priority {
		case domain.PriorityLow, domain.PriorityNormal, domain.PriorityHigh:
			update.Priority = &priority
		default:
			err = domain.NewError(domain.ErrValidation, "priority must be low, normal, or high", map[string]any{"priority": *update.Priority})
			return
		}
		changed = true
	}

	update.BucketKey = strings.TrimSpace(update.BucketKey)
	if update.BucketKey != "" {
		changed = true
	}
	if !changed {
		err = domain.NewError(domain.ErrValidation, "at least one task update is required", nil)
		return
	}

	if update.BucketKey != "" {
		task, err = s.workflow.MoveTask(ctx, project, taskID, update.BucketKey)
		if err != nil {
			return
		}
	}
	if update.Title != nil || update.Description != nil || update.Priority != nil {
		task, err = s.repo.UpdateTask(ctx, project.ID, taskID, update)
		if err != nil {
			return
		}
	}

	return
}
