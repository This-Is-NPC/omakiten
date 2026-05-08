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

// FinalBucketKey returns the key of the highest-position bucket in the active
// workflow — the destination for archive operations. Errors with
// ErrConfigInvalid if the active workflow has no buckets.
func (s *WorkflowService) FinalBucketKey(ctx context.Context) (string, error) {
	workflow, err := s.config.ActiveWorkflow(ctx)
	if err != nil {
		return "", err
	}
	if len(workflow.Buckets) == 0 {
		return "", domain.NewError(domain.ErrConfigInvalid, "active workflow has no buckets", nil)
	}
	final := workflow.Buckets[0]
	for _, b := range workflow.Buckets {
		if b.Position > final.Position {
			final = b
		}
	}
	return final.Key, nil
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

// OperationArchive / OperationDelete / OperationUnarchive label the
// non-transition operations whose guards are evaluated by the workflow
// service. Symbolic constants instead of bare strings so callers and the
// workflow Operations field cannot drift.
const (
	OperationArchive   = "archive"
	OperationDelete    = "delete"
	OperationUnarchive = "unarchive"
)

// EntityTask / EntityComment are the entity classes ResolveBucketPermissions
// arbitrates over. Keep these in lockstep with domain.BucketPermissions sub
// fields.
const (
	EntityTask    = "task"
	EntityComment = "comment"
)

// PermissionEdit / PermissionDelete identifies the CRUD action being checked.
const (
	PermissionEdit   = "edit"
	PermissionDelete = "delete"
)

// ResolveBucketPermissions tells the caller whether (entity, operation) is
// allowed in the bucket the task currently sits in. Returns a descriptive
// hint when the answer is "no", listing buckets where the operation IS
// permitted so the agent can suggest a remediation. Defaults: task.edit is
// true only in the first bucket; task.delete is false everywhere; comment.*
// inherits from task when not declared.
func (s *WorkflowService) ResolveBucketPermissions(ctx context.Context, project domain.ProjectContext, taskID int64, entity, operation string) (bool, string, error) {
	currentBucketID, currentBucketKey, err := s.repo.CurrentTaskBucket(ctx, project.ID, taskID)
	if err != nil {
		return false, "", err
	}
	workflow, err := s.config.ActiveWorkflow(ctx)
	if err != nil {
		return false, "", err
	}

	allowed, allowedBuckets := evaluatePermission(workflow, currentBucketID, entity, operation)
	if allowed {
		return true, "", nil
	}

	hint := buildPermissionHint(entity, operation, currentBucketKey, allowedBuckets)
	return false, hint, nil
}

// EvaluateOperationGuards runs every guard declared on
// `workflows[].operations.<operation>.guards` against the named task. Reuses
// the same comments_tagged / comments_min / blockers_in evaluator as
// transition guards so adding a guard type benefits both code paths at once.
// Returns nil when no guards are violated; otherwise a domain.ErrGuardViolation
// with the first failing guard's hint and counts.
func (s *WorkflowService) EvaluateOperationGuards(ctx context.Context, projectID, taskID int64, operation string) error {
	workflow, err := s.config.ActiveWorkflow(ctx)
	if err != nil {
		return err
	}
	guards := operationGuards(workflow, operation)
	if len(guards) == 0 {
		return nil
	}
	return s.runGuards(ctx, projectID, taskID, guards)
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

	state, err := s.repo.TaskState(ctx, project.ID, taskID)
	if err != nil {
		return
	}
	if state == domain.TaskStateArchived {
		err = domain.NewError(domain.ErrValidation, "task is archived; unarchive before moving", map[string]any{"task_id": taskID, "hint": "call tasks.unarchive(task_id) first"})
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
	return s.runGuards(ctx, projectID, taskID, guards)
}

// runGuards is the shared evaluator. Both transition guards and operation
// guards (archive/delete/unarchive) feed through here so a new guard type
// only needs one switch arm.
func (s *WorkflowService) runGuards(ctx context.Context, projectID, taskID int64, guards []domain.TransitionGuard) error {
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

// evaluatePermission resolves bucket policy without hitting the database.
// Returns (allowed, otherBucketKeysWhereAllowed) so the caller can compose a
// hint that tells the user where the action IS permitted.
func evaluatePermission(workflow domain.Workflow, currentBucketID int64, entity, operation string) (bool, []string) {
	if len(workflow.Buckets) == 0 {
		return false, nil
	}
	firstBucketID := workflow.Buckets[0].ID

	resolved := func(b domain.Bucket) (bool, bool) {
		isFirst := b.ID == firstBucketID
		var edit, del bool
		switch entity {
		case EntityComment:
			edit, del = b.ResolveCommentPermission(isFirst)
		default:
			edit, del = b.ResolveTaskPermission(isFirst)
		}
		return edit, del
	}

	var current domain.Bucket
	for _, b := range workflow.Buckets {
		if b.ID == currentBucketID {
			current = b
			break
		}
	}

	allowed := false
	if current.ID != 0 {
		edit, del := resolved(current)
		switch operation {
		case PermissionEdit:
			allowed = edit
		case PermissionDelete:
			allowed = del
		}
	}

	if allowed {
		return true, nil
	}

	var others []string
	for _, b := range workflow.Buckets {
		if b.ID == currentBucketID {
			continue
		}
		edit, del := resolved(b)
		permitted := false
		switch operation {
		case PermissionEdit:
			permitted = edit
		case PermissionDelete:
			permitted = del
		}
		if permitted {
			others = append(others, b.Key)
		}
	}
	return false, others
}

func buildPermissionHint(entity, operation, currentBucketKey string, allowedBuckets []string) string {
	if len(allowedBuckets) == 0 {
		return fmt.Sprintf("policy: %s.%s is not permitted in bucket %q (no bucket allows it; declare workflows[].buckets[].permissions.%s.%s)", entity, operation, currentBucketKey, entity, operation)
	}
	return fmt.Sprintf("policy: %s.%s is not permitted in bucket %q. Move the task to one of: %s — then retry.", entity, operation, currentBucketKey, strings.Join(allowedBuckets, ", "))
}

func operationGuards(workflow domain.Workflow, operation string) []domain.TransitionGuard {
	switch operation {
	case OperationArchive:
		return workflow.Operations.Archive.Guards
	case OperationDelete:
		return workflow.Operations.Delete.Guards
	case OperationUnarchive:
		return workflow.Operations.Unarchive.Guards
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
