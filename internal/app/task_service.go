package app

import (
	"context"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type TaskService struct {
	repo     TaskRepository
	workflow *WorkflowService
	registry *domain.EnumRegistry
	snap     *config.Snapshot
}

// NewTaskService wires the validation/orchestration layer for tasks. workflow
// owns the policy bits (default-bucket selection on Add, transition+guards on
// Move) so the task service stays focused on input validation and delegation.
// registry must be non-nil: priority label resolution and id validation go
// through the supplied bundle-scoped EnumRegistry. snap is the per-project
// view the repo methods read bucket key↔id through; tests that drive
// state-only flows may pass nil.
func NewTaskService(repo TaskRepository, workflow *WorkflowService, registry *domain.EnumRegistry, snap *config.Snapshot) *TaskService {
	return &TaskService{repo: repo, workflow: workflow, registry: registry, snap: snap}
}

// CompositeWorkflowStore is the adapter contract a single backing store (in
// production: *sqlite.Store) must satisfy to back both the task service and
// its embedded workflow service. Defining the composite here lets callers
// avoid passing the same adapter five times.
type CompositeWorkflowStore interface {
	WorkflowRepository
	GuardEvaluationRepository
	TaskRepository
	EventRepository
}

// NewTaskServiceFromStore is the production-path sugar: it wires WorkflowService
// against the composite store and returns a TaskService ready for use.
// The registry is required and forwarded to both WorkflowService and TaskService
// so priority lookups use the bundle-scoped tables. snap is the per-project
// view threaded into both services for bucket resolution.
func NewTaskServiceFromStore(store CompositeWorkflowStore, registry *domain.EnumRegistry, snap *config.Snapshot) *TaskService {
	return NewTaskService(store, NewWorkflowServiceFromStore(store, registry, snap), registry, snap)
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
	task, err = s.createTask(ctx, project, title, description, priority, bucketKey, nil)
	return
}

// AddSub creates a task and attaches it to parentID as a sub-task in a
// single atomic INSERT. The parent must belong to the same project and
// be active; archived parents and explicit cross-bucket sub-tasks are
// rejected at the service boundary. When bucketKey is empty, the new
// sub-task inherits the parent's bucket so the "workflow herdado do
// pai" invariant holds. Anti-cycle is unnecessary for fresh rows — they
// have no descendants yet. Every call writes one activity row — even
// pre-create rejections — so failed sub-task attempts surface in Stats
// › Logs.
func (s *TaskService) AddSub(ctx context.Context, project domain.ProjectContext, parentID int64, title, description, priority, bucketKey string) (task domain.Task, err error) {
	finish := activity.Track(ctx, "app.TaskService.AddSub", project, map[string]any{"title": title, "parent_id": parentID, "bucket": bucketKey})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	if parentID <= 0 {
		err = domain.NewError(domain.ErrValidation, "parent_id must be positive", map[string]any{"parent_id": parentID})
		return
	}
	parent, err := s.taskByID(ctx, project, parentID)
	if err != nil {
		return
	}
	if parent.State != domain.TaskStateActive {
		err = domain.NewError(domain.ErrValidation,
			"parent task is archived; unarchive before attaching a sub-task",
			map[string]any{"parent_id": parentID, "parent_state": string(parent.State)})
		return
	}
	bucketKey = strings.TrimSpace(bucketKey)
	if bucketKey != "" && bucketKey != parent.BucketKey {
		err = domain.NewError(domain.ErrValidation,
			"sub-task bucket must match parent bucket; sub-tasks inherit the parent's workflow position",
			map[string]any{
				"parent_id":     parentID,
				"parent_bucket": parent.BucketKey,
				"bucket":        bucketKey,
			})
		return
	}
	if bucketKey == "" {
		bucketKey = parent.BucketKey
	}
	pid := parentID
	task, err = s.createTask(ctx, project, title, description, priority, bucketKey, &pid)
	return
}

// createTask is the shared worker for Add and AddSub. parentID nil
// creates a root task; non-nil threads the FK into the same INSERT so
// the row + parent link land atomically and the task.created event
// payload carries the parent reference from t=0. Activity tracking
// lives in the public surfaces (Add / AddSub) so the tag stays stable
// per entry point and pre-call validations still write an activity row
// on failure.
func (s *TaskService) createTask(ctx context.Context, project domain.ProjectContext, title, description, priority, bucketKey string, parentID *int64) (task domain.Task, err error) {
	title = strings.TrimSpace(title)
	if title == "" {
		err = domain.NewError(domain.ErrValidation, "task title is required", nil)
		return
	}
	priorityID, err := s.resolvePriorityInput(strings.TrimSpace(priority))
	if err != nil {
		return
	}

	task, err = s.workflow.CreateTask(ctx, project.ID, title, strings.TrimSpace(description), priorityID, strings.TrimSpace(bucketKey), parentID)
	return
}

// resolvePriorityInput accepts the user-supplied priority token (label
// or empty) and returns the configured id. Empty falls back to the
// configured default priority; non-empty is resolved via the bundle-scoped
// registry. Unknown labels error with ErrValidation so the caller surfaces
// a helpful message instead of silently writing PriorityZero.
func (s *TaskService) resolvePriorityInput(label string) (domain.Priority, error) {
	if label == "" {
		// Caller did not name a priority — defer to the configured
		// default. Storage layer will substitute it before insert.
		return domain.PriorityZero, nil
	}
	if p, ok := s.registry.PriorityFromLabel(label); ok {
		return p, nil
	}
	return domain.PriorityZero, domain.NewError(domain.ErrValidation,
		"unknown priority label; must match a value in config.priorities",
		map[string]any{"priority": label})
}

// isPriorityRegistered reports whether the given priority id is known in
// the active table.
func (s *TaskService) isPriorityRegistered(p domain.Priority) bool {
	return s.registry.IsPriorityRegistered(p)
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

	tasks, err = s.repo.ListTasks(ctx, project.ID, filter, s.snap)
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
		// Edit callers already hold a resolved Priority id (TUI cycles
		// through the configured table; CLI/MCP went through
		// resolvePriorityInput before reaching here). The service still
		// re-checks the id is registered so a stale id (priority entry
		// removed since the caller cached it) is rejected loud rather
		// than silently passed through to the store.
		if !s.isPriorityRegistered(*update.Priority) {
			err = domain.NewError(domain.ErrValidation,
				"priority id is not in config.priorities",
				map[string]any{"priority": int(*update.Priority)})
			return
		}
		changed = true
	}

	update.BucketKey = strings.TrimSpace(update.BucketKey)
	if update.BucketKey != "" {
		changed = true
	}
	if update.ChangeParent {
		changed = true
	}
	if !changed {
		err = domain.NewError(domain.ErrValidation, "at least one task update is required", nil)
		return
	}

	hasFieldEdit := update.Title != nil || update.Description != nil || update.Priority != nil
	var before domain.Task
	if hasFieldEdit {
		before, err = s.taskByID(ctx, project, taskID)
		if err != nil {
			return
		}
		if before.State == domain.TaskStateArchived {
			err = domain.NewError(domain.ErrValidation, "task is archived; unarchive before editing", map[string]any{"task_id": taskID, "hint": "call tasks.unarchive(task_id) first"})
			return
		}
		var allowed bool
		var hint string
		allowed, hint, err = s.workflow.ResolveBucketPermissions(ctx, project, taskID, EntityTask, PermissionEdit)
		if err != nil {
			return
		}
		if !allowed {
			s.workflow.Evaluator().EmitViolated(ctx, project.ID, domain.EventEntityTask, taskID,
				GuardOperationTaskEdit, GuardRulePermissions, hint,
				map[string]any{"task_id": taskID, "entity": EntityTask, "operation": PermissionEdit})
			err = domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"task_id": taskID, "hint": hint, "entity": EntityTask, "operation": PermissionEdit})
			return
		}
	}

	if update.BucketKey != "" {
		task, err = s.workflow.MoveTask(ctx, project, taskID, update.BucketKey)
		if err != nil {
			return
		}
	}
	if hasFieldEdit {
		task, err = s.repo.UpdateTask(ctx, project.ID, taskID, update, s.snap)
		if err != nil {
			return
		}
		if _, evErr := s.repo.EmitTaskEditedEvent(ctx, project.ID, taskID, before, task); evErr != nil {
			err = evErr
			return
		}
	}
	if update.ChangeParent {
		if err = s.applyParentChange(ctx, project, taskID, update.NewParentID); err != nil {
			return
		}
		if task.ID == 0 {
			task, err = s.taskByID(ctx, project, taskID)
			if err != nil {
				return
			}
		} else {
			task.ParentID = update.NewParentID
		}
	}

	return
}

// applyParentChange validates a re-parent request against the cycle
// invariant ("T.parent = P is unsafe iff P descends from T") and writes
// the column. parentID nil clears parent_id (the task becomes a root).
// Short-circuits when the requested value already matches the current
// FK so a no-op edit doesn't bump updated_at or fire an extra write.
// Rejects re-parents under an archived parent — the workflow propagates
// from parent to child, so an archived parent has no live workflow to
// inherit.
func (s *TaskService) applyParentChange(ctx context.Context, project domain.ProjectContext, taskID int64, parentID *int64) error {
	current, err := s.taskByID(ctx, project, taskID)
	if err != nil {
		return err
	}
	if domain.ParentIDEquals(current.ParentID, parentID) {
		return nil
	}
	if parentID == nil {
		return s.repo.SetTaskParent(ctx, project.ID, taskID, nil)
	}
	if *parentID == taskID {
		return domain.NewError(domain.ErrValidation, "task cannot be its own parent", map[string]any{"task_id": taskID})
	}
	parent, err := s.taskByID(ctx, project, *parentID)
	if err != nil {
		return err
	}
	if parent.State != domain.TaskStateActive {
		return domain.NewError(domain.ErrValidation,
			"parent task is archived; unarchive before re-parenting under it",
			map[string]any{"task_id": taskID, "parent_id": *parentID, "parent_state": string(parent.State)})
	}
	cycle, err := s.repo.IsDescendantOf(ctx, project.ID, *parentID, taskID)
	if err != nil {
		return err
	}
	if cycle {
		return domain.NewError(domain.ErrValidation,
			"re-parent would create a cycle: target parent is already a descendant of this task",
			map[string]any{"task_id": taskID, "parent_id": *parentID})
	}
	return s.repo.SetTaskParent(ctx, project.ID, taskID, parentID)
}

// Delete enforces the bucket-level task.delete policy and any
// operations.delete.guards before hard-deleting the task with cascade.
// Returns the system-level task.removed event whose payload carries the
// pre-delete snapshot for audit.
func (s *TaskService) Delete(ctx context.Context, project domain.ProjectContext, taskID int64) (event domain.Event, err error) {
	finish := activity.Track(ctx, "app.TaskService.Delete", project, map[string]any{"task_id": taskID})
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

	// Verify the task exists before evaluating policy and guards — see Archive
	// for rationale. Reports task_not_found correctly instead of guard_violation
	// or permission denial for a phantom row.
	if _, err = s.taskByID(ctx, project, taskID); err != nil {
		return
	}

	allowed, hint, err := s.workflow.ResolveBucketPermissions(ctx, project, taskID, EntityTask, PermissionDelete)
	if err != nil {
		return
	}
	if !allowed {
		s.workflow.Evaluator().EmitViolated(ctx, project.ID, domain.EventEntityTask, taskID,
			GuardOperationTaskDelete, GuardRulePermissions, hint,
			map[string]any{"task_id": taskID, "entity": EntityTask, "operation": PermissionDelete})
		err = domain.NewError(domain.ErrGuardViolation, hint, map[string]any{"task_id": taskID, "hint": hint, "entity": EntityTask, "operation": PermissionDelete})
		return
	}
	if err = s.workflow.Evaluator().EvaluateOperation(ctx, project.ID, taskID, OperationDelete); err != nil {
		return
	}

	event, err = s.repo.HardDeleteTask(ctx, project.ID, taskID, s.snap)
	return
}

// Archive flips the task into archived state and moves it into the workflow's
// final bucket atomically. Bypasses bucket-permission policy and transition
// guards (escape hatch) but still respects operations.archive.guards.
func (s *TaskService) Archive(ctx context.Context, project domain.ProjectContext, taskID int64) (task domain.Task, event domain.Event, err error) {
	finish := activity.Track(ctx, "app.TaskService.Archive", project, map[string]any{"task_id": taskID})
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

	// Verify the task exists before evaluating guards — guards against a
	// non-existent task would otherwise misreport as guard_violation when the
	// real failure is task_not_found.
	if _, err = s.taskByID(ctx, project, taskID); err != nil {
		return
	}

	if err = s.workflow.Evaluator().EvaluateOperation(ctx, project.ID, taskID, OperationArchive); err != nil {
		return
	}

	finalBucket, err := s.workflow.FinalBucketKey(ctx)
	if err != nil {
		return
	}

	task, event, err = s.repo.SetTaskState(ctx, project.ID, taskID, domain.TaskStateArchived, finalBucket, s.snap)
	return
}

// Assign sets the task's free-text assignee (tasks.assigned_to) to the
// trimmed argument. An empty assignee clears the column to NULL — the
// recovery path for tasks whose claiming agent crashed without finishing.
// Emits task.assigned (non-empty) or task.unassigned (empty) in the same
// transaction as the UPDATE. No-op when the new value matches the current
// one; the returned Event is zero in that case.
func (s *TaskService) Assign(ctx context.Context, project domain.ProjectContext, taskID int64, assignee string) (task domain.Task, event domain.Event, err error) {
	finish := activity.Track(ctx, "app.TaskService.Assign", project, map[string]any{"task_id": taskID, "assignee": strings.TrimSpace(assignee)})
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
	task, event, err = s.repo.AssignTask(ctx, project.ID, taskID, assignee, "cli.assign", s.snap)
	return
}

// Unarchive restores an archived task to active state, leaving its bucket
// untouched. Respects operations.unarchive.guards if declared.
func (s *TaskService) Unarchive(ctx context.Context, project domain.ProjectContext, taskID int64) (task domain.Task, event domain.Event, err error) {
	finish := activity.Track(ctx, "app.TaskService.Unarchive", project, map[string]any{"task_id": taskID})
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

	// Verify the task exists before evaluating guards — see Archive for rationale.
	if _, err = s.taskByID(ctx, project, taskID); err != nil {
		return
	}

	if err = s.workflow.Evaluator().EvaluateOperation(ctx, project.ID, taskID, OperationUnarchive); err != nil {
		return
	}

	task, event, err = s.repo.SetTaskState(ctx, project.ID, taskID, domain.TaskStateActive, "", s.snap)
	return
}

func (s *TaskService) taskByID(ctx context.Context, project domain.ProjectContext, taskID int64) (domain.Task, error) {
	tasks, err := s.repo.ListTasks(ctx, project.ID, domain.TaskFilter{IncludeArchived: true}, s.snap)
	if err != nil {
		return domain.Task{}, err
	}
	for _, t := range tasks {
		if t.ID == taskID {
			return t, nil
		}
	}
	return domain.Task{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": taskID})
}
