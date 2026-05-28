package domain

// EventCategory is the coarse bucket the Logs inspector groups
// event_type values into. The Logs filter chip (TUI key `F` in the
// unified inspector) cycles through subsets composed of these
// categories, and the summary panel tallies counts per category.
//
// The set is closed: every value in KnownEventTypes must map to exactly
// one EventCategory through EventCategoryOf. The TestEventCategoryOf*
// tests in this package lock that parity — adding a new event_type
// without a corresponding switch arm fails the build.
type EventCategory string

const (
	// EventCategoryTask groups task lifecycle events: created, moved,
	// edited, archived/unarchived, removed, assigned/unassigned,
	// migrated, bucket_orphaned, completed.
	EventCategoryTask EventCategory = "task"
	// EventCategoryComment groups comment write events.
	EventCategoryComment EventCategory = "comment"
	// EventCategoryPlan groups plan / wave lifecycle events.
	EventCategoryPlan EventCategory = "plan"
	// EventCategoryTagDep groups tag attach/detach and dependency
	// add/remove events. Both are entity-relationship edges so they
	// share a category to keep the chip count low.
	EventCategoryTagDep EventCategory = "tag-dep"
	// EventCategoryGuard groups guard-violation events.
	EventCategoryGuard EventCategory = "guard"
	// EventCategoryAudit groups domain audit events recorded by the
	// canonical service layer (error.*, solution.*, project.removed,
	// confirmation.granted). Used by metrics.summary.
	EventCategoryAudit EventCategory = "audit"
	// EventCategoryHook groups hook dispatch events.
	EventCategoryHook EventCategory = "hook"
	// EventCategoryToolCall groups cli/mcp/tui per-invocation activity
	// log entries.
	EventCategoryToolCall EventCategory = "tool_call"
	// EventCategoryTrick groups TUI palette submissions.
	EventCategoryTrick EventCategory = "trick"
	// EventCategoryDomain groups infrastructure / domain bookkeeping
	// events that don't fit the other buckets (bundle.swapped,
	// bundle.imported, subtask_kit.notice_emitted).
	EventCategoryDomain EventCategory = "domain"
	// EventCategoryUnknown is returned by EventCategoryOf when the
	// event_type is not in KnownEventTypes. The Logs inspector renders
	// such rows under a generic "other" group and never panics.
	EventCategoryUnknown EventCategory = "unknown"
)

// KnownEventCategories is the closed set of categories the Logs
// inspector can group on. Order is informational; consumers must not
// depend on it. EventCategoryUnknown is excluded — it is a fallback,
// not a category a user can opt into.
var KnownEventCategories = []EventCategory{
	EventCategoryTask,
	EventCategoryComment,
	EventCategoryPlan,
	EventCategoryTagDep,
	EventCategoryGuard,
	EventCategoryAudit,
	EventCategoryHook,
	EventCategoryToolCall,
	EventCategoryTrick,
	EventCategoryDomain,
}

// EventCategoryOf returns the category an event_type belongs to.
// Returns EventCategoryUnknown for values outside KnownEventTypes —
// callers render those rows under a generic bucket rather than panic.
//
// The switch has one arm per KnownEventTypes entry; the enumeration
// test in event_category_test.go fails when a new event_type lands in
// event.go without a category arm here.
func EventCategoryOf(eventType string) EventCategory {
	switch eventType {
	// Task lifecycle.
	case EventTypeTaskCreated,
		EventTypeTaskMoved,
		EventTypeTaskMigrated,
		EventTypeTaskBucketOrphaned,
		EventTypeTaskCompleted,
		EventTypeTaskEdited,
		EventTypeTaskRemoved,
		EventTypeTaskArchived,
		EventTypeTaskUnarchived,
		EventTypeTaskAssigned,
		EventTypeTaskUnassigned:
		return EventCategoryTask

	// Comments.
	case EventTypeComment,
		EventTypeCommentEdited,
		EventTypeCommentRemoved:
		return EventCategoryComment

	// Plan / wave lifecycle.
	case EventTypePlanCreated,
		EventTypePlanWaveAdded,
		EventTypePlanGoalEdited,
		EventTypePlanDone,
		EventTypePlanAbandoned:
		return EventCategoryPlan

	// Tags + dependencies (entity-relationship edges).
	case EventTypeTagAdded,
		EventTypeTagRemoved,
		EventTypeDependencyAdded,
		EventTypeDependencyRemoved:
		return EventCategoryTagDep

	// Guards.
	case EventTypeGuardViolated:
		return EventCategoryGuard

	// Audit — domain service emissions, confirmations, project removal.
	case EventTypeProjectRemoved,
		EventTypeConfirmationGranted,
		EventTypeErrorRecorded,
		EventTypeErrorSearched,
		EventTypeSolutionAdded,
		EventTypeSolutionConfirmed,
		EventTypeSolutionLiked,
		EventTypeSolutionFailed,
		EventTypeSolutionViewedTop:
		return EventCategoryAudit

	// Hook dispatch.
	case EventTypeHookExecuted:
		return EventCategoryHook

	// Tool calls.
	case EventTypeCLIToolCall,
		EventTypeMCPToolCall,
		EventTypeTUIToolCall:
		return EventCategoryToolCall

	// Trick palette.
	case EventTypeTrickExecuted:
		return EventCategoryTrick

	// Domain / infrastructure bookkeeping.
	case EventTypeBundleSwapped,
		EventTypeBundleImported,
		EventTypeSubtaskKitNoticeEmitted:
		return EventCategoryDomain
	}
	return EventCategoryUnknown
}
