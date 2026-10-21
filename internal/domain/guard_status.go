package domain

// GuardStatus is the evaluated state of one workflow guard for one task.
// Details carries the guard type's counts or offending rows. Fix is the
// command that satisfies an unmet guard, empty when no single command does.
type GuardStatus struct {
	Type      string         `json:"type"`
	Satisfied bool           `json:"satisfied"`
	Details   map[string]any `json:"details,omitempty"`
	Hint      string         `json:"hint,omitempty"`
	Fix       string         `json:"fix,omitempty"`
	Message   string         `json:"-"`
}
