package domain

// Event types stored in the unified `events` table. The activity feed
// (entity_type='task') and the logs view (event_type='operation') both
// read from the same source — discriminator columns keep them apart.
//
// Naming convention is `<entity>.<action>` in past tense (`task.created`,
// not `task.create`). Granularity belongs in the payload, not the name —
// `guard.violated` carries the operation/rule in its payload so consumers
// can filter without the catalog growing per-rule constants.
//
// When adding a new event: declare the const here with a one-line godoc
// describing when it fires, the entity_type column value it lands under,
// and the minimum payload contract; emit it in the canonical service or
// sqlite layer; document it in `.docs/domain-events.md`.
const (
	// EventTypeComment is the legacy event_type for user/agent comments
	// recorded against a task. Kept as the canonical value to avoid a
	// destructive rename of historical rows; new code may reference
	// EventTypeCommentCreated as a forward-compatible alias.
	// EntityType=task, Body=comment text, Tags=optional via event_tags.
	EventTypeComment = "comment"
	// EventTypeCommentCreated is the forward-compatible alias for
	// EventTypeComment. Both values denote the same row shape; readers
	// that switch on event_type should accept either.
	EventTypeCommentCreated = "comment"
	// EventTypeCommentLegacy mirrors EventTypeComment for callers that
	// want the intent-revealing name when filtering historical rows.
	EventTypeCommentLegacy = "comment"
	// EventTypeCommentEdited fires when a comment's body or tag set is
	// updated. EntityType=task, Payload={comment_id, body:{from,to}}.
	EventTypeCommentEdited = "comment.edited"
	// EventTypeCommentRemoved fires when a comment is hard-deleted.
	// EntityType=task, Payload={comment_id, author_type, body}.
	EventTypeCommentRemoved = "comment.removed"

	// EventTypeTaskCreated fires when a task row is inserted.
	// EntityType=task, Payload={title, bucket, priority}.
	EventTypeTaskCreated = "task.created"
	// EventTypeTaskMoved fires when a task transitions between buckets.
	// EntityType=task, Payload={from, to}.
	EventTypeTaskMoved = "task.moved"
	// EventTypeTaskMigrated fires when a task is rebinded to a new bucket
	// because the active workflow changed (preset swap, bucket removed or
	// renamed). Distinct from task.moved because the trigger is a config
	// change, not a workflow transition — transition guards are bypassed.
	// EntityType=task, Payload={from, to, reason}.
	EventTypeTaskMigrated = "task.migrated"
	// EventTypeTaskCompleted fires when a task moves into the workflow's
	// final bucket. Co-emits with task.moved on the same transition.
	// EntityType=task, Payload={bucket}.
	EventTypeTaskCompleted = "task.completed"
	// EventTypeTaskEdited fires when a task's mutable fields change.
	// EntityType=task, Payload={fields:{<field>:{from,to}}}.
	EventTypeTaskEdited = "task.edited"
	// EventTypeTaskRemoved fires when a task is hard-deleted.
	// EntityType=task, Payload={title, bucket, priority}.
	EventTypeTaskRemoved = "task.removed"
	// EventTypeTaskArchived fires when a task is moved to the archived
	// state. EntityType=task, Payload={bucket}.
	EventTypeTaskArchived = "task.archived"
	// EventTypeTaskUnarchived fires when a previously-archived task is
	// restored. EntityType=task, Payload={bucket}.
	EventTypeTaskUnarchived = "task.unarchived"

	// EventTypeTagAdded fires when a tag is attached to an entity.
	// EntityType=task|project|comment|error per Payload.entity_type.
	// Payload={entity_type, entity_id, tag_id, tag_name}.
	EventTypeTagAdded = "tag.added"
	// EventTypeTagRemoved fires when a tag is detached from an entity.
	// EntityType=task|project|comment|error per Payload.entity_type.
	// Payload={entity_type, entity_id, tag_id, tag_name}.
	EventTypeTagRemoved = "tag.removed"

	// EventTypeDependencyAdded fires when a task→task dependency edge
	// is inserted. EntityType=task (the dependent), Payload={depends_on_task_id}.
	EventTypeDependencyAdded = "dependency.added"
	// EventTypeDependencyRemoved fires when a task→task dependency edge
	// is deleted. EntityType=task (the dependent), Payload={depends_on_task_id}.
	EventTypeDependencyRemoved = "dependency.removed"

	// EventTypeGuardViolated fires when any operation is rejected by a
	// configured guard (transition guard, archive/delete/unarchive
	// operation guard, CRUD permission). EntityType=task|comment per
	// Payload.target. Payload={operation, rule, hint, target, attempted_by}
	// — operation and rule are free-form strings supplied by the call
	// site so consumers can filter without the catalog growing per-rule
	// constants. attempted_by mirrors the request's author_type.
	EventTypeGuardViolated = "guard.violated"

	// EventTypeOperation is the per-call activity log entry written by
	// activity.Track. Drives the logs view; not surfaced in the activity
	// feed. EntityType=system, Source/Operation/Status/DurationMs populated.
	EventTypeOperation = "operation"

	// EventTypeHookExecuted fires once a hook's action has finished
	// running (success or failure). Emitted from inside the dispatch
	// goroutine after Action.Execute returns; never emitted when the
	// hook was filtered out before dispatch. EntityType=system,
	// Payload={hook_index, action, event_type, target_event_id,
	// success, error, duration_ms}.
	EventTypeHookExecuted = "hook.executed"

	// EventTypeBundleSwapped fires when the active config bundle is
	// replaced through the TUI hot-reload path (Settings → Config picker).
	// EntityType=system, Payload={from_workflow, to_workflow,
	// orphan_count, groups}. The hooks engine uses it to surface
	// migration prompts when orphan_count > 0.
	EventTypeBundleSwapped = "bundle.swapped"

	// EventTypeConfirmationGranted fires immediately before the TUI
	// dispatches a non-empty NotificationAction.Command in response to a
	// user keystroke. The audit log captures every CLI invocation that
	// was authorised through an interactive prompt so reviewers can
	// trace human-approved automation. EntityType=system,
	// Payload={notification_slug, action_id, command}; author_type
	// flows from ctx — `human` for the TUI surface, `agent` for any
	// future MCP-triggered confirmation flow.
	EventTypeConfirmationGranted = "confirmation.granted"

	// Domain events emitted from the canonical service layer when an
	// error or solution is recorded, searched, added, liked, etc. Used by
	// metrics.summary to benchmark agents — which models record vs search
	// vs reuse existing knowledge.

	// EventTypeErrorRecorded fires when ErrorService.Record persists a
	// new error row. EntityType=error, Payload={tags, has_context}.
	EventTypeErrorRecorded = "error.recorded"
	// EventTypeErrorSearched fires when ErrorService.Search runs.
	// EntityType=error (entity_id=0), Payload={query, tags, result_count}.
	EventTypeErrorSearched = "error.searched"
	// EventTypeSolutionAdded fires when ErrorService.AddSolution
	// persists a candidate. EntityType=solution, Payload={error_id}.
	EventTypeSolutionAdded = "solution.added"
	// EventTypeSolutionConfirmed fires whenever ErrorService.ConfirmSolution
	// runs, regardless of outcome. Co-emits with solution.liked or
	// solution.failed (which carry the outcome). EntityType=solution,
	// Payload={error_id, success, likes}.
	EventTypeSolutionConfirmed = "solution.confirmed"
	// EventTypeSolutionLiked fires when ConfirmSolution(success=true).
	// EntityType=solution, Payload={error_id, likes}.
	EventTypeSolutionLiked = "solution.liked"
	// EventTypeSolutionFailed fires when ConfirmSolution(success=false).
	// EntityType=solution, Payload={error_id, likes}.
	EventTypeSolutionFailed = "solution.failed"
	// EventTypeSolutionViewedTop fires when ListTopSolutions runs.
	// EntityType=solution (entity_id=0), Payload={limit, returned_count}.
	EventTypeSolutionViewedTop = "solution.viewed_top"
)

// KnownEventTypes is the closed set of event_type values the application
// emits. Used by config validation to reject overrides referencing
// unknown event types (typo guard) and by tests to assert catalog
// completeness. Order is informational; consumers must not depend on it.
//
// EventTypeOperation is excluded because it is written by activity.Track
// rather than the domain emit path and is not configurable per-event in
// `config.events.overrides`.
var KnownEventTypes = []string{
	EventTypeComment,
	EventTypeCommentEdited,
	EventTypeCommentRemoved,
	EventTypeTaskCreated,
	EventTypeTaskMoved,
	EventTypeTaskMigrated,
	EventTypeTaskCompleted,
	EventTypeTaskEdited,
	EventTypeTaskRemoved,
	EventTypeTaskArchived,
	EventTypeTaskUnarchived,
	EventTypeTagAdded,
	EventTypeTagRemoved,
	EventTypeDependencyAdded,
	EventTypeDependencyRemoved,
	EventTypeGuardViolated,
	EventTypeErrorRecorded,
	EventTypeErrorSearched,
	EventTypeSolutionAdded,
	EventTypeSolutionConfirmed,
	EventTypeSolutionLiked,
	EventTypeSolutionFailed,
	EventTypeSolutionViewedTop,
	EventTypeHookExecuted,
	EventTypeBundleSwapped,
	EventTypeConfirmationGranted,
}

// IsKnownEventType reports whether s matches one of KnownEventTypes.
// Used by config validation.
func IsKnownEventType(s string) bool {
	for _, t := range KnownEventTypes {
		if t == s {
			return true
		}
	}
	return false
}

const (
	// EventEntityTask scopes events to a task row (entity_id is the task id).
	EventEntityTask = "task"
	// EventEntitySystem scopes events that don't tie to a single row
	// (e.g. solution.viewed_top, error.searched).
	EventEntitySystem = "system"
	// EventEntityProject scopes events whose primary subject is a project
	// (project tag adds/removes today).
	EventEntityProject = "project"
	// EventEntityError scopes events tied to an error row.
	EventEntityError = "error"
	// EventEntitySolution scopes events tied to a solution row.
	EventEntitySolution = "solution"
)

// Event is the row shape of the unified events log. Different event_types
// populate different subsets of the fields — comments use Body/AuthorType,
// system events (task.*) use Payload, operations use Source/Operation/
// Status/DurationMs. Treat absent fields as empty.
type Event struct {
	ID           int64  `json:"id"`
	EntityType   string `json:"entity_type"`
	EntityID     int64  `json:"entity_id,omitempty"`
	ProjectID    int64  `json:"project_id,omitempty"`
	ProjectSlug  string `json:"project_slug,omitempty"`
	EventType    string `json:"event_type"`
	Body         string `json:"body,omitempty"`
	Payload      string `json:"payload,omitempty"`
	AuthorType   string `json:"author_type,omitempty"`
	Source       string `json:"source,omitempty"`
	Entrypoint   string `json:"entrypoint,omitempty"`
	Operation    string `json:"operation,omitempty"`
	Status       string `json:"status,omitempty"`
	DurationMs   int    `json:"duration_ms,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt      string `json:"created_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
	Tags           []Tag  `json:"tags,omitempty"`
	AgentModel     string `json:"agent_model,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
}
