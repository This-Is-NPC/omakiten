package domain

// EventCategory is the coarse bucket the Logs inspector groups
// event_type values into. The Logs filter chip (TUI key `F` in the
// unified inspector) cycles through subsets composed of these
// categories, and the summary panel tallies counts per category.
//
// The set is closed: every value in KnownEventTypes must map to exactly
// one EventCategory through EventCategoryOf. The TestEventCategoryOf*
// tests in this package lock that parity — adding a new event_type
// to the YAML registry without a matching category entry trips the
// closed-set assertion at boot.
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
	// EventCategoryUpdate groups `okt update` lifecycle events: pre-swap
	// health-check pass/fail, swap completion, swap abort. Added in
	// #368 so the Logs inspector can filter the upgrade audit trail
	// distinctly from other system events.
	EventCategoryUpdate EventCategory = "update"
	// EventCategoryTUI groups `okt tui` lifecycle events. Today only
	// boot-time health-check failure is recorded (passing boot is the
	// steady-state path and intentionally skipped) but the category
	// exists so future TUI audit events have a home without growing
	// the catalogue per-event.
	EventCategoryTUI EventCategory = "tui"
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
	EventCategoryUpdate,
	EventCategoryTUI,
}
