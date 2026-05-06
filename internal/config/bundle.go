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
	Templates []TaskTemplate  `yaml:"-" json:"templates,omitempty"`
	Workflows []Workflow      `yaml:"workflows" json:"workflows,omitempty"`
	Projects  []Project       `yaml:"-" json:"projects,omitempty"`
	Warnings  []SourceWarning `yaml:"-" json:"warnings,omitempty"`
}

// TemplateByDefault resolves the template that should be used as the active
// scaffold for `kind` in the context of `projectSlug`. Project-scoped wins:
// if a template declares default=kind, project=projectSlug it is returned
// first; otherwise the global default (default=kind, project="") is
// returned. Returns nil when neither matches — the caller treats that as
// "no template configured for this kind".
func (b Bundle) TemplateByDefault(kind, projectSlug string) *TaskTemplate {
	if kind == "" {
		return nil
	}
	var global *TaskTemplate
	for i := range b.Templates {
		t := &b.Templates[i]
		if t.Default != kind {
			continue
		}
		if projectSlug != "" && t.ProjectSlug == projectSlug {
			return t
		}
		if t.ProjectSlug == "" && global == nil {
			global = t
		}
	}
	return global
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
	Templates []string            `yaml:"templates,omitempty"`
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
	Output           OutputSettings   `yaml:"output" json:"output"`
	Context          ContextSettings  `yaml:"context" json:"context"`
	Workflow         WorkflowSettings `yaml:"workflow" json:"workflow"`
	Theme            ThemeSettings    `yaml:"theme" json:"theme"`
	TemplateDefaults []string         `yaml:"template_defaults,omitempty" json:"template_defaults,omitempty"`
}

// DefaultTemplateKinds is the canonical set of template-default slots when
// the user omits config.template_defaults. These are the kinds the TUI
// picker offers and the agent consumers query for.
var DefaultTemplateKinds = []string{"task", "pr", "comment-resume", "comment-selfbranch"}

// TemplateKinds returns the configured template_defaults list, falling back
// to DefaultTemplateKinds when omitted. Used by validator, TUI picker, and
// MCP query endpoints — all of them must agree on the valid kinds.
func (s Settings) TemplateKinds() []string {
	if len(s.TemplateDefaults) == 0 {
		out := make([]string, len(DefaultTemplateKinds))
		copy(out, DefaultTemplateKinds)
		return out
	}
	out := make([]string, len(s.TemplateDefaults))
	copy(out, s.TemplateDefaults)
	return out
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
	IsCustom    bool   `json:"is_custom,omitempty"`
}

type Persona struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Laws        []string `json:"laws,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
	IsCustom    bool     `json:"is_custom,omitempty"`
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
	IsCustom    bool   `json:"is_custom,omitempty"`
}

type Project struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Laws        []string `json:"laws,omitempty"`
}

// TaskTemplate is a resolved template: frontmatter fields merged with the body
// from a templates/<slug>.md file. The body is free-form markdown and is not
// validated structurally — agents use it as a scaffold, not as schema.
//
// Default is the kind this template is the active scaffold for (e.g. "task",
// "pr"). Empty means the template is loaded but inactive. ProjectSlug scopes
// the default to a single project — empty means it is the global default for
// the kind. Uniqueness is enforced by the validator: at most one template
// per (default, project) pair.
type TaskTemplate struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Entity      string `json:"entity,omitempty"`
	Default     string `json:"default,omitempty"`
	ProjectSlug string `json:"project,omitempty"`
	Body        string `json:"body,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
	IsCustom    bool   `json:"is_custom,omitempty"`
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

type TransitionGuard struct {
	Type    string   `yaml:"type" json:"type"`
	Buckets []string `yaml:"buckets,omitempty" json:"buckets,omitempty"`
	Count   int      `yaml:"count,omitempty" json:"count,omitempty"`
	Tag     string   `yaml:"tag,omitempty" json:"tag,omitempty"`
	Hint    string   `yaml:"hint,omitempty" json:"hint,omitempty"`
}

type Transition struct {
	From   int               `yaml:"from" json:"from"`
	To     int               `yaml:"to" json:"to"`
	Guards []TransitionGuard `yaml:"guards,omitempty" json:"guards,omitempty"`
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
	EntityKindSkill    EntityKind = "skill"
	EntityKindLaw      EntityKind = "law"
	EntityKindPersona  EntityKind = "persona"
	EntityKindTemplate EntityKind = "template"
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
	case EntityKindTemplate:
		return "templates"
	}
	return ""
}
