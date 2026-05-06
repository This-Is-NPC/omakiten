package app

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

// WorkflowService owns workflow policy: which bucket a freshly-created task
// lands in, whether a move is allowed by the configured transitions, whether
// the move's guards are satisfied, and whether a move into the final bucket
// should additionally emit task.completed. The adapter layer (sqlite) only
// supplies the underlying primitives; nothing about workflow rules lives in
// the Store any longer.
type WorkflowService struct {
	config ConfigRepository
	repo   WorkflowRepository
	guards GuardEvaluationRepository
	tasks  TaskRepository
	events EventRepository
}

func NewWorkflowService(config ConfigRepository, workflow WorkflowRepository, guards GuardEvaluationRepository, tasks TaskRepository, events EventRepository) *WorkflowService {
	return &WorkflowService{config: config, repo: workflow, guards: guards, tasks: tasks, events: events}
}

// NewWorkflowServiceFromStore is the production-path sugar for callers that
// hold a single composite store implementing every workflow port (in
// production: *sqlite.Store).
func NewWorkflowServiceFromStore(store CompositeWorkflowStore) *WorkflowService {
	return NewWorkflowService(store, store, store, store, store)
}

// ResolveDefaultBucket returns the key of the first bucket in the active
// workflow — the bucket new tasks land in when callers do not specify one.
// Errors with ErrConfigInvalid if the active workflow has no buckets, so
// upstream callers can default to "" without baking "backlog" into prod.
func (s *WorkflowService) ResolveDefaultBucket(ctx context.Context) (string, error) {
	workflow, err := s.config.ActiveWorkflow(ctx)
	if err != nil {
		return "", err
	}
	if len(workflow.Buckets) == 0 {
		return "", domain.NewError(domain.ErrConfigInvalid, "active workflow has no buckets", nil)
	}
	return workflow.Buckets[0].Key, nil
}

// CreateTask is the policy-bearing wrapper around TaskRepository.CreateTask:
// it resolves the default bucket when bucketKey is empty, then delegates to
// the persister. The store still emits task.created in the same transaction.
func (s *WorkflowService) CreateTask(ctx context.Context, projectID int64, title, description, priority, bucketKey string) (domain.Task, error) {
	bucketKey = strings.TrimSpace(bucketKey)
	if bucketKey == "" {
		key, err := s.ResolveDefaultBucket(ctx)
		if err != nil {
			return domain.Task{}, err
		}
		bucketKey = key
	}
	return s.tasks.CreateTask(ctx, projectID, title, description, priority, bucketKey)
}

// MoveTask runs the full move policy: resolve current/target buckets, enforce
// the configured transition + guards, persist the move via TaskRepository
// (which records task.moved), then conditionally emit task.completed when the
// destination is the workflow's final bucket. Returns the persisted task.
//
// Atomicity note: the policy checks happen outside the persistence
// transaction because the underlying ports are coarse-grained. In a
// single-user CLI/TUI workload that is fine — there are no concurrent writers
// that could change the relevant state between check and write.
func (s *WorkflowService) MoveTask(ctx context.Context, project domain.ProjectContext, taskID int64, targetBucketKey string) (task domain.Task, err error) {
	finish := activity.Track(ctx, "app.WorkflowService.MoveTask", project, map[string]any{"task_id": taskID, "bucket": targetBucketKey})
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
	targetBucketKey = strings.TrimSpace(targetBucketKey)
	if targetBucketKey == "" {
		err = domain.NewError(domain.ErrValidation, "target bucket is required", nil)
		return
	}

	currentBucketID, _, err := s.repo.CurrentTaskBucket(ctx, project.ID, taskID)
	if err != nil {
		return
	}

	target, err := s.repo.ResolveActiveBucket(ctx, targetBucketKey)
	if err != nil {
		return
	}

	if currentBucketID != target.ID {
		var allowed bool
		allowed, err = s.repo.TransitionAllowed(ctx, currentBucketID, target.ID)
		if err != nil {
			return
		}
		if !allowed {
			err = domain.NewError(domain.ErrWorkflowInvalidTransition, "transition not allowed", map[string]any{"task_id": taskID, "from": currentBucketID, "to": target.ID})
			return
		}
		if err = s.evaluateGuards(ctx, project.ID, taskID, currentBucketID, target.ID); err != nil {
			return
		}
	}

	task, err = s.tasks.MoveTask(ctx, project.ID, taskID, targetBucketKey)
	if err != nil {
		return
	}

	if currentBucketID != target.ID {
		var isFinal bool
		isFinal, err = s.repo.IsFinalActiveBucket(ctx, target.ID)
		if err != nil {
			return
		}
		if isFinal {
			payload := fmt.Sprintf(`{"bucket":%q}`, targetBucketKey)
			if _, err = s.events.RecordTaskEvent(ctx, project.ID, taskID, domain.EventTypeTaskCompleted, "", payload); err != nil {
				return
			}
		}
	}
	return
}

// evaluateGuards runs each guard attached to the (from, to) transition.
// Order follows declaration order; the first violation short-circuits.
func (s *WorkflowService) evaluateGuards(ctx context.Context, projectID, taskID, fromBucketID, toBucketID int64) error {
	guards, err := s.repo.LoadTransitionGuards(ctx, fromBucketID, toBucketID)
	if err != nil {
		return err
	}
	for _, guard := range guards {
		switch guard.Type {
		case "blockers_in":
			if err := s.checkBlockersIn(ctx, projectID, taskID, guard.Buckets, guard.Hint); err != nil {
				return err
			}
		case "comments_min":
			if err := s.checkCommentsMin(ctx, projectID, taskID, guard.Count, guard.Hint); err != nil {
				return err
			}
		case "comments_tagged":
			if err := s.checkCommentsTagged(ctx, projectID, taskID, guard.Tag, guard.Count, guard.Hint); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *WorkflowService) checkBlockersIn(ctx context.Context, projectID, taskID int64, allowedKeys []string, hint string) error {
	blockers, err := s.guards.ListTaskBlockerBuckets(ctx, projectID, taskID)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, k := range allowedKeys {
		allowed[k] = struct{}{}
	}
	var pending []string
	for _, b := range blockers {
		if _, ok := allowed[b.BucketKey]; !ok {
			pending = append(pending, fmt.Sprintf("#%d %q (in %q)", b.TaskID, b.Title, b.BucketKey))
		}
	}
	if len(pending) == 0 {
		return nil
	}
	msg := "blockers_in guard: pending blockers: " + strings.Join(pending, ", ")
	details := map[string]any{"pending_blockers": pending}
	if hint != "" {
		msg += ". Hint: " + hint
		details["hint"] = hint
	}
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (s *WorkflowService) checkCommentsMin(ctx context.Context, projectID, taskID int64, minCount int, hint string) error {
	count, err := s.guards.CountTaskComments(ctx, projectID, taskID)
	if err != nil {
		return err
	}
	if count >= minCount {
		return nil
	}
	msg := fmt.Sprintf("comments_min guard: task has %d comment(s); transition requires at least %d", count, minCount)
	details := map[string]any{"count": count, "required": minCount}
	if hint != "" {
		msg += ". Hint: " + hint
		details["hint"] = hint
	}
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (s *WorkflowService) checkCommentsTagged(ctx context.Context, projectID, taskID int64, tag string, minCount int, hint string) error {
	if minCount < 1 {
		minCount = 1
	}
	count, err := s.guards.CountTaskCommentsTagged(ctx, projectID, taskID, tag)
	if err != nil {
		return err
	}
	if count >= minCount {
		return nil
	}
	msg := fmt.Sprintf("comments_tagged guard: task has %d comment(s) tagged %q; transition requires at least %d", count, tag, minCount)
	details := map[string]any{"count": count, "required": minCount, "tag": tag}
	if hint != "" {
		msg += ". Hint: " + hint
		details["hint"] = hint
	}
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}
