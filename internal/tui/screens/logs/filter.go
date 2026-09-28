package logs

import (
	"omakiten/internal/domain"
)

const (
	FilterAll       = domain.LogsFilterAll
	FilterToolCalls = domain.LogsFilterToolCalls
	FilterDomain    = domain.LogsFilterDomain
	FilterSystem    = domain.LogsFilterSystem
)

var FilterModes = domain.LogsFilterModes

// FilterChipKey resolves the catalog key for the visible chip label.
func FilterChipKey(mode domain.LogsFilterMode) string {
	switch mode {
	case FilterToolCalls:
		return "tui.log.filter.tool_calls"
	case FilterDomain:
		return "tui.log.filter.domain"
	case FilterSystem:
		return "tui.log.filter.system"
	default:
		return "tui.log.filter.all"
	}
}
