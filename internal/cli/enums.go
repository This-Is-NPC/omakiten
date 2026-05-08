package cli

import (
	"strconv"

	"omakiten/internal/domain"
)

// parsePriority accepts either a numeric id ("3") or a label ("high")
// and resolves to the configured priority id. Numeric is tried first
// (scripts/agents pass ids; humans pass labels). Both routes validate
// through `IsRegistered` / `PriorityFromLabel` against the active
// registry so unknown values error loudly instead of writing
// PriorityZero. CLI/MCP entry points share this helper so the input
// surface is consistent across boundaries.
func parsePriority(input string) (domain.Priority, error) {
	if id, err := strconv.Atoi(input); err == nil {
		p := domain.Priority(id)
		if !p.IsRegistered() {
			return domain.PriorityZero, domain.NewError(domain.ErrValidation,
				"priority id is not in config.priorities",
				map[string]any{"priority": id})
		}
		return p, nil
	}
	if p, ok := domain.PriorityFromLabel(input); ok {
		return p, nil
	}
	return domain.PriorityZero, domain.NewError(domain.ErrValidation,
		"unknown priority; must match an id or value in config.priorities",
		map[string]any{"priority": input})
}

// parseSeverity mirrors parsePriority for law severities.
func parseSeverity(input string) (domain.Severity, error) {
	if id, err := strconv.Atoi(input); err == nil {
		s := domain.Severity(id)
		if !s.IsRegistered() {
			return domain.SeverityZero, domain.NewError(domain.ErrValidation,
				"severity id is not in config.severities",
				map[string]any{"severity": id})
		}
		return s, nil
	}
	if s, ok := domain.SeverityFromLabel(input); ok {
		return s, nil
	}
	return domain.SeverityZero, domain.NewError(domain.ErrValidation,
		"unknown severity; must match an id or value in config.severities",
		map[string]any{"severity": input})
}
