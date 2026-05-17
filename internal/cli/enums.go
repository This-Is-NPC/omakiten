package cli

import (
	"strconv"

	"omakiten/internal/domain"
)

// parsePriority accepts either a numeric id ("3") or a label ("high")
// and resolves to the configured priority id via the supplied registry.
// Numeric is tried first (scripts/agents pass ids; humans pass labels).
// Both routes validate against the bundle-scoped EnumRegistry so unknown
// values error loudly instead of writing PriorityZero. registry must be
// non-nil — composition roots are expected to supply the registry that
// ConfigService.Import returned.
func parsePriority(input string, registry *domain.EnumRegistry) (domain.Priority, error) {
	if id, err := strconv.Atoi(input); err == nil {
		p := domain.Priority(id)
		if !registry.IsPriorityRegistered(p) {
			return domain.PriorityZero, domain.NewError(domain.ErrValidation,
				t("cli.err.priority_id_not_configured"),
				map[string]any{"priority": id})
		}
		return p, nil
	}
	if p, ok := registry.PriorityFromLabel(input); ok {
		return p, nil
	}
	return domain.PriorityZero, domain.NewError(domain.ErrValidation,
		t("cli.err.priority_unknown"),
		map[string]any{"priority": input})
}

// parseSeverity mirrors parsePriority for law severities. registry must
// be non-nil — composition roots supply the bundle-scoped registry.
func parseSeverity(input string, registry *domain.EnumRegistry) (domain.Severity, error) {
	if id, err := strconv.Atoi(input); err == nil {
		s := domain.Severity(id)
		if !registry.IsSeverityRegistered(s) {
			return domain.SeverityZero, domain.NewError(domain.ErrValidation,
				t("cli.err.severity_id_not_configured"),
				map[string]any{"severity": id})
		}
		return s, nil
	}
	if s, ok := registry.SeverityFromLabel(input); ok {
		return s, nil
	}
	return domain.SeverityZero, domain.NewError(domain.ErrValidation,
		t("cli.err.severity_unknown"),
		map[string]any{"severity": input})
}
