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
	"maps"
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
	// CountPlanCommentsTagged gates the plan_comments_tagged plan guard.
	CountPlanCommentsTagged(ctx context.Context, projectID, planID int64, tagName string) (int, error)
	// PlanGateState and TaskPlanGateState read a plan's status and its done
	// and total active tasks, done meaning in finalBucketID; ok is false
	// when the task is in no plan.
	PlanGateState(ctx context.Context, projectID, planID, finalBucketID int64) (domain.PlanGateState, error)
	TaskPlanGateState(ctx context.Context, projectID, taskID, finalBucketID int64) (domain.PlanGateState, bool, error)
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
	statuses, err := e.transitionStatuses(ctx, projectID, task, fromBucketID, toBucketID, snap)
	if err != nil {
		return err
	}
	target := map[string]any{"task_id": task.ID, "from_bucket_id": fromBucketID, "to_bucket_id": toBucketID, "to_bucket": targetBucketKey}
	return e.reportUnmet(ctx, projectID, task, snap, OperationTaskTransition, target, statuses)
}

// transitionStatuses evaluates the guards of the (from, to) transition and
// then the gates of the task's plan that the move meets.
func (e *Evaluator) transitionStatuses(ctx context.Context, projectID int64, task domain.Task, fromBucketID, toBucketID int64, snap *config.Snapshot) ([]domain.GuardStatus, error) {
	statuses, err := e.evaluate(ctx, projectID, task, snap.Guards(fromBucketID, toBucketID), snap)
	if err != nil {
		return nil, err
	}
	gates, err := e.transitionPlanGates(ctx, projectID, task, fromBucketID, toBucketID, snap)
	if err != nil {
		return nil, err
	}
	return append(statuses, gates...), nil
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
	e.emitViolatedForTask(ctx, projectID, task, snap, operation, rule, hint, target, nil)
}

// emitViolatedForTask is EmitViolatedForTask with fields added at the top
// level of the payload.
func (e *Evaluator) emitViolatedForTask(ctx context.Context, projectID int64, task domain.Task, snap *config.Snapshot, operation, rule, hint string, target, fields map[string]any) {
	if e.events == nil {
		return
	}
	subject := map[string]any{}
	if snap != nil {
		taskSnap := snap.For(task)
		kitKey := ""
		if taskSnap != nil {
			kitKey = taskSnap.Kit().Key
		}
		maps.Copy(subject, taskSubjectFields(task, kitKey))
	}
	maps.Copy(subject, fields)
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

// attemptedBy identifies agent calls by their model provenance.
func attemptedBy(ctx context.Context) string {
	_, _, model, _, _ := activity.FromContext(ctx)
	if model != "" && model != "human" {
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
// type only needs one switch arm. Every guard is evaluated; each unmet one
// records its own guard.violated event, and the returned guard_violation lists
// them all. operation labels the call site for the guard.violated payload;
// defaultTarget carries call-site-specific identifiers.
func (e *Evaluator) runGuardsWithSnapshot(ctx context.Context, projectID int64, task domain.Task, specs []domain.TransitionGuard, operation string, defaultTarget map[string]any, snap *config.Snapshot) error {
	target := map[string]any{"task_id": task.ID}
	if defaultTarget != nil {
		target = defaultTarget
	}
	statuses, err := e.evaluate(ctx, projectID, task, specs, snap)
	if err != nil {
		return err
	}
	return e.reportUnmet(ctx, projectID, task, snap, operation, target, statuses)
}

// reportUnmet records a guard.violated event for each unmet status and
// returns the guard_violation listing them all, or nil when every status is
// met.
func (e *Evaluator) reportUnmet(ctx context.Context, projectID int64, task domain.Task, snap *config.Snapshot, operation string, target map[string]any, statuses []domain.GuardStatus) error {
	var unmet []domain.GuardStatus
	var messages []string
	for _, status := range statuses {
		if status.Satisfied {
			continue
		}
		e.emitViolatedForTask(ctx, projectID, task, snap, operation, status.Type, status.Message, target, planGateFields(status))
		unmet = append(unmet, status)
		messages = append(messages, status.Message)
	}
	if len(unmet) == 0 {
		return nil
	}
	return violation(unmet, messages, map[string]any{"task_id": task.ID})
}

// violation builds the guard_violation error for unmet statuses: the first
// one's details, type, hint and fix, ids naming the subject, and the full
// unmet list.
func violation(unmet []domain.GuardStatus, messages []string, ids map[string]any) error {
	details := make(map[string]any, len(unmet[0].Details)+len(ids)+4)
	for k, v := range unmet[0].Details {
		details[k] = v
	}
	details["guard_type"] = unmet[0].Type
	for k, v := range ids {
		details[k] = v
	}
	if unmet[0].Hint != "" {
		details["hint"] = unmet[0].Hint
	}
	if unmet[0].Fix != "" {
		details["fix"] = unmet[0].Fix
	}
	details["unmet"] = unmet
	return domain.NewError(domain.ErrGuardViolation, strings.Join(messages, "; "), details)
}

// PreviewTransitionForTask reports the state of every guard on the
// (from, to) transition without recording events. A nil snap falls back to
// the evaluator's root snapshot.
func (e *Evaluator) PreviewTransitionForTask(ctx context.Context, projectID int64, task domain.Task, fromBucketID, toBucketID int64, snap *config.Snapshot) ([]domain.GuardStatus, error) {
	snap = e.resolvedSnapshot(snap)
	if snap == nil {
		return nil, nil
	}
	return e.transitionStatuses(ctx, projectID, task, fromBucketID, toBucketID, snap)
}

// evaluate returns the state of each guard of a known type, in declaration
// order. It records nothing.
func (e *Evaluator) evaluate(ctx context.Context, projectID int64, task domain.Task, specs []domain.TransitionGuard, snap *config.Snapshot) ([]domain.GuardStatus, error) {
	var out []domain.GuardStatus
	for _, guard := range specs {
		status, known, err := e.check(ctx, projectID, task, guard, snap)
		if err != nil {
			return nil, err
		}
		if !known {
			continue
		}
		out = append(out, e.finish(snap, guard, status))
	}
	return out, nil
}

// finish stamps the guard type on status and, when it is unmet, folds the
// resolved hint and the fix into its message; a met status keeps no fix.
func (e *Evaluator) finish(snap *config.Snapshot, guard domain.TransitionGuard, status domain.GuardStatus) domain.GuardStatus {
	status.Type = guard.Type
	if status.Satisfied {
		status.Fix = ""
		return status
	}
	status.Hint = e.resolveHintFrom(snap, guard.Hint)
	if status.Hint != "" {
		status.Message += ". Hint: " + status.Hint
	}
	if status.Fix != "" {
		status.Message += ". Fix: " + status.Fix
	}
	return status
}

// check evaluates one guard. known is false for guard types the evaluator
// does not implement; those never block.
func (e *Evaluator) check(ctx context.Context, projectID int64, task domain.Task, guard domain.TransitionGuard, snap *config.Snapshot) (status domain.GuardStatus, known bool, err error) {
	switch guard.Type {
	case "blockers_in":
		status, err = e.checkBlockersIn(ctx, projectID, task, guard.Buckets, snap)
	case "comments_min":
		status, err = e.checkCommentsMin(ctx, projectID, task, guard.Count)
	case "comments_tagged":
		status, err = e.checkCommentsTagged(ctx, projectID, task, guard.Tag, guard.Count)
	case "wave_gate":
		status, err = e.checkWaveGate(ctx, projectID, task, snap)
	case "subtasks_complete":
		status, err = e.checkSubtasksComplete(ctx, projectID, task, snap)
	default:
		return domain.GuardStatus{}, false, nil
	}
	return status, true, err
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

func (e *Evaluator) checkWaveGate(ctx context.Context, projectID int64, task domain.Task, snap *config.Snapshot) (domain.GuardStatus, error) {
	pending, err := e.repo.CountPriorWavesPending(ctx, projectID, task.ID, snap)
	if err != nil {
		return domain.GuardStatus{}, err
	}
	return domain.GuardStatus{
		Satisfied: pending == 0,
		Details:   map[string]any{"pending": pending},
		Message:   fmt.Sprintf("wave_gate guard: %d task(s) pending in prior waves of the same plan", pending),
	}, nil
}

func (e *Evaluator) checkSubtasksComplete(ctx context.Context, projectID int64, task domain.Task, snap *config.Snapshot) (domain.GuardStatus, error) {
	childParentID := task.ID
	childSnap := snap.For(domain.Task{ParentID: &childParentID})
	if childSnap == nil {
		return domain.GuardStatus{Satisfied: true}, nil
	}
	finalKey := childSnap.Workflow().FinalBucketKey()
	finalBucket, ok := childSnap.BucketByKey(finalKey)
	if !ok {
		// Workflow without a final bucket cannot evaluate completeness;
		// treat as satisfied so misconfigured presets don't lock every
		// transition out.
		return domain.GuardStatus{Satisfied: true}, nil
	}
	open, found, err := e.repo.FirstChildNotInBucket(ctx, projectID, task.ID, finalBucket.ID, childSnap)
	if err != nil {
		return domain.GuardStatus{}, err
	}
	if !found {
		return domain.GuardStatus{Satisfied: true, Details: map[string]any{"final_bucket": finalKey}}, nil
	}
	return domain.GuardStatus{
		Details: map[string]any{
			"open_subtask_id":     open.ID,
			"open_subtask_title":  open.Title,
			"open_subtask_bucket": open.BucketKey,
			"final_bucket":        finalKey,
		},
		Fix:     continueFix(open.ID),
		Message: fmt.Sprintf("subtasks_complete guard: subtask #%d %q is in %q, not %q", open.ID, open.Title, open.BucketKey, finalKey),
	}, nil
}

func (e *Evaluator) checkBlockersIn(ctx context.Context, projectID int64, task domain.Task, allowedKeys []string, snap *config.Snapshot) (domain.GuardStatus, error) {
	blockers, err := e.repo.ListTaskBlockerBuckets(ctx, projectID, task.ID, snap)
	if err != nil {
		return domain.GuardStatus{}, err
	}
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, k := range allowedKeys {
		allowed[k] = struct{}{}
	}
	var pending []string
	var firstPending int64
	for _, b := range blockers {
		if _, ok := allowed[b.BucketKey]; !ok {
			if firstPending == 0 {
				firstPending = b.TaskID
			}
			pending = append(pending, fmt.Sprintf("#%d %q (in %q)", b.TaskID, b.Title, b.BucketKey))
		}
	}
	if len(pending) == 0 {
		return domain.GuardStatus{Satisfied: true}, nil
	}
	return domain.GuardStatus{
		Details: map[string]any{"pending_blockers": pending},
		Fix:     continueFix(firstPending),
		Message: "blockers_in guard: pending blockers: " + strings.Join(pending, ", "),
	}, nil
}

func (e *Evaluator) checkCommentsMin(ctx context.Context, projectID int64, task domain.Task, minCount int) (domain.GuardStatus, error) {
	count, err := e.repo.CountTaskComments(ctx, projectID, task.ID)
	if err != nil {
		return domain.GuardStatus{}, err
	}
	return domain.GuardStatus{
		Satisfied: count >= minCount,
		Details:   map[string]any{"count": count, "required": minCount},
		Fix:       commentFix(ctx, task.ID, "", "<note>"),
		Message:   fmt.Sprintf("comments_min guard: task has %d comment(s); transition requires at least %d", count, minCount),
	}, nil
}

func (e *Evaluator) checkCommentsTagged(ctx context.Context, projectID int64, task domain.Task, tag string, minCount int) (domain.GuardStatus, error) {
	if minCount < 1 {
		minCount = 1
	}
	count, err := e.repo.CountTaskCommentsTagged(ctx, projectID, task.ID, tag)
	if err != nil {
		return domain.GuardStatus{}, err
	}
	return domain.GuardStatus{
		Satisfied: count >= minCount,
		Details:   map[string]any{"count": count, "required": minCount, "tag": tag},
		Fix:       commentFix(ctx, task.ID, tag, "<evidence>"),
		Message:   fmt.Sprintf("comments_tagged guard: task has %d comment(s) tagged %q; transition requires at least %d", count, tag, minCount),
	}, nil
}
