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
	// EventTypeComment is the canonical event_type for user/agent
	// comments recorded against a task. The bare value "comment"
	// predates the dotted-namespace convention (comment.edited /
	// comment.removed) but stays as the write value so historical rows
	// keep matching without a destructive rename.
	// EntityType=task, Body=comment text, Tags=optional via event_tags.
	EventTypeComment = "comment"
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
	// EventTypeTaskBucketOrphaned fires per affected sub-task when a
	// subtask_kit enable/disable/swap leaves the sub-task in a bucket
	// absent from the incoming resolved kit. Distinct from task.migrated
	// because the trigger is a per-depth resolved-kit swap (not a root
	// workflow swap) and hooks resolve through the depth-aware dispatcher.
	// EntityType=task, Payload={task_id, parent_id, depth, old_bucket,
	// from_kit, to_kit, resolved_kit, reason}; reason is always
	// bucket_missing_in_resolved_kit for this trigger.
	EventTypeTaskBucketOrphaned = "task.bucket_orphaned"
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

	// EventTypeProjectRemoved fires when ProjectService.Delete removes
	// a project row (and cascades its dependent data). EntityType=
	// project, Payload={slug, name, counters{tasks, comments, plans,
	// tags, activity_log_entries}, backup_path}. The backup_path is
	// the snapshot the destructive flow wrote before the delete, so
	// audit consumers can correlate the event with the recovery
	// artefact.
	EventTypeProjectRemoved = "project.removed"

	// EventTypePlanCreated fires when a plan row is inserted.
	// EntityType=plan, Payload={slug, name, project_id}.
	EventTypePlanCreated = "plan.created"
	// EventTypePlanWaveAdded fires when a wave is appended to a plan.
	// EntityType=plan, Payload={wave_id, name, position}.
	EventTypePlanWaveAdded = "plan.wave_added"
	// EventTypePlanGoalEdited fires when a plan's goal_body is rewritten
	// via plans.update_goal_body. EntityType=plan, Payload={length}.
	EventTypePlanGoalEdited = "plan.goal_edited"
	// EventTypePlanDone fires when a plan auto-transitions to status=done
	// (every child task in a terminal bucket). EntityType=plan, Payload={}.
	EventTypePlanDone = "plan.done"
	// EventTypePlanAbandoned fires when a plan is explicitly aborted.
	// EntityType=plan, Payload={reason?}.
	EventTypePlanAbandoned = "plan.abandoned"
	// EventTypeTaskAssigned fires when tasks.assigned_to is set to a
	// non-empty value (via plans.claim_next or `okt task assign`).
	// EntityType=task, Payload={assignee, source}.
	EventTypeTaskAssigned = "task.assigned"
	// EventTypeTaskUnassigned fires when tasks.assigned_to is cleared,
	// either explicitly or as a side effect of leaving the dev bucket.
	// EntityType=task, Payload={former_assignee}.
	EventTypeTaskUnassigned = "task.unassigned"

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

	// EventTypeOperation is the legacy per-call activity log entry written
	// by activity.Track before the source-discriminated split. Retained
	// only so historic rows and the migration backfill can reference the
	// pre-019 value; new writes use EventTypeCLIToolCall /
	// EventTypeMCPToolCall / EventTypeTUIToolCall.
	//
	// Deprecated: do not emit. Use ToolCallEventTypeForSource for new code.
	EventTypeOperation = "operation"

	// EventTypeCLIToolCall is the per-call activity log entry written by
	// activity.Track for invocations originating in the CLI. Drives the
	// logs view; not surfaced in the activity feed. EntityType=system.
	// Payload mirrors the operation columns so hooks can filter via
	// `when: { tool_name: ..., status: ok }` without reading SQL columns:
	// Payload={tool_name, source, entrypoint, status, duration_ms,
	// error_message, args}.
	EventTypeCLIToolCall = "cli.tool_call"
	// EventTypeMCPToolCall mirrors EventTypeCLIToolCall for MCP-originated
	// tool calls. Same payload contract.
	EventTypeMCPToolCall = "mcp.tool_call"
	// EventTypeTUIToolCall mirrors EventTypeCLIToolCall for TUI-originated
	// tool calls. Same payload contract.
	EventTypeTUIToolCall = "tui.tool_call"

	// EventTypeHookExecuted fires once a hook's action has finished
	// running (success or failure). Emitted from inside the dispatch
	// goroutine after Action.Execute returns; never emitted when the
	// hook was filtered out before dispatch. EntityType=system,
	// Payload={hook_index, action, event_type, target_event_id,
	// success, error, duration_ms}.
	EventTypeHookExecuted = "hook.executed"

	// EventTypeSubtaskKitNoticeEmitted fires once per first-enablement
	// transition of subtask_kit (no sub-kit → some configured path) at
	// the bundle-cache rebuild point. Subsequent same-path reloads,
	// sub-kit swaps, and disable transitions do NOT emit. The audit
	// trail records the i18n key and the resolved kit identities so
	// hooks and downstream UI surfaces can explain the protocol boundary
	// (mcp_commands always resolves at the project root). EntityType=
	// system, Payload={i18n_key, from_kit, to_kit}.
	EventTypeSubtaskKitNoticeEmitted = "subtask_kit.notice_emitted"

	// EventTypeBundleSwapped fires when the active config bundle is
	// replaced through the TUI hot-reload path (Settings → Config picker).
	// EntityType=system, Payload={from_workflow, to_workflow,
	// orphan_count, groups}. The hooks engine uses it to surface
	// migration prompts when orphan_count > 0.
	EventTypeBundleSwapped = "bundle.swapped"

	// EventTypeBundleImported fires after ConfigService.Import (or the
	// hot-reload path) successfully parses a bundle and rotates the
	// in-memory provider snapshot. EntityType=system, Payload={path,
	// hash, workflow_key, workflow_count, persona_count, skill_count,
	// law_count, template_count}. Distinct from bundle.swapped: the
	// `imported` event records "a fresh bundle reached the runtime"
	// (source-of-truth flipped), while `swapped` records "an existing
	// bundle handle was activated" (e.g. project switch). Hooks subscribe
	// to bundle.imported to react when configuration content changes.
	EventTypeBundleImported = "bundle.imported"

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

	// EventTypeTrickExecuted fires once per trick submission from the TUI
	// palette (Ctrl+K overlay). Built-in handlers consume `verb=nav`
	// (screen route resolved via the palette ScreenRegistry) and
	// `verb=op` (entity open by id); any other verb is user-defined and
	// reaches hooks through `when: {verb: <name>, operand: <value>}`
	// filtering. EntityType=system, Payload={verb, operand, raw}.
	EventTypeTrickExecuted = "trick.executed"
)

// ToolCallEventTypeForSource returns the canonical event_type string for
// the per-call activity log entry written by activity.Track. Returns ""
// for sources outside the known cli/mcp/tui set so callers can detect a
// typo before INSERTing a row with a malformed event_type.
func ToolCallEventTypeForSource(source ActivitySource) string {
	switch source {
	case ActivitySourceCLI:
		return EventTypeCLIToolCall
	case ActivitySourceMCP:
		return EventTypeMCPToolCall
	case ActivitySourceTUI:
		return EventTypeTUIToolCall
	}
	return ""
}

// KnownEventTypes is the closed set of event_type values the application
// emits. Used by config validation to reject overrides referencing
// unknown event types (typo guard) and by tests to assert catalog
// completeness. Order is informational; consumers must not depend on it.
//
// EventTypeOperation is excluded because it is the pre-019 legacy value
// no longer emitted by activity.Track — the three EventType*ToolCall
// constants supersede it.
var KnownEventTypes = []string{
	EventTypeComment,
	EventTypeCommentEdited,
	EventTypeCommentRemoved,
	EventTypeTaskCreated,
	EventTypeTaskMoved,
	EventTypeTaskMigrated,
	EventTypeTaskBucketOrphaned,
	EventTypeTaskCompleted,
	EventTypeTaskEdited,
	EventTypeTaskRemoved,
	EventTypeTaskArchived,
	EventTypeTaskUnarchived,
	EventTypeTaskAssigned,
	EventTypeTaskUnassigned,
	EventTypeProjectRemoved,
	EventTypePlanCreated,
	EventTypePlanWaveAdded,
	EventTypePlanGoalEdited,
	EventTypePlanDone,
	EventTypePlanAbandoned,
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
	EventTypeBundleImported,
	EventTypeSubtaskKitNoticeEmitted,
	EventTypeConfirmationGranted,
	EventTypeCLIToolCall,
	EventTypeMCPToolCall,
	EventTypeTUIToolCall,
	EventTypeTrickExecuted,
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
	// EventEntityPlan scopes events tied to a plan row (entity_id is the
	// plan id). Wave events also land under this entity scope — the wave
	// id travels in the payload because the activity feed groups by plan.
	EventEntityPlan = "plan"
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
