package config

// Bundle is the in-memory representation of an omakiten config directory.
// `omakiten.yaml` provides settings, workflows, and reference wiring; per-entity
// `.md` files (skills/<slug>.md, laws/<slug>.md, personas/<slug>.md) provide the
// authoring content. The loader merges them into Bundle; the saver splits them
// back out.
type Bundle struct {
	Version   int             `yaml:"version" json:"version"`
	Kit       Kit             `yaml:"kit" json:"kit"`
	Config    Settings        `yaml:"config" json:"config"`
	Skills    []Skill         `yaml:"-" json:"skills,omitempty"`
	Personas  []Persona       `yaml:"-" json:"personas,omitempty"`
	Laws      []Law           `yaml:"-" json:"laws,omitempty"`
	Workflows []Workflow      `yaml:"workflows" json:"workflows,omitempty"`
	Projects  []Project       `yaml:"-" json:"projects,omitempty"`
	Warnings  []SourceWarning `yaml:"-" json:"warnings,omitempty"`
}

// wiring is the literal YAML shape of `omakiten.yaml`. Loader unmarshals into
// this; saver marshals from it. Keeps Bundle decoupled from on-disk layout.
type wiring struct {
	Version   int                 `yaml:"version"`
	Kit       Kit                 `yaml:"kit"`
	Config    Settings            `yaml:"config"`
	Workflows []Workflow          `yaml:"workflows"`
	Skills    []string            `yaml:"skills,omitempty"`
	Laws      []string            `yaml:"laws,omitempty"`
	Personas  []PersonaWiring     `yaml:"personas,omitempty"`
	Projects  []ProjectWiring     `yaml:"projects,omitempty"`
}

// PersonaWiring is the persona entry inside `omakiten.yaml`. The persona body
// (description, free-form notes) lives in personas/<slug>.md; this struct only
// holds the relationships managed by the system.
type PersonaWiring struct {
	Slug   string   `yaml:"slug"`
	Skills []string `yaml:"skills,omitempty"`
	Laws   []string `yaml:"laws,omitempty"`
}

// ProjectWiring is the project entry inside `omakiten.yaml` (Phase 2 surface).
type ProjectWiring struct {
	Slug        string   `yaml:"slug"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Laws        []string `yaml:"laws,omitempty"`
}

type Kit struct {
	ID   int    `yaml:"id" json:"id"`
	Key  string `yaml:"key" json:"key"`
	Name string `yaml:"name" json:"name"`
}

type Settings struct {
	Output   OutputSettings   `yaml:"output" json:"output"`
	Context  ContextSettings  `yaml:"context" json:"context"`
	Workflow WorkflowSettings `yaml:"workflow" json:"workflow"`
	Theme    ThemeSettings    `yaml:"theme" json:"theme"`
}

type OutputSettings struct {
	JSONMinified bool `yaml:"json_minified" json:"json_minified"`
	OmitEmpty    bool `yaml:"omit_empty" json:"omit_empty"`
}

type ContextSettings struct {
	DefaultLevel int `yaml:"default_level" json:"default_level"`
	MaxTokens    int `yaml:"max_tokens" json:"max_tokens"`
}

type WorkflowSettings struct {
	Active string `yaml:"active" json:"active"`
}

type ThemeSettings struct {
	Active string `yaml:"active" json:"active"`
}

// Skill is a resolved skill: its frontmatter + body merged with the slug taken
// from the source filename (without `.md`).
type Skill struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
}

type Persona struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Laws        []string `json:"laws,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
}

type Law struct {
	Slug        string `json:"slug"`
	Name        string `json:"name,omitempty"`
	Severity    string `json:"severity"`
	Body        string `json:"body"`
	Scope       string `json:"scope,omitempty"`
	ProjectSlug string `json:"project,omitempty"`
	PersonaSlug string `json:"persona,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
}

type Project struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Laws        []string `json:"laws,omitempty"`
}

type Workflow struct {
	ID          int          `yaml:"id" json:"id"`
	Key         string       `yaml:"key" json:"key"`
	Name        string       `yaml:"name" json:"name"`
	Buckets     []Bucket     `yaml:"buckets" json:"buckets"`
	Transitions []Transition `yaml:"transitions" json:"transitions,omitempty"`
}

type Bucket struct {
	ID       int    `yaml:"id" json:"id"`
	Key      string `yaml:"key" json:"key"`
	Name     string `yaml:"name" json:"name"`
	Position int    `yaml:"position" json:"position"`
}

type Transition struct {
	From int `yaml:"from" json:"from"`
	To   int `yaml:"to" json:"to"`
}

type Theme struct {
	Version int               `yaml:"version" json:"version"`
	Key     string            `yaml:"key" json:"key"`
	Name    string            `yaml:"name" json:"name"`
	Colors  map[string]string `yaml:"colors" json:"colors"`
}

// SourceWarning surfaces non-fatal issues (e.g. filename ↔ frontmatter name
// drift). Validator collects them; CLI/TUI render them as `warning` fields.
type SourceWarning struct {
	Slug    string `json:"slug,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// EntityKind identifies the three folder-backed entity types.
type EntityKind string

const (
	EntityKindSkill   EntityKind = "skill"
	EntityKindLaw     EntityKind = "law"
	EntityKindPersona EntityKind = "persona"
)

// EntityFolder returns the per-kind folder name relative to the config dir.
func (k EntityKind) Folder() string {
	switch k {
	case EntityKindSkill:
		return "skills"
	case EntityKindLaw:
		return "laws"
	case EntityKindPersona:
		return "personas"
	}
	return ""
}
