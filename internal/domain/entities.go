package domain

// Severity is the id of a configured law-severity entry. The human
// label (e.g. "info", "warning", "error") and the optional color live
// in `config.severities` and are resolved at the rendering boundary
// via an injected `EnumRegistry`. Same id↔value pattern as Priority:
// code references the int handle and the domain layer stays free of
// process-global state; JSON marshaling emits the raw int id.
type Severity int

// SeverityZero is the sentinel "no severity" id. Production code
// always resolves a real id from the active severities table before
// persisting; this exists so zero-value Law structs are valid.
const SeverityZero Severity = 0

// severityRegistry holds the active id↔value mapping plus the configured
// default. Used internally by EnumRegistry; not exported.
type severityRegistry struct {
	byID      map[int]string
	byLabel   map[string]int
	defaultID Severity
}

// SeverityPair is the wire shape EnumRegistry consumes. Mirrors just
// enough of config.SeverityDefinition to keep the domain layer free of
// an internal/config import. Default flags the entry that writers
// substitute when a law arrives without an explicit severity; the
// config-layer validator rejects more than one entry with the flag set.
type SeverityPair struct {
	ID      int
	Value   string
	Default bool
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
