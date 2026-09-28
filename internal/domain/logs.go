package domain

import (
	"strings"
)

// LogsFilterMode is the user-facing filter preset for the unified event log.
// The domain owns the category partition so every consumer uses the same
// event-type policy.
type LogsFilterMode int

const (
	LogsFilterAll LogsFilterMode = iota
	LogsFilterToolCalls
	LogsFilterDomain
	LogsFilterSystem
)

// LogsFilterModes is the canonical filter cycle order.
var LogsFilterModes = []LogsFilterMode{
	LogsFilterAll,
	LogsFilterToolCalls,
	LogsFilterDomain,
	LogsFilterSystem,
}

var logsFilterPartition = map[LogsFilterMode][]EventCategory{
	LogsFilterAll: nil,
	LogsFilterToolCalls: {
		EventCategoryToolCall,
		EventCategoryHook,
	},
	LogsFilterDomain: {
		EventCategoryTask,
		EventCategoryComment,
		EventCategoryPlan,
		EventCategoryTrick,
		EventCategoryTagDep,
	},
	LogsFilterSystem: {
		EventCategoryAudit,
		EventCategoryGuard,
		EventCategoryDomain,
		EventCategoryUpdate,
		EventCategoryTUI,
	},
}

// CycleLogsFilter rotates the Logs filter preset. Unknown modes recover to the
// all-events preset before applying the requested step.
func CycleLogsFilter(mode LogsFilterMode, step int) LogsFilterMode {
	index := -1
	for i, candidate := range LogsFilterModes {
		if candidate == mode {
			index = i
			break
		}
	}
	if index < 0 {
		return LogsFilterAll
	}
	length := len(LogsFilterModes)
	return LogsFilterModes[((index+step)%length+length)%length]
}

// LogsFilterCategories projects a filter preset onto repository categories.
// A nil result means no category filter.
func LogsFilterCategories(mode LogsFilterMode) []EventCategory {
	return logsFilterPartition[mode]
}

// EventStats is the prepared aggregate rendered by the Logs summary tables.
type EventStats struct {
	Categories      map[EventCategory]int
	ToolCallOK      int
	ToolCallError   int
	ToolCallRunning int
}

// ComputeEventStats folds loaded rows into the tool-call health summary and
// combines it with repository-provided category totals.
func ComputeEventStats(rows []EventRow, counts map[EventCategory]int) EventStats {
	stats := EventStats{Categories: counts}
	if stats.Categories == nil {
		stats.Categories = make(map[EventCategory]int, len(KnownEventCategories))
		for _, category := range KnownEventCategories {
			stats.Categories[category] = 0
		}
	}
	for _, row := range rows {
		if !isToolCallHealthEvent(row.EventType) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(row.Status)) {
		case "ok":
			stats.ToolCallOK++
		case "error":
			stats.ToolCallError++
		case "running":
			stats.ToolCallRunning++
		}
	}
	return stats
}

// FilterLogVisibleRows applies the event registry's visibility policy.
// Unknown event types remain visible for forward compatibility.
func FilterLogVisibleRows(rows []EventRow) []EventRow {
	if len(rows) == 0 {
		return rows
	}
	filtered := make([]EventRow, 0, len(rows))
	for _, row := range rows {
		if row.LogHidden {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func isToolCallHealthEvent(eventType string) bool {
	switch eventType {
	case EventTypeCLIToolCall, EventTypeMCPToolCall, EventTypeTUIToolCall, EventTypeHookExecuted:
		return true
	default:
		return false
	}
}
