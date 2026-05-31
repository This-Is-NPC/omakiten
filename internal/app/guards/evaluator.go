// Package guards owns workflow guard evaluation: transition guards declared
// on (from,to) edges and operation guards declared on archive/delete/unarchive.
// Extracting the evaluator out of WorkflowService keeps each guard type's
// implementation in a single place and lets app services compose it without
// pulling in workflow's bucket-policy concerns.
package guards

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// OperationArchive / OperationDelete / OperationUnarchive label the
// non-transition operations whose guards are evaluated by the evaluator.
const (
	OperationArchive   = "archive"
	OperationDelete    = "delete"
	OperationUnarchive = "unarchive"
)

// OperationTaskTransition / OperationTaskArchive / ... are the canonical
// free-form `operation` payload values emitted with guard.violated events.
// Stable strings so log grouping/aggregation across consumers does not drift.
const (
	OperationTaskTransition = "task.transition"
	OperationTaskArchive    = "task.archive"
	OperationTaskUnarchive  = "task.unarchive"
	OperationTaskEdit       = "task.edit"
	OperationTaskDelete     = "task.delete"
	OperationCommentCreate  = "comment.create"
	OperationCommentEdit    = "comment.edit"
	OperationCommentDelete  = "comment.delete"
)

// RuleTransition / RulePermissions are the canonical free-form `rule` payload
// values. Transition guards reuse the guard.Type strings so consumers can
// join with workflow YAML; permission denials use "permissions".
const (
	RuleTransition  = "transition_not_allowed"
	RulePermissions = "permissions"
)

// Repository exposes the read-only counts the evaluator needs. Defined here
// (rather than imported from app) to keep the dependency direction inward
// and let guards be composed without an app↔guards import cycle.
type Repository interface {
	ListTaskBlockerBuckets(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) ([]domain.TaskBlocker, error)
	CountTaskComments(ctx context.Context, projectID, taskID int64) (int, error)
	CountTaskCommentsTagged(ctx context.Context, projectID, taskID int64, tagName string) (int, error)
	// CountPriorWavesPending counts tasks in earlier waves of the same
	// plan that are not yet in the workflow's final bucket. Returns 0
	// when the task has no wave attachment so the wave_gate guard is a
	// silent no-op for non-plan tasks.
	CountPriorWavesPending(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (int, error)
	// FirstChildNotInBucket gates the subtasks_complete guard. Returns
	// the first direct child whose bucket_id != finalBucketID, ordered
	// by id; the boolean reports whether a child still blocks. Walking
	// only direct children is intentional — deeper levels gate their
	// own promotions, so the parent's guard fires exactly when the
	// immediate sub-tasks are still open.
	FirstChildNotInBucket(ctx context.Context, projectID, parentID, finalBucketID int64, buckets domain.BucketResolver) (domain.Task, bool, error)
}

// EventSink records guard.violated domain events. Telemetry only — emission
// errors are swallowed so guard failures never become event-record failures.
type EventSink interface {
	RecordEntityEvent(ctx context.Context, entityType string, entityID int64, projectID int64, eventType string, payload string) error
}

// Evaluator evaluates transition and operation guards against the immutable
// per-project Snapshot. Construct one per ProjectRuntime; the snap pointer is
// captured at construction and used for every guard read.
type Evaluator struct {
	snap   *config.Snapshot
	repo   Repository
	events EventSink
}

// NewGuardEvaluator wires the evaluator with the per-project Snapshot, the
// counts repository, and the event sink. snap is required; events may be nil
// for read-only paths but production composition always supplies one.
func NewGuardEvaluator(snap *config.Snapshot, repo Repository, events EventSink) *Evaluator {
	return &Evaluator{snap: snap, repo: repo, events: events}
}

// EvaluateTransition runs every guard attached to the (from, to) transition.
// Order follows declaration order; the first violation short-circuits.
// targetBucketKey carries the user-visible bucket name used in the
// guard.violated payload's target.to_bucket.
//
// Compat shim: legacy callers pass a bare taskID. The synthesized
// `domain.Task{ID: taskID}` carries no depth/parent metadata, so the
// guard.violated payload's subject_depth lands as 0 (root). Production
// callsites should use EvaluateTransitionForTask so the payload
// carries the task's true depth + resolved-kit identity (#301 review
// §11557 finding A4).
func (e *Evaluator) EvaluateTransition(ctx context.Context, projectID, taskID, fromBucketID, toBucketID int64, targetBucketKey string) error {
	return e.EvaluateTransitionForTask(ctx, projectID, domain.Task{ID: taskID}, fromBucketID, toBucketID, targetBucketKey, nil)
}

// EvaluateTransitionFor is the depth-aware variant used by services that have
// already resolved the task's kit. A nil snap falls back to the evaluator's root
// snapshot to preserve the older call surface.
//
// Compat shim: legacy callers pass taskID; the synthesized task lacks
// depth/parent metadata. Use EvaluateTransitionForTask in new code.
func (e *Evaluator) EvaluateTransitionFor(ctx context.Context, projectID, taskID, fromBucketID, toBucketID int64, targetBucketKey string, snap *config.Snapshot) error {
	return e.EvaluateTransitionForTask(ctx, projectID, domain.Task{ID: taskID}, fromBucketID, toBucketID, targetBucketKey, snap)
}

// EvaluateTransitionForTask is the depth-aware production entry point.
// The task's Depth + ParentID feed the guard.violated payload's
// subject metadata so depth-aware hooks (hook.SubjectDepth) and
// per-kit notification routing dispatch correctly for sub-task
// transitions. A nil snap falls back to the evaluator's root snapshot.
func (e *Evaluator) EvaluateTransitionForTask(ctx context.Context, projectID int64, task domain.Task, fromBucketID, toBucketID int64, targetBucketKey string, snap *config.Snapshot) error {
	snap = e.resolvedSnapshot(snap)
	if snap == nil {
		return nil
	}
	specs := snap.Guards(fromBucketID, toBucketID)
	target := map[string]any{"task_id": task.ID, "from_bucket_id": fromBucketID, "to_bucket_id": toBucketID, "to_bucket": targetBucketKey}
	return e.runGuardsWithSnapshot(ctx, projectID, task, specs, OperationTaskTransition, target, snap)
}

// EvaluateOperation runs every guard declared on the named operation against
// the named task. operation is one of the Operation* constants.
//
// Compat shim: production callsites should use EvaluateOperationForTask
// so the guard.violated payload carries the task's true depth (#301
// review §11557 finding A4).
func (e *Evaluator) EvaluateOperation(ctx context.Context, projectID, taskID int64, operation string) error {
	return e.EvaluateOperationForTask(ctx, projectID, domain.Task{ID: taskID}, operation, nil)
}

// EvaluateOperationFor evaluates archive/delete/unarchive guards against the
// task's resolved kit. A nil snap falls back to the evaluator's root snapshot.
//
// Compat shim: use EvaluateOperationForTask in production code.
func (e *Evaluator) EvaluateOperationFor(ctx context.Context, projectID, taskID int64, operation string, snap *config.Snapshot) error {
	return e.EvaluateOperationForTask(ctx, projectID, domain.Task{ID: taskID}, operation, snap)
}

// EvaluateOperationForTask is the depth-aware production entry point
// for non-transition guards (archive / delete / unarchive). The task's
// depth/parent metadata feeds the guard.violated payload subject
// fields so sub-task violations route to sub-kit hooks (#301 review
// §11557 finding A4).
func (e *Evaluator) EvaluateOperationForTask(ctx context.Context, projectID int64, task domain.Task, operation string, snap *config.Snapshot) error {
	snap = e.resolvedSnapshot(snap)
	if snap == nil {
		return nil
	}
	workflow := snap.Workflow()
	specs := operationGuards(workflow, operation)
	if len(specs) == 0 {
		return nil
	}
	return e.runGuardsWithSnapshot(ctx, projectID, task, specs, OperationPayloadName(operation), nil, snap)
}

func (e *Evaluator) resolvedSnapshot(snap *config.Snapshot) *config.Snapshot {
	if snap != nil {
		return snap
	}
	return e.snap
}

// EmitViolated records a guard.violated domain event. operation and rule are
// free-form strings — call sites pick the values that name the operation
// precisely. target carries identifiers (task_id, comment_id, from_bucket,
// to_bucket). attempted_by is derived from the request source: mcp -> "agent",
// anything else -> "user". Telemetry must not break business logic; emission
// errors are swallowed.
//
// Task-scoped callsites should prefer EmitViolatedForTask so the
// payload carries `subject_task_id` / `subject_parent_id` /
// `subject_depth` / `resolved_kit` — the same depth-routing metadata
// every other task event already emits. Without those fields the
// hooks engine cannot pick the right depth filter for sub-tasks (#301
// review §11557 finding A4).
func (e *Evaluator) EmitViolated(ctx context.Context, projectID int64, entityType string, entityID int64, operation, rule, hint string, target map[string]any) {
	if e.events == nil {
		return
	}
	body := buildGuardViolatedPayload(ctx, operation, rule, hint, target, nil)
	if body == "" {
		return
	}
	_ = e.events.RecordEntityEvent(ctx, entityType, entityID, projectID, domain.EventTypeGuardViolated, body)
}

// EmitViolatedForTask is the task-scoped variant: it stamps the
// payload with `subject_task_id`, `subject_parent_id`,
// `subject_depth`, and `resolved_kit` (derived from task + snap.For).
// Sub-task guard violations carry depth=1 + the resolved sub-kit's
// key, so depth-aware hooks (`hook.SubjectDepth`) and per-kit
// notification routing pick the right side of the cascade — locked
// decision on task #301 (review §11557 finding A4).
//
// Falls back to the bare EmitViolated payload when snap is nil
// (uninitialised tests) so the legacy invariant still holds.
func (e *Evaluator) EmitViolatedForTask(ctx context.Context, projectID int64, task domain.Task, snap *config.Snapshot, operation, rule, hint string, target map[string]any) {
	if e.events == nil {
		return
	}
	var subject map[string]any
	if snap != nil {
		taskSnap := snap.For(task)
		kitKey := ""
		if taskSnap != nil {
			kitKey = taskSnap.Kit().Key
		}
		subject = taskSubjectFields(task, kitKey)
	}
	body := buildGuardViolatedPayload(ctx, operation, rule, hint, target, subject)
	if body == "" {
		return
	}
	_ = e.events.RecordEntityEvent(ctx, domain.EventEntityTask, task.ID, projectID, domain.EventTypeGuardViolated, body)
}

// EmitViolatedForProject is the project-scoped variant for guard
// violations whose subject is a project rather than a task (project-scoped
// comment edit/delete denials). The event is recorded under
// EventEntityProject with entity_id = projectID and no task subject fields —
// projects have no depth/parent metadata, so the depth-routing block other
// task events carry is intentionally absent. Telemetry only; emission errors
// are swallowed.
func (e *Evaluator) EmitViolatedForProject(ctx context.Context, projectID int64, operation, rule, hint string, target map[string]any) {
	if e.events == nil {
		return
	}
	body := buildGuardViolatedPayload(ctx, operation, rule, hint, target, nil)
	if body == "" {
		return
	}
	_ = e.events.RecordEntityEvent(ctx, domain.EventEntityProject, projectID, projectID, domain.EventTypeGuardViolated, body)
}

// buildGuardViolatedPayload assembles the JSON body shared by both
// emission paths. `subject` is folded in at the top level (alongside
// `operation` / `rule` / `hint` / `target` / `attempted_by`) so the
// hooks engine's `subject_depth` extractor sees the same shape every
// other task event uses.
func buildGuardViolatedPayload(ctx context.Context, operation, rule, hint string, target, subject map[string]any) string {
	if target == nil {
		target = map[string]any{}
	}
	payload := map[string]any{
		"operation":    operation,
		"rule":         rule,
		"hint":         hint,
		"target":       target,
		"attempted_by": attemptedBy(ctx),
	}
	for k, v := range subject {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(body)
}

// taskSubjectFields mirrors domain.NewTaskSubjectPayload's subject
// metadata block so guard.violated payloads carry the exact keys the
// hooks engine + audit consumers already read against. Returns nil
// for a zero-value task so the EmitViolated path stays clean.
func taskSubjectFields(task domain.Task, kitKey string) map[string]any {
	if task.ID == 0 {
		return nil
	}
	out := map[string]any{
		"subject_task_id": task.ID,
		"subject_depth":   task.Depth,
		"resolved_kit":    kitKey,
	}
	if task.ParentID != nil {
		out["subject_parent_id"] = *task.ParentID
	} else {
		out["subject_parent_id"] = nil
	}
	return out
}

// OperationPayloadName maps the internal Operation* constant to the canonical
// free-form `operation` payload value used in guard.violated events.
func OperationPayloadName(operation string) string {
	switch operation {
	case OperationArchive:
		return OperationTaskArchive
	case OperationDelete:
		return OperationTaskDelete
	case OperationUnarchive:
		return OperationTaskUnarchive
	}
	return operation
}

// attemptedBy derives the attempted_by tag from the request source. MCP
// traffic is agent-driven; CLI/TUI are treated as user-driven.
func attemptedBy(ctx context.Context) string {
	source, _, _, _, _ := activity.FromContext(ctx)
	if source == "mcp" {
		return "agent"
	}
	return "user"
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

// runGuardsWithSnapshot is the shared dispatch. Both transition guards and
// operation guards (archive/delete/unarchive) feed through here so a new guard
// type only needs one switch arm. operation labels the call site for the
// guard.violated payload; defaultTarget carries call-site-specific identifiers
// and is overridden per check when needed.
func (e *Evaluator) runGuardsWithSnapshot(ctx context.Context, projectID int64, task domain.Task, specs []domain.TransitionGuard, operation string, defaultTarget map[string]any, snap *config.Snapshot) error {
	target := map[string]any{"task_id": task.ID}
	if defaultTarget != nil {
		target = defaultTarget
	}
	for _, guard := range specs {
		hint := e.resolveHintFrom(snap, guard.Hint)
		switch guard.Type {
		case "blockers_in":
			if err := e.checkBlockersIn(ctx, projectID, task, guard.Buckets, hint, operation, target, snap); err != nil {
				return err
			}
		case "comments_min":
			if err := e.checkCommentsMin(ctx, projectID, task, guard.Count, hint, operation, target, snap); err != nil {
				return err
			}
		case "comments_tagged":
			if err := e.checkCommentsTagged(ctx, projectID, task, guard.Tag, guard.Count, hint, operation, target, snap); err != nil {
				return err
			}
		case "wave_gate":
			if err := e.checkWaveGate(ctx, projectID, task, hint, operation, target, snap); err != nil {
				return err
			}
		case "subtasks_complete":
			if err := e.checkSubtasksComplete(ctx, projectID, task, hint, operation, target, snap); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveHintFrom expands `${{intl:KEY}}` tokens in guard hint strings via the
// resolved Snapshot's catalog projection so presets can keep hint copy in the
// language catalog instead of hardcoding it in each YAML entry. The inner-layer
// accessor `ResolveGuardHint` shields this package from catalog/surface types
// (see internal/arch/i18n_boundary_test.go).
func (e *Evaluator) resolveHintFrom(snap *config.Snapshot, hint string) string {
	if hint == "" {
		return ""
	}
	if snap == nil {
		return hint
	}
	return snap.ResolveGuardHint(hint)
}

func (e *Evaluator) checkWaveGate(ctx context.Context, projectID int64, task domain.Task, hint, operation string, target map[string]any, snap *config.Snapshot) error {
	pending, err := e.repo.CountPriorWavesPending(ctx, projectID, task.ID, snap)
	if err != nil {
		return err
	}
	if pending == 0 {
		return nil
	}
	msg := fmt.Sprintf("wave_gate guard: %d task(s) pending in prior waves of the same plan", pending)
	details := map[string]any{"pending": pending}
	if hint != "" {
		msg += ". Hint: " + hint
		details["hint"] = hint
	}
	e.EmitViolatedForTask(ctx, projectID, task, snap, operation, "wave_gate", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkSubtasksComplete(ctx context.Context, projectID int64, task domain.Task, hint, operation string, target map[string]any, snap *config.Snapshot) error {
	childParentID := task.ID
	childSnap := snap.For(domain.Task{ParentID: &childParentID})
	if childSnap == nil {
		return nil
	}
	finalKey := childSnap.Workflow().FinalBucketKey()
	finalBucket, ok := childSnap.BucketByKey(finalKey)
	if !ok {
		// Workflow without a final bucket cannot evaluate completeness;
		// treat as satisfied so misconfigured presets don't lock every
		// transition out.
		return nil
	}
	open, found, err := e.repo.FirstChildNotInBucket(ctx, projectID, task.ID, finalBucket.ID, childSnap)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	msg := fmt.Sprintf("subtasks_complete guard: subtask #%d %q is in %q, not %q",
		open.ID, open.Title, open.BucketKey, finalKey)
	details := map[string]any{
		"open_subtask_id":     open.ID,
		"open_subtask_title":  open.Title,
		"open_subtask_bucket": open.BucketKey,
		"final_bucket":        finalKey,
	}
	if hint != "" {
		msg += ". Hint: " + hint
		details["hint"] = hint
	}
	e.EmitViolatedForTask(ctx, projectID, task, snap, operation, "subtasks_complete", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkBlockersIn(ctx context.Context, projectID int64, task domain.Task, allowedKeys []string, hint, operation string, target map[string]any, snap *config.Snapshot) error {
	blockers, err := e.repo.ListTaskBlockerBuckets(ctx, projectID, task.ID, snap)
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
	e.EmitViolatedForTask(ctx, projectID, task, snap, operation, "blockers_in", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkCommentsMin(ctx context.Context, projectID int64, task domain.Task, minCount int, hint, operation string, target map[string]any, snap *config.Snapshot) error {
	count, err := e.repo.CountTaskComments(ctx, projectID, task.ID)
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
	e.EmitViolatedForTask(ctx, projectID, task, snap, operation, "comments_min", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkCommentsTagged(ctx context.Context, projectID int64, task domain.Task, tag string, minCount int, hint, operation string, target map[string]any, snap *config.Snapshot) error {
	if minCount < 1 {
		minCount = 1
	}
	count, err := e.repo.CountTaskCommentsTagged(ctx, projectID, task.ID, tag)
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
	e.EmitViolatedForTask(ctx, projectID, task, snap, operation, "comments_tagged", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}
