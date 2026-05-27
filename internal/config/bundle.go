package config

// Bundle is the in-memory representation of an omakiten config directory.
// `omakiten.yaml` provides settings, workflows, and reference wiring; per-entity
// `.md` files (skills/<slug>.md, laws/<slug>.md, personas/<slug>.md) provide the
// authoring content. The loader merges them into Bundle; the saver splits them
// back out.
type Bundle struct {
	Version       int                       `yaml:"version" json:"version"`
	Kit           Kit                       `yaml:"kit" json:"kit"`
	Config        Settings                  `yaml:"config" json:"config"`
	SubtaskKit    string                    `yaml:"-" json:"subtask_kit,omitempty"`
	SubtaskBundle *Bundle                   `yaml:"-" json:"subtask_bundle,omitempty"`
	Skills        []Skill                   `yaml:"-" json:"skills,omitempty"`
	Personas      []Persona                 `yaml:"-" json:"personas,omitempty"`
	Laws          []Law                     `yaml:"-" json:"laws,omitempty"`
	Templates     []TaskTemplate            `yaml:"-" json:"templates,omitempty"`
	Workflows     []Workflow                `yaml:"workflows" json:"workflows,omitempty"`
	Projects      []Project                 `yaml:"-" json:"projects,omitempty"`
	MCPCommands   map[string]MCPCommandSpec `yaml:"-" json:"mcp_commands,omitempty"`
	Notifications map[string]Notification   `yaml:"-" json:"notifications,omitempty"`
	Languages     []Language                `yaml:"-" json:"languages,omitempty"`
	// ActiveTheme is the theme resolved by LoadBundle from
	// themes/<Config.Theme.Active>.yaml (custom→default precedence). When
	// the loader cannot resolve the active slug the field is left as a
	// zero-Theme and ActiveThemeErr carries the underlying failure so CLI
	// commands that never render the TUI continue to load while TUI boot
	// and hot reload can surface ErrConfigInvalid through
	// Snapshot.ThemeError().
	ActiveTheme    Theme           `yaml:"-" json:"active_theme"`
	ActiveThemeErr error           `yaml:"-" json:"-"`
	Warnings       []SourceWarning `yaml:"-" json:"warnings,omitempty"`
	SourcePaths    []string        `yaml:"-" json:"-"`
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
	SubtaskKit  string                    `yaml:"subtask_kit,omitempty"`
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
	Output           OutputSettings      `yaml:"output" json:"output"`
	Context          ContextSettings     `yaml:"context" json:"context"`
	Workflow         WorkflowSettings    `yaml:"workflow" json:"workflow"`
	Theme            ThemeSettings       `yaml:"theme" json:"theme"`
	TemplateDefaults []string            `yaml:"template_defaults,omitempty" json:"template_defaults,omitempty"`
	Views            ViewSettings        `yaml:"views,omitempty" json:"views,omitempty"`
	MCP              MCPSettings         `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	TUI              TUISettings         `yaml:"tui,omitempty" json:"tui,omitempty"`
	SQLite           SQLiteSettings      `yaml:"sqlite,omitempty" json:"sqlite,omitempty"`
	ActivityLog      ActivityLogSettings `yaml:"activity_log,omitempty" json:"activity_log,omitempty"`
	Solutions        SolutionsSettings   `yaml:"solutions,omitempty" json:"solutions,omitempty"`
	Backup           BackupSettings      `yaml:"backup,omitempty" json:"backup,omitempty"`
	Events           EventsSettings      `yaml:"events,omitempty" json:"events,omitempty"`
	Search           SearchSettings      `yaml:"search,omitempty" json:"search,omitempty"`
	Hooks            []HookSpec          `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Tricks           TricksSettings      `yaml:"tricks,omitempty" json:"tricks,omitempty"`
	TagSynonyms      map[string]string   `yaml:"tag_synonyms,omitempty" json:"tag_synonyms,omitempty"`
	// Priorities is the configurable id↔value table for task priorities.
	// Code references the id (opaque); renderers resolve the value via
	// lookup. Authors who want to rename, add, or reorder priority labels
	// edit this block in YAML — no code changes required. Order follows
	// id ascending and is also the sort weight (higher id = higher
	// priority on `ORDER BY priority`). Required — validator rejects an
	// empty list; the kit YAML at defaults/omakiten.yaml ships the
	// canonical 3-entry table.
	Priorities []PriorityDefinition `yaml:"priorities,omitempty" json:"priorities,omitempty"`
	// Severities is the configurable id↔value table for law severities.
	// Same shape and contract as Priorities: code references the id,
	// frontmatter parsers resolve labels via lookup, renamer edits a
	// single line in YAML. Required — see Priorities.
	Severities []SeverityDefinition `yaml:"severities,omitempty" json:"severities,omitempty"`
	// Languages picks the active language code per surface. CLI and TUI
	// resolve against discovered languages/<code>.yaml files; AgentOutput
	// is a free-form directive surfaced into the MCP prompt composer
	// and is not validated against the catalog. See EffectiveLanguages
	// for defaults (en for CLI/TUI, empty for AgentOutput).
	Languages LanguageSettings `yaml:"languages,omitempty" json:"languages,omitempty"`
}

// LanguageSettings holds the three independent surface language codes.
// CLI drives help/usage chrome and CLI-owned errors. TUI drives every
// terminal-UI label and screen. AgentOutput is a free-form string
// (e.g. "English", "pt-br", "Português (Brasil)") appended to the MCP
// composer prompt as the trailing "Output language" directive; the
// agent honors it based on its own training rather than any catalog
// lookup, so any non-empty string is accepted at validation time.
type LanguageSettings struct {
	CLI         string `yaml:"cli,omitempty" json:"cli,omitempty"`
	TUI         string `yaml:"tui,omitempty" json:"tui,omitempty"`
	AgentOutput string `yaml:"agent_output,omitempty" json:"agent_output,omitempty"`
}

// EffectiveLanguages returns the resolved LanguageSettings with
// defaults applied: CLI and TUI fall back to "en"; AgentOutput stays
// empty when unset so the MCP composer skips the trailing directive
// line entirely. Field-by-field defaulting matches the project-local /
// user-global override semantics from #51 where each setting falls
// back independently if not supplied by the more specific layer.
func (s Settings) EffectiveLanguages() LanguageSettings {
	eff := s.Languages
	if eff.CLI == "" {
		eff.CLI = "en"
	}
	if eff.TUI == "" {
		eff.TUI = "en"
	}
	return eff
}

// SeverityDefinition is one row of the configurable law-severity table.
// ID is the storage handle; Value is the human label rendered in
// frontmatter, CLI output, MCP responses, and JSON marshaling. Default
// flags the severity applied when a law arrives without one (validator
// enforces at most one). Color is an optional theme-token name picked
// up by the TUI badge renderer (`error` / `warning` / `success` /
// `info`).
type SeverityDefinition struct {
	ID      int    `yaml:"id" json:"id"`
	Value   string `yaml:"value" json:"value"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
	Color   string `yaml:"color,omitempty" json:"color,omitempty"`
}

// EffectiveSeverities returns the resolved severity table. The validator
// guarantees `Severities` is non-empty when the bundle reaches runtime;
// the user's omakiten.yaml is the single canonical source. This method
// remains for the explicit naming at call sites (`bundle.Config.
// EffectiveSeverities()` reads more clearly than `bundle.Config.
// Severities`); it returns a fresh copy so callers can mutate without
// affecting the bundle.
func (s Settings) EffectiveSeverities() []SeverityDefinition {
	out := make([]SeverityDefinition, len(s.Severities))
	copy(out, s.Severities)
	return out
}

// DefaultSeverityID returns the id flagged `default: true` in the
// configured severity table, falling back to the middle entry's id
// when no flag is declared. Validator rejects multiple defaults and an
// empty table.
func (s Settings) DefaultSeverityID() int {
	if len(s.Severities) == 0 {
		return 0
	}
	for _, sev := range s.Severities {
		if sev.Default {
			return sev.ID
		}
	}
	return s.Severities[len(s.Severities)/2].ID
}

// PriorityDefinition is one row of the configurable priority table.
// ID is the storage handle and the sort weight; Value is the human label
// rendered in TUI/CLI/MCP/JSON output. Default flags the priority
// applied to tasks created without an explicit priority — at most one
// definition may set it (validator-enforced). Color is an optional
// theme-token name (e.g. `error`, `warning`, `success`) used by the TUI
// to colorize the badge; omitted falls back to the neutral info token.
type PriorityDefinition struct {
	ID      int    `yaml:"id" json:"id"`
	Value   string `yaml:"value" json:"value"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
	Color   string `yaml:"color,omitempty" json:"color,omitempty"`
}

// EffectivePriorities returns the resolved priority table. The validator
// guarantees `Priorities` is non-empty at runtime — the user's
// omakiten.yaml (materialised from defaults/omakiten.yaml on first run)
// is the single canonical source. This method exists for the explicit
// naming at call sites; it returns a fresh copy.
func (s Settings) EffectivePriorities() []PriorityDefinition {
	out := make([]PriorityDefinition, len(s.Priorities))
	copy(out, s.Priorities)
	return out
}

// DefaultPriorityID returns the id of the priority flagged `default: true`
// in the configured table, falling back to the middle entry's id when
// no explicit default is declared. Validator rejects multiple defaults
// and an empty table.
func (s Settings) DefaultPriorityID() int {
	if len(s.Priorities) == 0 {
		return 0
	}
	for _, p := range s.Priorities {
		if p.Default {
			return p.ID
		}
	}
	return s.Priorities[len(s.Priorities)/2].ID
}

// TemplateKinds returns the configured template_defaults list. Validator
// enforces non-empty in the loaded bundle so callers do not need a
// fallback — the kit's omakiten.yaml ships the canonical set and the
// user's file inherits via install-time materialisation.
func (s Settings) TemplateKinds() []string {
	out := make([]string, len(s.TemplateDefaults))
	copy(out, s.TemplateDefaults)
	return out
}

type OutputSettings struct {
	JSONMinified bool `yaml:"json_minified" json:"json_minified"`
	OmitEmpty    bool `yaml:"omit_empty" json:"omit_empty"`
}

// MCPSettings tunes how MCP responses are shaped to fit the agent's
// context window. **Every field is required** — the canonical values
// live in `defaults/omakiten.yaml` (the embedded kit YAML the installer
// materialises into the user's config root). Validator rejects bundles
// that omit any field. Pointer booleans (`*bool`) require an explicit
// `true` / `false` declaration; nil = invalid.
type MCPSettings struct {
	// RecentCommentLimit caps how many recent comments tools like
	// `tasks.continue` and `project.overview` ship per call. Required;
	// validator demands > 0.
	RecentCommentLimit int `yaml:"recent_comment_limit" json:"recent_comment_limit"`

	// MaxCommentChars truncates comment bodies past this length with a
	// trailing ellipsis when shipped over MCP. Required; validator
	// demands >= 0 (0 = no truncation).
	MaxCommentChars int `yaml:"max_comment_chars" json:"max_comment_chars"`

	// IncludeWorkflowInContinue toggles the `workflow` block in
	// `tasks.continue` responses. Required `*bool` — explicit
	// declaration so the user opts in or out deliberately.
	IncludeWorkflowInContinue *bool `yaml:"include_workflow_in_continue" json:"include_workflow_in_continue"`

	// CachePrompts toggles emitting the Anthropic-aware `cache_control`
	// hint on `prompts/get` content. Required `*bool`.
	CachePrompts *bool `yaml:"cache_prompts" json:"cache_prompts"`

	// RecentContextLimit caps how many recent context entries flow into
	// `tasks.continue` / `project.overview` / `project.resume`
	// responses. Required; validator demands > 0.
	RecentContextLimit int `yaml:"recent_context_limit" json:"recent_context_limit"`

	// NextWorkLimit caps the "likely next work" suggestion list shipped
	// in `project.resume`. Required; validator demands > 0.
	NextWorkLimit int `yaml:"next_work_limit" json:"next_work_limit"`

	// SimilarTaskLimit caps how many similar-task hints are surfaced by
	// `tasks.create_intent`. Required; validator demands > 0.
	SimilarTaskLimit int `yaml:"similar_task_limit" json:"similar_task_limit"`
}

// EffectiveRecentCommentLimit and friends are identity passthroughs
// kept for explicit naming at call sites. Validator guarantees the
// fields are valid when the bundle reaches runtime — no fallback.
func (m MCPSettings) EffectiveRecentCommentLimit() int { return m.RecentCommentLimit }
func (m MCPSettings) EffectiveMaxCommentChars() int    { return m.MaxCommentChars }
func (m MCPSettings) EffectiveIncludeWorkflowInContinue() bool {
	return m.IncludeWorkflowInContinue != nil && *m.IncludeWorkflowInContinue
}
func (m MCPSettings) EffectiveCachePrompts() bool {
	return m.CachePrompts != nil && *m.CachePrompts
}
func (m MCPSettings) EffectiveRecentContextLimit() int { return m.RecentContextLimit }
func (m MCPSettings) EffectiveNextWorkLimit() int      { return m.NextWorkLimit }
func (m MCPSettings) EffectiveSimilarTaskLimit() int   { return m.SimilarTaskLimit }

type ContextSettings struct {
	DefaultLevel int `yaml:"default_level" json:"default_level"`
	MaxTokens    int `yaml:"max_tokens" json:"max_tokens"`
}

// SQLiteSettings tunes the connection-level SQLite knobs the Store applies
// at Open time. Required block — the kit's defaults/omakiten.yaml ships
// the canonical value the user inherits at install time. PRAGMAs that
// describe correctness invariants (foreign_keys=ON, journal_mode=WAL,
// synchronous=NORMAL) intentionally stay in code: they encode the
// engine-level contract Omakiten depends on, not user preference.
type SQLiteSettings struct {
	// BusyTimeoutMs sets PRAGMA busy_timeout. Required; validator
	// demands > 0. Larger DBs or systems with concurrent writers may
	// need a higher value than the kit's default.
	BusyTimeoutMs int `yaml:"busy_timeout_ms" json:"busy_timeout_ms"`
	// CacheSizeKB tunes PRAGMA cache_size; passed as the negative
	// kilobyte form ("-N" = N KiB of page cache). Required; validator
	// demands > 0. Larger projects / longer task histories benefit
	// from a fatter page cache because the TUI keystroke read fan-out
	// repeatedly walks the same tables (tasks, events, deps).
	CacheSizeKB int `yaml:"cache_size_kb" json:"cache_size_kb"`
	// MmapSizeBytes sets PRAGMA mmap_size; 0 disables mmap, any
	// positive value asks SQLite to memory-map up to that many bytes
	// of the database file. Required; validator demands >= 0. Disabled
	// by default because mmap interacts poorly with NFS / FUSE mounts
	// some users keep their config root on.
	MmapSizeBytes int `yaml:"mmap_size_bytes" json:"mmap_size_bytes"`
}

// ActivityLogSettings declares the retention window for the per-call
// `operation` event log used by the activity feed. Required block — the
// kit ships the canonical values; users with longer support windows
// can raise MaxAgeDays without a code change.
type ActivityLogSettings struct {
	// MaxRows caps how many `operation` rows survive after a prune
	// pass. Older rows are deleted in id-DESC order. Required; > 0.
	MaxRows int `yaml:"max_rows" json:"max_rows"`
	// MaxAgeDays prunes `operation` rows older than this many days.
	// Required; > 0.
	MaxAgeDays int `yaml:"max_age_days" json:"max_age_days"`
}

// SolutionsSettings caps the `solutions.list_top` MCP response shape.
// DefaultTopLimit applies when a caller passes <=0; MaxTopLimit clamps
// caller-supplied limits so MCP responses stay bounded regardless of
// what the agent asks for. Required block.
type SolutionsSettings struct {
	// DefaultTopLimit is the limit applied when the caller omits one.
	// Required; > 0.
	DefaultTopLimit int `yaml:"default_top_limit" json:"default_top_limit"`
	// MaxTopLimit caps caller-supplied limits. Required; >=
	// DefaultTopLimit so the validator catches inverted ranges.
	MaxTopLimit int `yaml:"max_top_limit" json:"max_top_limit"`
}

// BackupSettings tunes the rolling DB snapshot the `okt db backup`
// command (and every destructive command that runs an auto-backup
// before mutating state) writes under StateDir/backups/. RetentionCount
// is the count cap pruneBackups enforces after each successful write:
// the N most-recent snapshots are kept, older ones deleted. The cap is
// best-effort — a prune failure never aborts the backup itself (the
// .db is already on disk). RetentionCount <= 0 disables pruning so
// power users keeping snapshots out-of-band can opt out without
// touching files.
type BackupSettings struct {
	// RetentionCount caps the count of snapshots BackupDir keeps after
	// a successful write. Validator requires >= 0; zero disables prune.
	RetentionCount int `yaml:"retention_count" json:"retention_count"`
}

// HookSpec is one entry of `config.hooks`. Mirrors hooks.Hook so the
// config layer stays free of an import cycle (internal/hooks imports
// config). Composition root maps HookSpec → hooks.Hook before handing
// the slice to the engine.
//
// Two mutually-exclusive shapes are supported:
//
//   - action shape:        on + when + do + args  (exec/noop dispatch)
//   - notification shape:  on + when + notification:<slug>  (per-event notification card)
//
// In notification shape the hook MAY also carry Message or MessageField as a
// fallback the action consults when the referenced notification YAML did
// not set its own. It may also carry DetailMessage or DetailMessageField,
// an optional second-page payload the TUI notification can show on tab.
// The notification YAML wins on tie-break — useful when the same
// notification fires on many events but the per-event hint is different.
//
// Validators reject mixing the action and notification shapes in the same
// entry.
type HookSpec struct {
	On                 string                 `yaml:"on" json:"on"`
	When               map[string]string      `yaml:"when,omitempty" json:"when,omitempty"`
	Do                 string                 `yaml:"do,omitempty" json:"do,omitempty"`
	Args               map[string]interface{} `yaml:"args,omitempty" json:"args,omitempty"`
	Notification       string                 `yaml:"notification,omitempty" json:"notification,omitempty"`
	Message            string                 `yaml:"message,omitempty" json:"message,omitempty"`
	MessageField       string                 `yaml:"message_field,omitempty" json:"message_field,omitempty"`
	DetailMessage      string                 `yaml:"detail_message,omitempty" json:"detail_message,omitempty"`
	DetailMessageField string                 `yaml:"detail_message_field,omitempty" json:"detail_message_field,omitempty"`
}

// TricksSettings holds the user-overridable parts of the TUI trick
// palette (Ctrl+K overlay). Nav remaps a positional 2-digit code (the
// shape the palette ScreenRegistry ships with) to a different Route
// slug — useful when the user's muscle memory expects a code on a
// different screen than the canonical layout. An empty Nav map keeps
// every positional default in place.
//
// Reserved-verb enforcement does not live in this struct: the
// validator inspects every HookSpec.When["verb"] entry against
// domain.ReservedTrickVerbs at load time and rejects matches with a
// hard error, so users cannot silently rebind built-in dispatch.
type TricksSettings struct {
	// Nav maps positional codes (2 digits in 1-9, e.g. "33") to Route
	// slugs (e.g. "settings.personas"). The palette layer validates
	// the slug-against-route-table semantic check at TUI bind time;
	// this struct's shape is the only contract the config validator
	// enforces.
	Nav map[string]string `yaml:"nav,omitempty" json:"nav,omitempty"`
}

// EventsSettings declares per-event-type channel policies (log to db,
// broadcast in-process, dispatch to hooks) plus the fallback recent-events
// limit used by `Store.ListRecentEvents` when callers pass <=0. Defaults
// apply to every known event type unless overridden in Overrides; an
// override only carries the channels it changes (others inherit defaults).
// Broadcast and Hook are reserved for the upcoming event-bus task —
// declared today so authors can stage configuration alongside log gating.
type EventsSettings struct {
	// DefaultRecentLimit is the fallback row count applied when the
	// caller passes <=0. Required; > 0.
	DefaultRecentLimit int `yaml:"default_recent_limit" json:"default_recent_limit"`
	// Defaults applies to every known event type unless explicitly
	// overridden. Required block.
	Defaults EventChannelSettings `yaml:"defaults" json:"defaults"`
	// Overrides keys are event_type strings (e.g. "tag.added"); values
	// override only the channels they declare. The validator rejects
	// keys outside domain.KnownEventTypes so typos surface at load time
	// rather than as silent no-ops.
	Overrides map[string]EventChannelSettings `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

// EventChannelSettings is the per-event tri-channel policy. Pointer fields
// distinguish "inherit default" (nil) from explicit false. The runtime
// only consumes Log today; Broadcast and Hook are reserved for the
// upcoming event-bus task.
type EventChannelSettings struct {
	Log       *bool `yaml:"log,omitempty" json:"log,omitempty"`
	Broadcast *bool `yaml:"broadcast,omitempty" json:"broadcast,omitempty"`
	Hook      *bool `yaml:"hook,omitempty" json:"hook,omitempty"`
}

// ResolveLog reports whether the given event_type should be persisted to
// the events table. Lookup is overrides → defaults; nil leaves at every
// layer mean "use the layer default" so authors can omit fields. When
// neither layer declares Log explicitly, the conservative answer is true
// (preserves the pre-feature behaviour).
func (e EventsSettings) ResolveLog(eventType string) bool {
	return resolveEventChannel(e, eventType, func(c EventChannelSettings) *bool { return c.Log })
}

// ResolveBroadcast reports whether the event bus should fan the event
// out to subscribers. Same overrides → defaults → true fallback as
// ResolveLog so an unconfigured runtime keeps broadcasting.
func (e EventsSettings) ResolveBroadcast(eventType string) bool {
	return resolveEventChannel(e, eventType, func(c EventChannelSettings) *bool { return c.Broadcast })
}

// ResolveHook reports whether the hooks engine should consider the
// event for dispatch. Same overrides → defaults → true fallback as
// ResolveLog.
func (e EventsSettings) ResolveHook(eventType string) bool {
	return resolveEventChannel(e, eventType, func(c EventChannelSettings) *bool { return c.Hook })
}

func resolveEventChannel(e EventsSettings, eventType string, pick func(EventChannelSettings) *bool) bool {
	if override, ok := e.Overrides[eventType]; ok {
		if v := pick(override); v != nil {
			return *v
		}
	}
	if v := pick(e.Defaults); v != nil {
		return *v
	}
	return true
}

// SearchSettings tunes text-similarity heuristics shared across
// agent-side ranking (similar-task hints, query overlap scoring).
// Required block — the kit ships an English baseline; multilingual
// users add Portuguese/Spanish/etc. words without a code change.
type SearchSettings struct {
	// Stopwords are the lowercase tokens dropped before computing
	// overlap scores. Required; non-empty.
	Stopwords []string `yaml:"stopwords" json:"stopwords"`
}

// TUISettings tunes how the terminal UI presents data. Required block
// — the kit's `defaults/omakiten.yaml` declares the canonical values
// the user inherits at install time.
type TUISettings struct {
	TokenBadge TokenBadgeThresholds `yaml:"token_badge" json:"token_badge"`
}

// TokenBadgeThresholds drives the colored TOKENS:N badge on entity
// cards. Above RedAt → red; above YellowAt → yellow; else green.
// Required fields: validator demands both > 0.
type TokenBadgeThresholds struct {
	YellowAt int `yaml:"yellow_at" json:"yellow_at"`
	RedAt    int `yaml:"red_at" json:"red_at"`
}

// Effective returns the configured (yellow, red) thresholds. Identity
// passthrough — validator guarantees both > 0 at runtime.
func (t TokenBadgeThresholds) Effective() (yellow, red int) {
	return t.YellowAt, t.RedAt
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

// EffectiveViews returns the configured ViewSettings. Identity
// passthrough kept for explicit naming at call sites — validator
// guarantees every required field (sort.field, sort.order, logs.limit)
// is set when the bundle reaches runtime.
func (s Settings) EffectiveViews() ViewSettings { return s.Views }

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
	ID          int                `yaml:"id" json:"id"`
	Key         string             `yaml:"key" json:"key"`
	Name        string             `yaml:"name" json:"name"`
	Buckets     []Bucket           `yaml:"buckets" json:"buckets"`
	Transitions []Transition       `yaml:"transitions" json:"transitions,omitempty"`
	Operations  WorkflowOperations `yaml:"operations,omitempty" json:"operations,omitempty"`
	// Defaults declares the workflow-level fallback for task/comment edit
	// and delete. nil means "no rule declared at this layer" — the
	// resolver falls through to bucket overrides and finally to the
	// implicit `true` (no rule = allow). Authors who want strict policy
	// declare it explicitly here.
	Defaults *WorkflowDefaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`
}

// WorkflowDefaults is the YAML mirror of domain.WorkflowDefaults. Comment
// inherits from Task field-by-field at every layer, so a workflow that
// declares only Task in defaults gets the same policy applied to comments
// without restating it.
type WorkflowDefaults struct {
	Task    *EntityPermission `yaml:"task,omitempty" json:"task,omitempty"`
	Comment *EntityPermission `yaml:"comment,omitempty" json:"comment,omitempty"`
}

type Bucket struct {
	ID          int                `yaml:"id" json:"id"`
	Key         string             `yaml:"key" json:"key"`
	Name        string             `yaml:"name" json:"name"`
	Position    int                `yaml:"position" json:"position"`
	Permissions *BucketPermissions `yaml:"permissions,omitempty" json:"permissions,omitempty"`
}

// BucketPermissions wires task/comment CRUD policy per bucket. nil pointers
// mean "no override at this layer" — resolution falls through to
// workflow.defaults and finally to the implicit `true`. Comment inherits
// from Task field-by-field at every layer.
type BucketPermissions struct {
	Task    *EntityPermission `yaml:"task,omitempty" json:"task,omitempty"`
	Comment *EntityPermission `yaml:"comment,omitempty" json:"comment,omitempty"`
}

// EntityPermission is the CRUD policy for one entity. Both fields are
// pointers so the YAML can omit a field and have the runtime fall through
// to the next layer of the resolution chain rather than treating the zero
// value as "explicit false".
type EntityPermission struct {
	Edit   *bool `yaml:"edit,omitempty" json:"edit,omitempty"`
	Delete *bool `yaml:"delete,omitempty" json:"delete,omitempty"`
}

// WorkflowOperations declares the guards that gate non-flow operations
// (archive / delete / unarchive). Reuses the TransitionGuard shape so the
// existing comments_tagged evaluator can be reused without a new guard type.
type WorkflowOperations struct {
	Archive   OperationPolicy `yaml:"archive,omitempty" json:"archive,omitempty"`
	Delete    OperationPolicy `yaml:"delete,omitempty" json:"delete,omitempty"`
	Unarchive OperationPolicy `yaml:"unarchive,omitempty" json:"unarchive,omitempty"`
}

type OperationPolicy struct {
	Guards []TransitionGuard `yaml:"guards,omitempty" json:"guards,omitempty"`
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
