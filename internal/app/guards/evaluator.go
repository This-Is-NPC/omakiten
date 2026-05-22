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
func (e *Evaluator) EvaluateTransition(ctx context.Context, projectID, taskID, fromBucketID, toBucketID int64, targetBucketKey string) error {
	specs := e.snap.Guards(fromBucketID, toBucketID)
	target := map[string]any{"task_id": taskID, "from_bucket_id": fromBucketID, "to_bucket_id": toBucketID, "to_bucket": targetBucketKey}
	return e.runGuards(ctx, projectID, taskID, specs, OperationTaskTransition, target)
}

// EvaluateOperation runs every guard declared on the named operation against
// the named task. operation is one of the Operation* constants.
func (e *Evaluator) EvaluateOperation(ctx context.Context, projectID, taskID int64, operation string) error {
	workflow := e.snap.Workflow()
	specs := operationGuards(workflow, operation)
	if len(specs) == 0 {
		return nil
	}
	return e.runGuards(ctx, projectID, taskID, specs, OperationPayloadName(operation), nil)
}

// EmitViolated records a guard.violated domain event. operation and rule are
// free-form strings — call sites pick the values that name the operation
// precisely. target carries identifiers (task_id, comment_id, from_bucket,
// to_bucket). attempted_by is derived from the request source: mcp -> "agent",
// anything else -> "user". Telemetry must not break business logic; emission
// errors are swallowed.
func (e *Evaluator) EmitViolated(ctx context.Context, projectID int64, entityType string, entityID int64, operation, rule, hint string, target map[string]any) {
	if e.events == nil {
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
		"attempted_by": attemptedBy(ctx),
	})
	if err != nil {
		return
	}
	_ = e.events.RecordEntityEvent(ctx, entityType, entityID, projectID, domain.EventTypeGuardViolated, string(body))
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

// runGuards is the shared dispatch. Both transition guards and operation
// guards (archive/delete/unarchive) feed through here so a new guard type
// only needs one switch arm. operation labels the call site for the
// guard.violated payload; defaultTarget carries call-site-specific
// identifiers and is overridden per check when needed.
func (e *Evaluator) runGuards(ctx context.Context, projectID, taskID int64, specs []domain.TransitionGuard, operation string, defaultTarget map[string]any) error {
	target := map[string]any{"task_id": taskID}
	if defaultTarget != nil {
		target = defaultTarget
	}
	for _, guard := range specs {
		hint := e.resolveHint(guard.Hint)
		switch guard.Type {
		case "blockers_in":
			if err := e.checkBlockersIn(ctx, projectID, taskID, guard.Buckets, hint, operation, target); err != nil {
				return err
			}
		case "comments_min":
			if err := e.checkCommentsMin(ctx, projectID, taskID, guard.Count, hint, operation, target); err != nil {
				return err
			}
		case "comments_tagged":
			if err := e.checkCommentsTagged(ctx, projectID, taskID, guard.Tag, guard.Count, hint, operation, target); err != nil {
				return err
			}
		case "wave_gate":
			if err := e.checkWaveGate(ctx, projectID, taskID, hint, operation, target); err != nil {
				return err
			}
		case "subtasks_complete":
			if err := e.checkSubtasksComplete(ctx, projectID, taskID, hint, operation, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolveHint expands `${{intl:KEY}}` tokens in guard hint strings via the
// Snapshot's catalog projection so presets can keep hint copy in the
// language catalog instead of hardcoding it in each YAML entry. The
// inner-layer accessor `ResolveGuardHint` shields this package from
// catalog/surface types (see internal/arch/i18n_boundary_test.go).
func (e *Evaluator) resolveHint(hint string) string {
	if hint == "" {
		return ""
	}
	return e.snap.ResolveGuardHint(hint)
}

func (e *Evaluator) checkWaveGate(ctx context.Context, projectID, taskID int64, hint, operation string, target map[string]any) error {
	pending, err := e.repo.CountPriorWavesPending(ctx, projectID, taskID, e.snap)
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
	e.EmitViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "wave_gate", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkSubtasksComplete(ctx context.Context, projectID, taskID int64, hint, operation string, target map[string]any) error {
	// The final bucket comes from the per-project workflow snapshot —
	// every preset that wires this guard inherits the same notion of
	// "done" the rest of the engine uses (FinalBucketKey).
	finalKey := e.snap.Workflow().FinalBucketKey()
	finalBucket, ok := e.snap.BucketByKey(finalKey)
	if !ok {
		// Workflow without a final bucket cannot evaluate completeness;
		// treat as satisfied so misconfigured presets don't lock every
		// transition out.
		return nil
	}
	open, found, err := e.repo.FirstChildNotInBucket(ctx, projectID, taskID, finalBucket.ID, e.snap)
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
	e.EmitViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "subtasks_complete", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkBlockersIn(ctx context.Context, projectID, taskID int64, allowedKeys []string, hint, operation string, target map[string]any) error {
	blockers, err := e.repo.ListTaskBlockerBuckets(ctx, projectID, taskID, e.snap)
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
	e.EmitViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "blockers_in", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkCommentsMin(ctx context.Context, projectID, taskID int64, minCount int, hint, operation string, target map[string]any) error {
	count, err := e.repo.CountTaskComments(ctx, projectID, taskID)
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
	e.EmitViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "comments_min", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}

func (e *Evaluator) checkCommentsTagged(ctx context.Context, projectID, taskID int64, tag string, minCount int, hint, operation string, target map[string]any) error {
	if minCount < 1 {
		minCount = 1
	}
	count, err := e.repo.CountTaskCommentsTagged(ctx, projectID, taskID, tag)
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
	e.EmitViolated(ctx, projectID, domain.EventEntityTask, taskID, operation, "comments_tagged", msg, target)
	return domain.NewError(domain.ErrGuardViolation, msg, details)
}
