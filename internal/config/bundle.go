package config

// Bundle is the in-memory representation of an omakiten config directory.
// `omakiten.yaml` provides settings, workflows, and reference wiring; per-entity
// `.md` files (skills/<slug>.md, laws/<slug>.md, personas/<slug>.md) provide the
// authoring content. The loader merges them into Bundle; the saver splits them
// back out.
type Bundle struct {
	Version     int                       `yaml:"version" json:"version"`
	Kit         Kit                       `yaml:"kit" json:"kit"`
	Config      Settings                  `yaml:"config" json:"config"`
	Skills      []Skill                   `yaml:"-" json:"skills,omitempty"`
	Personas    []Persona                 `yaml:"-" json:"personas,omitempty"`
	Laws        []Law                     `yaml:"-" json:"laws,omitempty"`
	Templates   []TaskTemplate            `yaml:"-" json:"templates,omitempty"`
	Workflows   []Workflow                `yaml:"workflows" json:"workflows,omitempty"`
	Projects    []Project                 `yaml:"-" json:"projects,omitempty"`
	MCPCommands map[string]MCPCommandSpec `yaml:"-" json:"mcp_commands,omitempty"`
	Warnings    []SourceWarning           `yaml:"-" json:"warnings,omitempty"`
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
	Version     int                       `yaml:"version"`
	Kit         Kit                       `yaml:"kit"`
	Config      Settings                  `yaml:"config"`
	Workflows   []Workflow                `yaml:"workflows"`
	Skills      []string                  `yaml:"skills,omitempty"`
	Laws        []string                  `yaml:"laws,omitempty"`
	Templates   []string                  `yaml:"templates,omitempty"`
	Personas    []PersonaWiring           `yaml:"personas,omitempty"`
	Projects    []ProjectWiring           `yaml:"projects,omitempty"`
	MCPCommands map[string]MCPCommandSpec `yaml:"mcp_commands,omitempty"`
}

// MCPCommandSpec binds an `okt-*` MCP prompt to a persona, a set of laws, a
// set of templates, and an opt-out list. The reserved key `global` declares
// the laws applied to every command resolution; per-command entries can either
// add laws (`laws:`) or remove laws inherited from `global` (`laws_disabled:`).
//
// Templates listed here are surfaced in the resolved prompt so the agent has
// the relevant scaffold body without having to call templates.show first.
type MCPCommandSpec struct {
	Persona      string   `yaml:"persona,omitempty" json:"persona,omitempty"`
	Laws         []string `yaml:"laws,omitempty" json:"laws,omitempty"`
	LawsDisabled []string `yaml:"laws_disabled,omitempty" json:"laws_disabled,omitempty"`
	Templates    []string `yaml:"templates,omitempty" json:"templates,omitempty"`
}

// MCPCommandsGlobalKey is the reserved entry inside mcp_commands that supplies
// laws inherited by every command. Per-command entries may opt out via
// `laws_disabled:`. Anything else under this key is ignored — the shape is
// the same MCPCommandSpec for symmetry, but Persona/Templates are not applied.
const MCPCommandsGlobalKey = "global"

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
	MCP              MCPSettings      `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	TUI              TUISettings      `yaml:"tui,omitempty" json:"tui,omitempty"`
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

// MCPSettings tunes how MCP responses are shaped to fit the agent's context
// window. Every field is optional; omit a key to keep the canonical default.
// Pointer booleans (`*bool`) distinguish "not declared" from "declared false"
// — e.g. omitting `cache_prompts` keeps caching on, while writing
// `cache_prompts: false` is an explicit opt-out.
//
// Defaults are surfaced via the `Effective*` accessors so the validator and
// the runtime agree on resolved values without each caller re-deriving them.
type MCPSettings struct {
	// RecentCommentLimit caps how many recent comments tools like
	// `tasks.continue` and `project.overview` ship per call. <=0 keeps the
	// canonical default (DefaultRecentCommentLimit).
	RecentCommentLimit int `yaml:"recent_comment_limit,omitempty" json:"recent_comment_limit,omitempty"`

	// MaxCommentChars truncates comment bodies past this length with a
	// trailing ellipsis when shipped over MCP. <=0 keeps full bodies; >0
	// truncates. Use to bound `tasks.continue` payloads on tasks with long
	// `#resume` / `#documentation` comments.
	MaxCommentChars int `yaml:"max_comment_chars,omitempty" json:"max_comment_chars,omitempty"`

	// IncludeWorkflowInContinue toggles the `workflow` block in
	// `tasks.continue` responses. nil keeps the canonical default
	// (DefaultIncludeWorkflowInContinue, true). Set false once `okt` has
	// loaded the workflow for the session and the agent does not need it
	// re-shipped per call.
	IncludeWorkflowInContinue *bool `yaml:"include_workflow_in_continue,omitempty" json:"include_workflow_in_continue,omitempty"`

	// CachePrompts toggles emitting the Anthropic-aware `cache_control` hint
	// on `prompts/get` content. nil keeps the canonical default
	// (DefaultCachePrompts, true). Aware clients (recent Claude Code) reuse
	// the cached prompt across calls; unaware clients ignore the hint
	// silently — disabling only matters when a client misbehaves on it.
	CachePrompts *bool `yaml:"cache_prompts,omitempty" json:"cache_prompts,omitempty"`
}

// Canonical defaults for MCPSettings. Centralized so validator and runtime
// resolve to the same values without re-deriving them locally.
const (
	DefaultRecentCommentLimit        = 5
	DefaultMaxCommentChars           = 0 // 0 = no truncation
	DefaultIncludeWorkflowInContinue = true
	DefaultCachePrompts              = true
)

// EffectiveRecentCommentLimit returns the configured cap on recent comments
// shipped per MCP call, falling back to DefaultRecentCommentLimit when the
// user omitted the field or set a non-positive value.
func (m MCPSettings) EffectiveRecentCommentLimit() int {
	if m.RecentCommentLimit <= 0 {
		return DefaultRecentCommentLimit
	}
	return m.RecentCommentLimit
}

// EffectiveMaxCommentChars returns the configured comment-body truncation
// length, falling back to DefaultMaxCommentChars (0 = no truncation) when the
// user omitted the field or set a negative value.
func (m MCPSettings) EffectiveMaxCommentChars() int {
	if m.MaxCommentChars < 0 {
		return DefaultMaxCommentChars
	}
	return m.MaxCommentChars
}

// EffectiveIncludeWorkflowInContinue returns whether `tasks.continue` should
// embed the active workflow shape. nil → DefaultIncludeWorkflowInContinue.
func (m MCPSettings) EffectiveIncludeWorkflowInContinue() bool {
	if m.IncludeWorkflowInContinue == nil {
		return DefaultIncludeWorkflowInContinue
	}
	return *m.IncludeWorkflowInContinue
}

// EffectiveCachePrompts returns whether `prompts/get` should emit the
// `cache_control` hint. nil → DefaultCachePrompts.
func (m MCPSettings) EffectiveCachePrompts() bool {
	if m.CachePrompts == nil {
		return DefaultCachePrompts
	}
	return *m.CachePrompts
}

type ContextSettings struct {
	DefaultLevel int `yaml:"default_level" json:"default_level"`
	MaxTokens    int `yaml:"max_tokens" json:"max_tokens"`
}

// TUISettings tunes how the terminal UI presents data. Every field is
// optional; omit a key to keep the canonical default. Currently scoped to
// the entity-card token-health badge thresholds, but ready to grow as more
// TUI knobs need to escape hardcoded constants.
type TUISettings struct {
	TokenBadge TokenBadgeThresholds `yaml:"token_badge,omitempty" json:"token_badge,omitempty"`
}

// TokenBadgeThresholds drives the colored TOKENS:N badge on entity cards.
// Above RedAt → red; above YellowAt → yellow; else green. Values are token
// counts (the renderer uses the same approximation as the right-rail token
// budget, so tuning here matches what users see in the rail). <=0 keeps the
// canonical default.
type TokenBadgeThresholds struct {
	YellowAt int `yaml:"yellow_at,omitempty" json:"yellow_at,omitempty"`
	RedAt    int `yaml:"red_at,omitempty" json:"red_at,omitempty"`
}

// Canonical defaults for TokenBadgeThresholds. Calibrated against the default
// kit: most laws land in the 70–190 token range with their few-shot examples,
// so the green band must extend above that to keep the panel signal-rich
// rather than uniformly yellow.
const (
	DefaultTokenBadgeYellowAt = 150
	DefaultTokenBadgeRedAt    = 400
)

// Effective returns the resolved (yellow, red) thresholds, falling back to
// the canonical defaults when the user omitted a field or set a non-positive
// value. Callers should use this rather than reading the raw fields so the
// validator and the TUI agree on the same boundary.
func (t TokenBadgeThresholds) Effective() (yellow, red int) {
	yellow = t.YellowAt
	red = t.RedAt
	if yellow <= 0 {
		yellow = DefaultTokenBadgeYellowAt
	}
	if red <= 0 {
		red = DefaultTokenBadgeRedAt
	}
	return yellow, red
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
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Entity      string   `json:"entity,omitempty"`
	Default     string   `json:"default,omitempty"`
	ProjectSlug string   `json:"project,omitempty"`
	Laws        []string `json:"laws,omitempty"`
	Body        string   `json:"body,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
	IsCustom    bool     `json:"is_custom,omitempty"`
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
