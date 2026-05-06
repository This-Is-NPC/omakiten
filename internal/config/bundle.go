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
	Views            ViewSettings     `yaml:"views,omitempty" json:"views,omitempty"`
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

// SortSettings is the (field, order) pair that drives ORDER BY for a view.
// Field is interpreted per view (the validator enforces the allowed set);
// Order is "asc" or "desc". Empty fields are filled in by EffectiveViews
// from per-view defaults, so omitting the section in YAML keeps existing
// configs working unchanged.
type SortSettings struct {
	Field string `yaml:"field,omitempty" json:"field,omitempty"`
	Order string `yaml:"order,omitempty" json:"order,omitempty"`
}

type BoardFilterSettings struct {
	Priority []string `yaml:"priority,omitempty" json:"priority,omitempty"`
}

type TableFilterSettings struct {
	Priority []string `yaml:"priority,omitempty" json:"priority,omitempty"`
	Bucket   []string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
}

type LogsFilterSettings struct {
	Source []string `yaml:"source,omitempty" json:"source,omitempty"`
}

type BoardViewSettings struct {
	Sort   SortSettings        `yaml:"sort,omitempty" json:"sort,omitempty"`
	Filter BoardFilterSettings `yaml:"filter,omitempty" json:"filter,omitempty"`
}

type TableViewSettings struct {
	Sort   SortSettings        `yaml:"sort,omitempty" json:"sort,omitempty"`
	Filter TableFilterSettings `yaml:"filter,omitempty" json:"filter,omitempty"`
}

type GraphViewSettings struct {
	Sort SortSettings `yaml:"sort,omitempty" json:"sort,omitempty"`
}

type LogsViewSettings struct {
	Sort   SortSettings       `yaml:"sort,omitempty" json:"sort,omitempty"`
	Limit  int                `yaml:"limit,omitempty" json:"limit,omitempty"`
	Filter LogsFilterSettings `yaml:"filter,omitempty" json:"filter,omitempty"`
}

type TaskActivityViewSettings struct {
	Sort SortSettings `yaml:"sort,omitempty" json:"sort,omitempty"`
}

// ViewSettings is the per-view default sort/filter block. The TUI seeds
// itself from these on startup so the user does not have to re-apply
// preferences every session.
type ViewSettings struct {
	Board        BoardViewSettings        `yaml:"board,omitempty" json:"board,omitempty"`
	Table        TableViewSettings        `yaml:"table,omitempty" json:"table,omitempty"`
	Graph        GraphViewSettings        `yaml:"graph,omitempty" json:"graph,omitempty"`
	Logs         LogsViewSettings         `yaml:"logs,omitempty" json:"logs,omitempty"`
	TaskActivity TaskActivityViewSettings `yaml:"task_activity,omitempty" json:"task_activity,omitempty"`
}

// Default sort/filter values used when the user omits config.views or any
// of its sub-fields. Centralising them here keeps the validator, the TUI
// and the store query in agreement — all three call EffectiveViews() to
// read the merged result rather than re-deriving the defaults locally.
const (
	DefaultBoardSortField        = "created_at"
	DefaultBoardSortOrder        = "desc"
	DefaultTableSortField        = "created_at"
	DefaultTableSortOrder        = "desc"
	DefaultGraphSortField        = "id"
	DefaultGraphSortOrder        = "asc"
	DefaultLogsSortOrder         = "desc"
	DefaultLogsLimit             = 50
	DefaultTaskActivitySortOrder = "asc"
)

// EffectiveViews returns ViewSettings with omitted fields filled in from
// the canonical defaults. Callers should always go through this so the
// store query, the validator and the TUI agree on the resolved values.
func (s Settings) EffectiveViews() ViewSettings {
	v := s.Views
	if v.Board.Sort.Field == "" {
		v.Board.Sort.Field = DefaultBoardSortField
	}
	if v.Board.Sort.Order == "" {
		v.Board.Sort.Order = DefaultBoardSortOrder
	}
	if v.Table.Sort.Field == "" {
		v.Table.Sort.Field = DefaultTableSortField
	}
	if v.Table.Sort.Order == "" {
		v.Table.Sort.Order = DefaultTableSortOrder
	}
	if v.Graph.Sort.Field == "" {
		v.Graph.Sort.Field = DefaultGraphSortField
	}
	if v.Graph.Sort.Order == "" {
		v.Graph.Sort.Order = DefaultGraphSortOrder
	}
	if v.Logs.Sort.Order == "" {
		v.Logs.Sort.Order = DefaultLogsSortOrder
	}
	if v.Logs.Limit <= 0 {
		v.Logs.Limit = DefaultLogsLimit
	}
	if v.TaskActivity.Sort.Order == "" {
		v.TaskActivity.Sort.Order = DefaultTaskActivitySortOrder
	}
	return v
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
