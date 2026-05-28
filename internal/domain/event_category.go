package domain

import "sort"

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

// EventCategoryOf returns the category an event_type belongs to,
// resolved through the YAML-loaded registry (EventDefByKey). Returns
// EventCategoryUnknown for values outside the registry — callers render
// those rows under a generic bucket rather than panic.
//
// Until LoadEventRegistryFromYAML has populated the registry every input
// resolves to EventCategoryUnknown, including known constants — boot
// wiring must hydrate the registry before consumers run.
func EventCategoryOf(eventType string) EventCategory {
	def, ok := EventDefByKey[eventType]
	if !ok {
		return EventCategoryUnknown
	}
	return def.Category
}

// EventTypesForCategory returns every event_type that maps to the given
// category, walking EventDefinitions in O(N) on each call. N≈41 keeps
// the linear scan cheap and avoids a rebuild step after registry reloads.
// The output is sorted by event_type for deterministic SQL IN lists.
// Unknown categories (including EventCategoryUnknown by construction)
// return nil so callers can treat the result as a distinguishable
// "no matches" sentinel.
//
// Used by repository layers (e.g. sqlite.ListEvents) to expand
// EventFilter.Categories into an event_type IN (...) SQL clause.
func EventTypesForCategory(c EventCategory) []string {
	var out []string
	for _, def := range EventDefinitions {
		if def.Category == c {
			out = append(out, def.Key)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
