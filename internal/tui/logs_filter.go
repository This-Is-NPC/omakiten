package tui

import "omakiten/internal/domain"

// LogsFilterMode is the user-facing filter preset cycled via the
// Stats › Logs `F` key (umbrella #320 AC #2). The set is closed
// because the chip strip enumerates every variant; adding a value
// here means adding an entry to logsFilterPartition (the
// mode→categories source of truth) plus a chip render arm. Order
// is the cycle order itself:
//
//	all → tool-calls → domain → system → all
//
// LogsFilterAll is the zero value so a freshly constructed Model
// (and every test fixture that does not opt in) starts on the
// no-op filter and surfaces every event_type the project recorded.
type LogsFilterMode int

const (
	// LogsFilterAll is the no-op filter: logsFilterCategories returns
	// nil so domain.EventFilter treats it as "no category filter" and
	// the renderer surfaces every event_type inside the snapshot's
	// logs.window_days horizon.
	LogsFilterAll LogsFilterMode = iota
	// LogsFilterToolCalls scopes the panel to cli/mcp/tui tool-call
	// rows + hook dispatches. Matches the same set computeEventStats's
	// "Health · tool_calls" subset folds — the chip and the health
	// table tell the same story.
	LogsFilterToolCalls
	// LogsFilterDomain scopes the panel to user-domain activity:
	// tasks, comments, plans, trick palette submissions, and the
	// shared tag/dependency edges. Excludes audit + guard + tool
	// telemetry so the user can focus on what they (or other agents)
	// authored.
	LogsFilterDomain
	// LogsFilterSystem scopes the panel to system bookkeeping:
	// guards, audit emissions, and the catch-all `domain` category
	// (bundle.swapped, subtask_kit.notice_emitted, …). Tool calls and
	// user-domain rows are excluded — those have their own chips.
	LogsFilterSystem
)

// logsFilterModes is the canonical cycle order. `logsFilterCycle`
// indexes into it; tests pin the exact rotation `all → tool-calls
// → domain → system → all` against this slice so the test fails
// when the order drifts.
var logsFilterModes = []LogsFilterMode{
	LogsFilterAll,
	LogsFilterToolCalls,
	LogsFilterDomain,
	LogsFilterSystem,
}

// logsFilterCycle returns the next mode in the cycle. `step` is
// +1 for forward (`f`) and -1 for backward (`shift+F`). Out-of-band
// modes (e.g. an unknown int landing on Model from a future test
// fixture) collapse to LogsFilterAll before stepping so the cycle
// never indexes out of bounds.
func logsFilterCycle(mode LogsFilterMode, step int) LogsFilterMode {
	idx := -1
	for i, m := range logsFilterModes {
		if m == mode {
			idx = i
			break
		}
	}
	if idx < 0 {
		return LogsFilterAll
	}
	n := len(logsFilterModes)
	next := ((idx+step)%n + n) % n
	return logsFilterModes[next]
}

// logsFilterPartition is the single source of truth for the
// mode → repository-filter category projection. Each LogsFilterMode
// constant maps to the canonical category subset the panel surfaces
// when that chip is active; LogsFilterAll maps to nil so
// EventFilter treats it as "no category filter" downstream.
//
// When a new domain.EventCategory lands (or a new chip is added),
// this map is the only edit site: logsFilterCategories delegates,
// the union test pins it against domain.KnownEventCategories, and
// TestLogsFilterPartitionMapMatchesEnumeration catches enum drift.
var logsFilterPartition = map[LogsFilterMode][]domain.EventCategory{
	LogsFilterAll: nil, // nil = no filter, show every category
	LogsFilterToolCalls: {
		domain.EventCategoryToolCall,
		domain.EventCategoryHook,
	},
	LogsFilterDomain: {
		domain.EventCategoryTask,
		domain.EventCategoryComment,
		domain.EventCategoryPlan,
		domain.EventCategoryTrick,
		domain.EventCategoryTagDep,
		domain.EventCategoryNote,
	},
	LogsFilterSystem: {
		domain.EventCategoryAudit,
		domain.EventCategoryGuard,
		domain.EventCategoryDomain,
	},
}

// logsFilterCategories projects a mode onto the EventFilter.Categories
// slice the repository consumes by looking the mode up in
// logsFilterPartition. LogsFilterAll → nil (no filter); other modes
// → the canonical category subset for that preset. Unknown modes
// fall through to nil so a stale fixture cannot panic the renderer.
func logsFilterCategories(mode LogsFilterMode) []domain.EventCategory {
	return logsFilterPartition[mode]
}

// logsFilterChipKey returns the i18n catalog key for the chip label
// of the given mode. Centralised so the chip renderer and the help
// overlay share the same vocabulary — when a new mode lands, both
// surfaces pick it up by extending this switch and the en.yaml pack.
func logsFilterChipKey(mode LogsFilterMode) string {
	switch mode {
	case LogsFilterToolCalls:
		return "tui.log.filter.tool_calls"
	case LogsFilterDomain:
		return "tui.log.filter.domain"
	case LogsFilterSystem:
		return "tui.log.filter.system"
	}
	return "tui.log.filter.all"
}
