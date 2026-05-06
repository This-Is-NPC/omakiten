package domain

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

type LawSeverity string

const (
	LawSeverityInfo    LawSeverity = "info"
	LawSeverityWarning LawSeverity = "warning"
	LawSeverityError   LawSeverity = "error"
)

type LawScope string

const (
	LawScopeGlobal  LawScope = "global"
	LawScopeProject LawScope = "project"
	LawScopePersona LawScope = "persona"
)

type Law struct {
	ID          int64    `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name,omitempty"`
	Severity    string   `json:"severity"`
	Body        string   `json:"body"`
	Scope       LawScope `json:"scope,omitempty"`
	ProjectKey  string   `json:"project,omitempty"`
	PersonaKey  string   `json:"persona,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
	Warning     string   `json:"warning,omitempty"`
	IsCustom    bool     `json:"is_custom,omitempty"`
}

type LawInput struct {
	Key      string
	Severity LawSeverity
	Body     string
	Name     string
	Scope    LawScope
	Project  string
	Persona  string
}

type LawUpdate struct {
	Key      *string
	Severity *LawSeverity
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
