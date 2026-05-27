package app

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/app/guards"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// WorkflowService owns workflow policy: which bucket a freshly-created task
// lands in, whether a move is allowed by the configured transitions, whether
// the move's guards are satisfied, and whether a move into the final bucket
// should additionally emit task.completed. The adapter layer (sqlite) only
// supplies the underlying state primitives (CurrentTaskBucket, TaskState);
// every config read goes through the immutable per-project Snapshot held
// here. Guard evaluation is delegated to the guards.Evaluator captured at
// construction.
type WorkflowService struct {
	snap          *config.Snapshot
	repo          WorkflowRepository
	guards        *guards.Evaluator
	tasks         TaskRepository
	events        EventRepository
	registry      *domain.EnumRegistry
	planFinalizer PlanFinalizer
}

// WithPlanFinalizer attaches the optional PlanFinalizer hook called after
// a task moves into the workflow's final bucket. Production wiring goes
// through NewWorkflowServiceFromStore (type-asserts the composite store
// against PlanFinalizer); tests can stub or skip it without touching the
// existing fakes. Returns the receiver so callers can chain.
func (s *WorkflowService) WithPlanFinalizer(pf PlanFinalizer) *WorkflowService {
	s.planFinalizer = pf
	return s
}

// NewWorkflowService wires the workflow service against an immutable
// per-project Snapshot and a guard evaluator. The snap pointer is captured
// here and used for every workflow / bucket / transition lookup; guard
// evaluation flows through the supplied evaluator (whose own snap pointer
// must be the same instance). The SQL repo is only consulted for state
// (CurrentTaskBucket, TaskState).
func NewWorkflowService(snap *config.Snapshot, workflow WorkflowRepository, evaluator *guards.Evaluator, tasks TaskRepository, events EventRepository, registry *domain.EnumRegistry) *WorkflowService {
	return &WorkflowService{snap: snap, repo: workflow, guards: evaluator, tasks: tasks, events: events, registry: registry}
}

// NewWorkflowServiceFromStore is the production-path sugar for callers that
// hold a single composite store implementing every workflow port (in
// production: *sqlite.Store). snap is the per-project Snapshot captured
// at construction; the registry is required and is threaded into the
// service so priority lookups use the bundle-scoped tables. The guard
// evaluator is built from the same composite so every component shares
// one snap pointer.
func NewWorkflowServiceFromStore(store CompositeWorkflowStore, registry *domain.EnumRegistry, snap *config.Snapshot) *WorkflowService {
	evaluator := guards.NewGuardEvaluator(snap, store, store)
	svc := NewWorkflowService(snap, store, evaluator, store, store, registry)
	if pf, ok := store.(PlanFinalizer); ok {
		svc.WithPlanFinalizer(pf)
	}
	return svc
}

// ResolveDefaultBucket returns the key of the first bucket in the active
// workflow — the bucket new tasks land in when callers do not specify one.
// Errors with ErrConfigInvalid if the active workflow has no buckets, so
// upstream callers can default to "" without baking "backlog" into prod.
func (s *WorkflowService) ResolveDefaultBucket(_ context.Context) (string, error) {
	return defaultBucketKey(s.snap)
}

// FinalBucketKey returns the key of the highest-position bucket in the active
// workflow — the destination for archive operations. Errors with
// ErrConfigInvalid if the active workflow has no buckets.
func (s *WorkflowService) FinalBucketKey(_ context.Context) (string, error) {
	return finalBucketKey(s.snap)
}

// workflowBuckets returns the resolved workflow's bucket slice plus the
// nil-snap / empty-buckets guard the kit-key helpers share. Extracted
// per #297 review-opportunity §D.14 — `defaultBucketKey` + `finalBucketKey`
// duplicated the guard byte-for-byte and now route through one source of
// truth.
func workflowBuckets(snap *config.Snapshot) ([]domain.Bucket, error) {
	if snap == nil {
		return nil, domain.NewError(domain.ErrConfigInvalid, "active workflow snapshot is nil", nil)
	}
	workflow := snap.Workflow()
	if len(workflow.Buckets) == 0 {
		return nil, domain.NewError(domain.ErrConfigInvalid, "active workflow has no buckets", nil)
	}
	return workflow.Buckets, nil
}

func defaultBucketKey(snap *config.Snapshot) (string, error) {
	buckets, err := workflowBuckets(snap)
	if err != nil {
		return "", err
	}
	return buckets[0].Key, nil
}

func finalBucketKey(snap *config.Snapshot) (string, error) {
	buckets, err := workflowBuckets(snap)
	if err != nil {
		return "", err
	}
	final := buckets[0]
	for _, b := range buckets {
		if b.Position > final.Position {
			final = b
		}
	}
	return final.Key, nil
}

// CreateTask is the policy-bearing wrapper around TaskRepository.CreateTask:
// it resolves the default bucket when bucketKey is empty, then delegates to
// the persister. The store still emits task.created in the same transaction.
// parentID is forwarded straight to the storage layer so sub-task creation
// stays atomic — when non-nil the INSERT carries the FK, when nil the row
// lands as a root.
func (s *WorkflowService) CreateTask(ctx context.Context, projectID int64, title, description string, priority domain.Priority, bucketKey string, parentID *int64) (domain.Task, error) {
	taskSnap := s.snap.For(domain.Task{ParentID: parentID})
	bucketKey = strings.TrimSpace(bucketKey)
	if bucketKey == "" {
		key, err := defaultBucketKey(taskSnap)
		if err != nil {
			return domain.Task{}, err
		}
		bucketKey = key
	}
	if priority == domain.PriorityZero {
		priority = s.defaultPriority()
	}
	return s.tasks.CreateTask(ctx, projectID, title, description, priority, bucketKey, parentID, taskSnap)
}

// OperationArchive / OperationDelete / OperationUnarchive label the
// non-transition operations whose guards are evaluated by the workflow
// service. Symbolic constants instead of bare strings so callers and the
// workflow Operations field cannot drift.
const (
	OperationArchive   = guards.OperationArchive
	OperationDelete    = guards.OperationDelete
	OperationUnarchive = guards.OperationUnarchive
)

// GuardOperation* are the canonical free-form `operation` payload values
// emitted with guard.violated events. Consumers filter on these strings;
// the catalog stays fixed so log grouping/aggregation is stable.
const (
	GuardOperationTaskTransition = guards.OperationTaskTransition
	GuardOperationTaskArchive    = guards.OperationTaskArchive
	GuardOperationTaskUnarchive  = guards.OperationTaskUnarchive
	GuardOperationTaskEdit       = guards.OperationTaskEdit
	GuardOperationTaskDelete     = guards.OperationTaskDelete
	GuardOperationCommentEdit    = guards.OperationCommentEdit
	GuardOperationCommentDelete  = guards.OperationCommentDelete
)

// GuardRule* are the canonical free-form `rule` payload values emitted
// alongside the operation. Transition guards reuse the guard.Type strings
// so consumers can join with workflow YAML; permission denials use
// "permissions" so they are easy to bucket.
const (
	GuardRuleTransition  = guards.RuleTransition
	GuardRulePermissions = guards.RulePermissions
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
// permitted so the agent can suggest a remediation.
func (s *WorkflowService) ResolveBucketPermissions(ctx context.Context, project domain.ProjectContext, taskID int64, entity, operation string) (bool, string, error) {
	task, err := s.tasks.GetTaskByID(ctx, project.ID, taskID, s.snap)
	if err != nil {
		return false, "", err
	}
	taskSnap := s.snap.For(task)
	currentBucketID, currentBucketKey, err := s.repo.CurrentTaskBucket(ctx, project.ID, taskID, taskSnap)
	if err != nil {
		return false, "", err
	}
	workflow := taskSnap.Workflow()

	allowed, allowedBuckets := evaluatePermission(workflow, currentBucketID, entity, operation)
	if allowed {
		return true, "", nil
	}

	hint := buildPermissionHint(entity, operation, currentBucketKey, allowedBuckets)
	return false, hint, nil
}

// Evaluator returns the per-project guard evaluator the service composes.
// Callers (TaskService, CommentService, TUI render paths) that need to emit
// a violation event or evaluate non-transition operation guards go through
// the evaluator directly instead of widening the WorkflowService surface
// with forwarder methods that mirror the evaluator API.
func (s *WorkflowService) Evaluator() *guards.Evaluator {
	return s.guards
}

// TaskByID returns the live task row resolved through the per-project
// Snapshot. Exposed so CommentService can hand the task into
// `Evaluator.EmitViolatedForTask` when surfacing a comment-level guard
// violation — the entity is `task`, so the payload must carry the
// task's depth/parent metadata (#301 review §11557 finding A4).
func (s *WorkflowService) TaskByID(ctx context.Context, project domain.ProjectContext, taskID int64) (domain.Task, error) {
	return s.tasks.GetTaskByID(ctx, project.ID, taskID, s.snap)
}

// ResolveTaskSnap returns the task row plus the snapshot resolved for
// its depth. Used by CommentService when threading the task through
// `Evaluator.EmitViolatedForTask` so the payload's `resolved_kit` is
// derived from `snap.For(task)` instead of falling back to root.
func (s *WorkflowService) ResolveTaskSnap(ctx context.Context, project domain.ProjectContext, taskID int64) (domain.Task, *config.Snapshot, error) {
	task, err := s.TaskByID(ctx, project, taskID)
	if err != nil {
		return domain.Task{}, nil, err
	}
	return task, s.snap.For(task), nil
}

// MoveTask runs the full move policy: resolve current/target buckets, enforce
// the configured transition + guards, persist the move via TaskRepository
// (which records task.moved), then conditionally emit task.completed when the
// destination is the workflow's final bucket. Returns the persisted task.
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

	taskForResolution, err := s.tasks.GetTaskByID(ctx, project.ID, taskID, s.snap)
	if err != nil {
		return
	}
	taskSnap := s.snap.For(taskForResolution)

	state, err := s.repo.TaskState(ctx, project.ID, taskID)
	if err != nil {
		return
	}
	if state == domain.TaskStateArchived {
		err = domain.NewError(domain.ErrValidation, "task is archived; unarchive before moving", map[string]any{"task_id": taskID, "hint": "call tasks.unarchive(task_id) first"})
		return
	}

	currentBucketID, _, err := s.repo.CurrentTaskBucket(ctx, project.ID, taskID, taskSnap)
	if err != nil {
		return
	}

	target, ok := taskSnap.BucketByKey(targetBucketKey)
	if !ok {
		err = domain.NewError(domain.ErrBucketNotFound, "bucket not found", map[string]any{"bucket": targetBucketKey})
		return
	}

	if currentBucketID != target.ID {
		allowed := taskSnap.TransitionAllowed(currentBucketID, target.ID)
		if !allowed {
			s.guards.EmitViolatedForTask(ctx, project.ID, taskForResolution, taskSnap,
				GuardOperationTaskTransition, GuardRuleTransition,
				"transition not allowed",
				map[string]any{"task_id": taskID, "from_bucket_id": currentBucketID, "to_bucket_id": target.ID, "to_bucket": targetBucketKey})
			err = domain.NewError(domain.ErrWorkflowInvalidTransition, "transition not allowed", map[string]any{"task_id": taskID, "from": currentBucketID, "to": target.ID})
			return
		}
		if err = s.guards.EvaluateTransitionForTask(ctx, project.ID, taskForResolution, currentBucketID, target.ID, targetBucketKey, taskSnap); err != nil {
			return
		}
	}

	task, err = s.tasks.MoveTask(ctx, project.ID, taskID, targetBucketKey, taskSnap)
	if err != nil {
		return
	}

	if currentBucketID != target.ID {
		if taskSnap.IsFinalBucket(target.ID) {
			payload, payloadErr := domain.NewTaskSubjectPayload(task, taskSnap.Kit().Key, map[string]any{"bucket": targetBucketKey})
			if payloadErr != nil {
				err = payloadErr
				return
			}
			if _, err = s.events.RecordTaskEvent(ctx, project.ID, taskID, domain.EventTypeTaskCompleted, "", payload); err != nil {
				return
			}
			// Plan auto-done: when the task that just landed in the
			// terminal bucket was the last pending one in its plan,
			// transition the plan to status='done' and emit
			// plan.done. Non-plan tasks are a no-op; finaliser
			// failures are swallowed because plan finalisation is
			// recomputable on the next terminal move — losing the
			// audit signal beats blocking a legitimate move.
			if s.planFinalizer != nil {
				_, _ = s.planFinalizer.MaybeFinalizePlanForTask(ctx, project.ID, taskID, taskSnap)
			}
		}
	}
	return
}

// evaluatePermission resolves bucket policy without hitting the database.
// Returns (allowed, otherBucketKeysWhereAllowed) so the caller can compose a
// hint that tells the user where the action IS permitted. Resolution is
// fully data-driven: bucket overrides → workflow.defaults → implicit
// `true` (no rule = allow).
func evaluatePermission(workflow domain.Workflow, currentBucketID int64, entity, operation string) (bool, []string) {
	if len(workflow.Buckets) == 0 {
		return false, nil
	}

	resolved := func(b domain.Bucket) (bool, bool) {
		var edit, del bool
		switch entity {
		case EntityComment:
			edit, del = b.ResolveCommentPermission(workflow.Defaults)
		default:
			edit, del = b.ResolveTaskPermission(workflow.Defaults)
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

// defaultPriority returns the configured default priority id via the
// bundle-scoped registry. Returns PriorityZero when the registry has no
// entries (uninitialised tests) — callers treat that as "let the storage
// layer pick" so partially-bootstrapped tests still write rows.
func (s *WorkflowService) defaultPriority() domain.Priority {
	return s.registry.DefaultPriority()
}
