package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
)

// Severity is the id of a configured law-severity entry. The human
// label (e.g. "info", "warning", "error") and the optional color live
// in `config.severities` and are resolved at the rendering boundary
// via the process-global severity registry below. Same id↔value
// pattern as Priority: code references the int handle, renderers (TUI
// badge, CLI output, JSON marshaling) emit the configured label.
type Severity int

// SeverityZero is the sentinel "no severity" id. Production code
// always resolves a real id from the active severities table before
// persisting; this exists so zero-value Law structs are valid.
const SeverityZero Severity = 0

type severityRegistry struct {
	byID      map[int]string
	byLabel   map[string]int
	defaultID Severity
}

var activeSeverities atomic.Pointer[severityRegistry]

// SeverityPair is the wire shape RegisterSeverities accepts. Mirrors
// just enough of config.SeverityDefinition to keep the domain layer
// free of an internal/config import. Default flags the entry that
// writers substitute when a law arrives without an explicit severity;
// validator rejects more than one entry with the flag set.
type SeverityPair struct {
	ID      int
	Value   string
	Default bool
}

// RegisterSeverities replaces the active severity registry with the
// supplied id↔value pairs. Same semantics as RegisterPriorities: order
// is sort weight (low → high by id), one entry may flag itself default,
// and an empty slice clears the registry (used by tests resetting
// between scenarios).
func RegisterSeverities(pairs []SeverityPair) {
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
	activeSeverities.Store(reg)
}

// DefaultSeverity returns the id flagged `default: true` in the active
// registry, falling back to the middle entry when no flag is set.
// SeverityZero when uninitialised — callers should treat that as "let
// the storage layer pick" so partially-bootstrapped tests still work.
//
// Deprecated: Use EnumRegistry.DefaultSeverity instead.
func DefaultSeverity() Severity {
	if reg := activeSeverities.Load(); reg != nil {
		return reg.defaultID
	}
	return SeverityZero
}

// Label returns the configured label for this severity id, or "" when
// the registry is empty or the id is unknown.
//
// Deprecated: Use EnumRegistry.SeverityLabel instead.
func (s Severity) Label() string {
	if reg := activeSeverities.Load(); reg != nil {
		return reg.byID[int(s)]
	}
	return ""
}

// String returns Label when known, otherwise the numeric id as a
// fallback so log lines and error messages never read empty.
func (s Severity) String() string {
	if label := s.Label(); label != "" {
		return label
	}
	return strconv.Itoa(int(s))
}

// MarshalJSON renders the severity as its label string when registered,
// preserving the historical wire format ("info" / "warning" / "error").
// When no registry is wired (tests, partially-initialised runtimes)
// falls back to the numeric id so consumers still get unambiguous data.
func (s Severity) MarshalJSON() ([]byte, error) {
	if label := s.Label(); label != "" {
		return json.Marshal(label)
	}
	return json.Marshal(int(s))
}

// UnmarshalJSON accepts either an integer id or a string label. Strings
// are resolved against the active registry; unknown labels error so
// typos surface immediately. Uninitialised registry (test fixture
// missed the RegisterSeverities call) is reported distinctly from an
// unknown label so the failure points at the wiring problem.
func (s *Severity) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*s = SeverityZero
		return nil
	}
	if data[0] == '"' {
		var label string
		if err := json.Unmarshal(data, &label); err != nil {
			return err
		}
		if label == "" {
			*s = SeverityZero
			return nil
		}
		reg := activeSeverities.Load()
		if reg == nil || len(reg.byLabel) == 0 {
			return fmt.Errorf("severity registry not initialised; call RegisterSeverities first (received label %q)", label)
		}
		if id, ok := reg.byLabel[label]; ok {
			*s = Severity(id)
			return nil
		}
		return fmt.Errorf("unknown severity label %q (must match a value in config.severities)", label)
	}
	var id int
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}
	*s = Severity(id)
	return nil
}

// SeverityFromLabel looks up a severity id by its configured label.
// Returns SeverityZero, false when the registry is empty or the label
// is not configured. Used by CLI/MCP boundary layers and the
// configstore frontmatter loader to translate user-supplied strings
// into ids before crossing the domain boundary.
//
// Deprecated: Use EnumRegistry.SeverityFromLabel instead.
func SeverityFromLabel(label string) (Severity, bool) {
	if label == "" {
		return SeverityZero, false
	}
	if reg := activeSeverities.Load(); reg != nil {
		if id, ok := reg.byLabel[label]; ok {
			return Severity(id), true
		}
	}
	return SeverityZero, false
}

// IsRegistered reports whether the given id corresponds to an entry
// in the active severity table.
//
// Deprecated: Use EnumRegistry.IsSeverityRegistered instead.
func (s Severity) IsRegistered() bool {
	if reg := activeSeverities.Load(); reg != nil {
		_, ok := reg.byID[int(s)]
		return ok
	}
	return false
}

type Skill struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
	Warning     string `json:"warning,omitempty"`
	IsCustom    bool   `json:"is_custom,omitempty"`
}

type Persona struct {
	ID          int64    `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	SkillIDs    []int64  `json:"skill_ids,omitempty"`
	SkillKeys   []string `json:"skill_keys,omitempty"`
	LawKeys     []string `json:"law_keys,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
	Warning     string   `json:"warning,omitempty"`
	IsCustom    bool     `json:"is_custom,omitempty"`
}

type LawScope string

const (
	LawScopeGlobal  LawScope = "global"
	LawScopeProject LawScope = "project"
	LawScopePersona LawScope = "persona"
)

type Law struct {
	ID         int64    `json:"id"`
	Key        string   `json:"key"`
	Name       string   `json:"name,omitempty"`
	Severity   Severity `json:"severity"`
	Body       string   `json:"body"`
	Scope      LawScope `json:"scope,omitempty"`
	ProjectKey string   `json:"project,omitempty"`
	PersonaKey string   `json:"persona,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
	Warning    string   `json:"warning,omitempty"`
	IsCustom   bool     `json:"is_custom,omitempty"`
}

type LawInput struct {
	Key      string
	Severity Severity
	Body     string
	Name     string
	Scope    LawScope
	Project  string
	Persona  string
}

type LawUpdate struct {
	Key      *string
	Severity *Severity
	Body     *string
	Name     *string
}

type SkillInput struct {
	Key         string
	Name        string
	Description string
	Body        string
}

type SkillUpdate struct {
	Key         *string
	Name        *string
	Description *string
	Body        *string
}

type PersonaInput struct {
	Key         string
	Name        string
	Description string
	Body        string
	SkillIDs    []int64
	SkillKeys   []string
}

type PersonaUpdate struct {
	Key         *string
	Name        *string
	Description *string
	Body        *string
	SkillIDs    *[]int64
	SkillKeys   *[]string
}
