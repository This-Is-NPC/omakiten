package app

import (
	"context"
	"encoding/json"
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
func (s *WorkflowService) CreateTask(ctx context.Context, projectID int64, title, description string, priority domain.Priority, bucketKey string) (domain.Task, error) {
	bucketKey = strings.TrimSpace(bucketKey)
	if bucketKey == "" {
		key, err := s.ResolveDefaultBucket(ctx)
		if err != nil {
			return domain.Task{}, err
		}
		bucketKey = key
	}
	// Resolve "no priority specified" to the configured `default: true`
	// id BEFORE reaching the store. Without this, PriorityZero falls
	// through to the SQL column DEFAULT (the canonical kit's id 2 =
	// "normal") and ignores user customisations to config.priorities.
	// domain.DefaultPriority returns PriorityZero when the registry has
	// not been wired (test contexts), in which case the store's SQL
	// DEFAULT keeps acting as the safety net.
	if priority == domain.PriorityZero {
		priority = domain.DefaultPriority()
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

// GuardOperation* are the canonical free-form `operation` payload values
// emitted with guard.violated events. Consumers filter on these strings;
// the catalog stays fixed so log grouping/aggregation is stable.
const (
	GuardOperationTaskTransition = "task.transition"
	GuardOperationTaskArchive    = "task.archive"
	GuardOperationTaskUnarchive  = "task.unarchive"
	GuardOperationTaskEdit       = "task.edit"
	GuardOperationTaskDelete     = "task.delete"
	GuardOperationCommentEdit    = "comment.edit"
	GuardOperationCommentDelete  = "comment.delete"
)

// GuardRule* are the canonical free-form `rule` payload values emitted
// alongside the operation. Transition guards reuse the guard.Type strings
// so consumers can join with workflow YAML; permission denials use
// "permissions" so they are easy to bucket.
const (
	GuardRuleTransition  = "transition_not_allowed"
	GuardRulePermissions = "permissions"
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
// permitted so the agent can suggest a remediation. Resolution chain is
// fully data-driven: bucket.permissions → workflow.defaults → implicit
// `true` (no rule = allow). Comment fields inherit from task field-by-field
// at every layer when unset. There is no hardcoded "first bucket is
// special" rule — every constraint lives in the YAML.
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
// with the first failing guard's hint and counts. Emits guard.violated when
// a guard fails, tagged with the supplied opPayload (e.g. task.archive).
func (s *WorkflowService) EvaluateOperationGuards(ctx context.Context, projectID, taskID int64, operation string) error {
	workflow, err := s.config.ActiveWorkflow(ctx)
	if err != nil {
		return err
	}
	guards := operationGuards(workflow, operation)
	if len(guards) == 0 {
		return nil
	}
	opPayload := operationPayloadName(operation)
	return s.runGuards(ctx, projectID, taskID, guards, opPayload)
}

// EmitGuardViolated records a guard.violated domain event. operation and
// rule are free-form strings — call sites pick the values that name the
// operation precisely. target carries identifiers (task_id, comment_id,
// from_bucket, to_bucket). attempted_by is derived from the request
// source: mcp -> "agent", anything else -> "user". Telemetry must not
// break business logic; emission errors are swallowed.
func (s *WorkflowService) EmitGuardViolated(ctx context.Context, projectID int64, entityType string, entityID int64, operation, rule, hint string, target map[string]any) {
	if s.events == nil {
		return
	}
	if target == nil {
		target = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"operation":    operation,
		"rule":         rule,
		"hint":         hint,
		"target":       target,
		"attempted_by": guardAttemptedBy(ctx),
	})
	if err != nil {
		return
	}
	_ = s.events.RecordEntityEvent(ctx, entityType, entityID, projectID, domain.EventTypeGuardViolated, string(body))
}

// guardAttemptedBy derives the attempted_by tag from the request source.
// MCP traffic is agent-driven; CLI/TUI are treated as user-driven.
func guardAttemptedBy(ctx context.Context) string {
	source, _, _, _, _ := activity.FromContext(ctx)
	if source == "mcp" {
		return "agent"
	}
	return "user"
}

// operationPayloadName maps the internal Operation* constant to the
// canonical free-form `operation` payload value used in guard.violated.
func operationPayloadName(operation string) string {
	switch operation {
	case OperationArchive:
		return GuardOperationTaskArchive
	case OperationDelete:
		return GuardOperationTaskDelete
	case OperationUnarchive:
		return GuardOperationTaskUnarchive
	}
	return operation
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
			s.EmitGuardViolated(ctx, project.ID, domain.EventEntityTask, taskID,
				GuardOperationTaskTransition, GuardRuleTransition,
				"transition not allowed",
				map[string]any{"task_id": taskID, "from_bucket_id": currentBucketID, "to_bucket_id": target.ID, "to_bucket": targetBucketKey})
			err = domain.NewError(domain.ErrWorkflowInvalidTransition, "transition not allowed", map[string]any{"task_id": taskID, "from": currentBucketID, "to": target.ID})
			return
		}
		if err = s.evaluateGuards(ctx, project.ID, taskID, currentBucketID, target.ID, targetBucketKey); err != nil {
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
// targetBucketKey carries the user-visible bucket name used in the
// guard.violated payload's target.to_bucket.
func (s *WorkflowService) evaluateGuards(ctx context.Context, projectID, taskID, fromBucketID, toBucketID int64, targetBucketKey string) error {
	guards, err := s.repo.LoadTransitionGuards(ctx, fromBucketID, toBucketID)
	if err != nil {
		return err
	}
	target := map[string]any{"task_id": taskID, "from_bucket_id": fromBucketID, "to_bucket_id": toBucketID, "to_bucket": targetBucketKey}
	return s.runGuards(ctx, projectID, taskID, guards, GuardOperationTaskTransition, target)
}

// runGuards is the shared evaluator. Both transition guards and operation
// guards (archive/delete/unarchive) feed through here so a new guard type
// only needs one switch arm. operation labels the call site for the
// guard.violated payload; defaultTarget carries call-site-specific
// identifiers and is overridden per check when needed.
func (s *WorkflowService) runGuards(ctx context.Context, projectID, taskID int64, guards []domain.TransitionGuard, operation string, defaultTarget ...map[string]any) error {
	target := map[string]any{"task_id": taskID}
	if len(defaultTarget) > 0 && defaultTarget[0] != nil {
		target = defaultTarget[0]
	}
	for _, guard := range guards {
		switch guard.Type {
		case "blockers_in":
			if err := s.checkBlockersIn(ctx, projectID, taskID, guard.Buckets, guard.Hint, operation, target); err != nil {
				return err
			}
		case "comments_min":
			if err := s.checkCommentsMin(ctx, projectID, taskID, guard.Count, guard.Hint, operation, target); err != nil {
				return err
			}
		case "comments_tagged":
			if err := s.checkCommentsTagged(ctx, projectID, taskID, guard.Tag, guard.Count, guard.Hint, operation, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// evaluatePermission resolves bucket policy without hitting the database.
// Returns (allowed, otherBucketKeysWhereAllowed) so the caller can compose a
// hint that tells the user where the action IS permitted. Resolution is
// fully data-driven: bucket overrides → workflow.defaults → implicit
// `true` (no rule = allow). There is no hardcoded "first bucket is
// special" rule — that lives in the YAML now.
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

func (s *WorkflowService) checkBlockersIn(ctx context.Context, projectID, taskID int64, allowedKeys []string, hint, operation string, target map[string]any) error {
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
	s.EmitGuardViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "blockers_in", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (s *WorkflowService) checkCommentsMin(ctx context.Context, projectID, taskID int64, minCount int, hint, operation string, target map[string]any) error {
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
	s.EmitGuardViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "comments_min", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (s *WorkflowService) checkCommentsTagged(ctx context.Context, projectID, taskID int64, tag string, minCount int, hint, operation string, target map[string]any) error {
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
	s.EmitGuardViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "comments_tagged", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}
