package domain

// EnumRegistry holds instance-scoped priority and severity lookup tables,
// eliminating the need for process-global state in concurrent or multi-tenant
// scenarios. It is populated from the loaded config bundle and injected into
// services that need to resolve labels or look up ids.
type EnumRegistry struct {
	priorities *priorityRegistry
	severities *severityRegistry
}

// NewEnumRegistry builds a registry from the wire shapes exported by the
// config layer. Both slices may be nil (empty registry); lookups return the
// zero value / false in that case.
func NewEnumRegistry(pairs []PriorityPair, sevs []SeverityPair) *EnumRegistry {
	return &EnumRegistry{
		priorities: buildPriorityRegistry(pairs),
		severities: buildSeverityRegistry(sevs),
	}
}

func buildPriorityRegistry(pairs []PriorityPair) *priorityRegistry {
	reg := &priorityRegistry{
		byID:    make(map[int]string, len(pairs)),
		byLabel: make(map[string]int, len(pairs)),
	}
	for _, p := range pairs {
		reg.byID[p.ID] = p.Value
		reg.byLabel[p.Value] = p.ID
		if p.Default {
			reg.defaultID = Priority(p.ID)
		}
	}
	if reg.defaultID == PriorityZero && len(pairs) > 0 {
		reg.defaultID = Priority(pairs[len(pairs)/2].ID)
	}
	return reg
}

func buildSeverityRegistry(pairs []SeverityPair) *severityRegistry {
	reg := &severityRegistry{
		byID:    make(map[int]string, len(pairs)),
		byLabel: make(map[string]int, len(pairs)),
	}
	for _, p := range pairs {
		reg.byID[p.ID] = p.Value
		reg.byLabel[p.Value] = p.ID
		if p.Default {
			reg.defaultID = Severity(p.ID)
		}
	}
	if reg.defaultID == SeverityZero && len(pairs) > 0 {
		reg.defaultID = Severity(pairs[len(pairs)/2].ID)
	}
	return reg
}

// PriorityLabel returns the configured label for the given priority id, or
// "" when the id is unknown or the registry is empty.
func (r *EnumRegistry) PriorityLabel(id Priority) string {
	if r == nil || r.priorities == nil {
		return ""
	}
	return r.priorities.byID[int(id)]
}

// SeverityLabel returns the configured label for the given severity id, or
// "" when the id is unknown or the registry is empty.
func (r *EnumRegistry) SeverityLabel(id Severity) string {
	if r == nil || r.severities == nil {
		return ""
	}
	return r.severities.byID[int(id)]
}

// PriorityFromLabel looks up a priority id by its configured label.
// Returns PriorityZero, false when the registry is empty or the label
// is not configured.
func (r *EnumRegistry) PriorityFromLabel(label string) (Priority, bool) {
	if r == nil || r.priorities == nil || label == "" {
		return PriorityZero, false
	}
	if id, ok := r.priorities.byLabel[label]; ok {
		return Priority(id), true
	}
	return PriorityZero, false
}

// SeverityFromLabel looks up a severity id by its configured label.
// Returns SeverityZero, false when the registry is empty or the label
// is not configured.
func (r *EnumRegistry) SeverityFromLabel(label string) (Severity, bool) {
	if r == nil || r.severities == nil || label == "" {
		return SeverityZero, false
	}
	if id, ok := r.severities.byLabel[label]; ok {
		return Severity(id), true
	}
	return SeverityZero, false
}

// DefaultPriority returns the priority id flagged default: true in the
// registry, falling back to the middle entry. PriorityZero when empty.
func (r *EnumRegistry) DefaultPriority() Priority {
	if r == nil || r.priorities == nil {
		return PriorityZero
	}
	return r.priorities.defaultID
}

// DefaultSeverity returns the severity id flagged default: true in the
// registry, falling back to the middle entry. SeverityZero when empty.
func (r *EnumRegistry) DefaultSeverity() Severity {
	if r == nil || r.severities == nil {
		return SeverityZero
	}
	return r.severities.defaultID
}

// IsPriorityRegistered reports whether the given id corresponds to an entry
// in the priority table.
func (r *EnumRegistry) IsPriorityRegistered(p Priority) bool {
	if r == nil || r.priorities == nil {
		return false
	}
	_, ok := r.priorities.byID[int(p)]
	return ok
}

// IsSeverityRegistered reports whether the given id corresponds to an entry
// in the severity table.
func (r *EnumRegistry) IsSeverityRegistered(s Severity) bool {
	if r == nil || r.severities == nil {
		return false
	}
	_, ok := r.severities.byID[int(s)]
	return ok
}

// PriorityPairs returns the original wire pairs used to build the registry.
// Useful for re-serialisation or merging.
func (r *EnumRegistry) PriorityPairs() []PriorityPair {
	if r == nil || r.priorities == nil {
		return nil
	}
	out := make([]PriorityPair, 0, len(r.priorities.byID))
	for id, value := range r.priorities.byID {
		out = append(out, PriorityPair{ID: id, Value: value, Default: Priority(id) == r.priorities.defaultID})
	}
	return out
}

// SeverityPairs returns the original wire pairs used to build the registry.
func (r *EnumRegistry) SeverityPairs() []SeverityPair {
	if r == nil || r.severities == nil {
		return nil
	}
	out := make([]SeverityPair, 0, len(r.severities.byID))
	for id, value := range r.severities.byID {
		out = append(out, SeverityPair{ID: id, Value: value, Default: Severity(id) == r.severities.defaultID})
	}
	return out
}
