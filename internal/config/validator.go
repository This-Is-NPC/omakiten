package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"omakiten/internal/domain"
)

var allowedSortOrders = map[string]struct{}{
	"asc":  {},
	"desc": {},
}

var (
	allowedTaskSortFields  = []string{"id", "title", "priority", "created_at"}
	allowedGraphSortFields = []string{"id", "title"}
)

// effectiveSeverityValues returns the configurable label set for law
// severity validation. Reads from config.severities (validator-required,
// non-empty by guarantee) so authors can rename, add, or reorder
// severity labels in YAML and have law frontmatter parse against the
// new set without a code change.
func effectiveSeverityValues(bundle Bundle) []string {
	sevs := bundle.Config.EffectiveSeverities()
	out := make([]string, len(sevs))
	for i, s := range sevs {
		out[i] = s.Value
	}
	return out
}

// effectivePriorityValues returns the configurable label set for
// priority view filters. Reads from config.priorities (validator-
// required, non-empty by guarantee) so an author who adds "urgent" or
// renames "high" can reference the new label in
// `config.views.{board,table}.filter.priority` without a code change.
func effectivePriorityValues(bundle Bundle) []string {
	prios := bundle.Config.EffectivePriorities()
	out := make([]string, len(prios))
	for i, p := range prios {
		out[i] = p.Value
	}
	return out
}

// ValidateBundle checks the merged bundle against on-disk entity sets.
//
// loadedSkills/loadedLaws/loadedPersonas hold the full set of files discovered
// in the per-entity folders; the resolved Bundle holds only the slugs actually
// referenced by omakiten.yaml. Validation enforces:
//   - settings/workflow shape
//   - every reference resolves to a loaded file
//   - severities are within the allowed enum
//   - persona skill refs resolve to loaded skills
//   - persona/project law refs resolve and don't double-list a global law
func ValidateBundle(bundle Bundle, loadedSkills []Skill, loadedLaws []Law, loadedPersonas []Persona, loadedTemplates []TaskTemplate) error {
	if bundle.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if err := requireKitFields(bundle.Kit); err != nil {
		return err
	}
	if bundle.Config.Context.DefaultLevel < 1 || bundle.Config.Context.DefaultLevel > 3 {
		return fmt.Errorf("config.context.default_level must be between 1 and 3")
	}
	if bundle.Config.Context.MaxTokens < 0 {
		return fmt.Errorf("config.context.max_tokens cannot be negative")
	}
	if strings.TrimSpace(bundle.Config.Workflow.Active) == "" {
		return fmt.Errorf("config.workflow.active is required")
	}
	if strings.TrimSpace(bundle.Config.Theme.Active) == "" {
		return fmt.Errorf("config.theme.active is required")
	}
	if err := validateViewSettings(bundle.Config.Views, bundle.Workflows, bundle.Config.Workflow.Active, effectivePriorityValues(bundle)); err != nil {
		return err
	}
	if err := validateMCPSettings(bundle.Config.MCP); err != nil {
		return err
	}
	if err := validateTUISettings(bundle.Config.TUI); err != nil {
		return err
	}
	if err := validateSQLiteSettings(bundle.Config.SQLite); err != nil {
		return err
	}
	if err := validateActivityLogSettings(bundle.Config.ActivityLog); err != nil {
		return err
	}
	if err := validateSolutionsSettings(bundle.Config.Solutions); err != nil {
		return err
	}
	if err := validateBackupSettings(bundle.Config.Backup); err != nil {
		return err
	}
	if err := validateEventsSettings(bundle.Config.Events); err != nil {
		return err
	}
	// Action-name resolution happens at composition root (after the
	// runtime registers built-ins); skip it here so plain LoadBundle
	// callers (tests, CLI subcommands) still validate event-type +
	// argv shape without needing an engine. The kit-local definitions
	// map supplies the closed set of valid event_types — the domain
	// registry is empty until LoadDomainEventRegistry runs, so passing
	// the bundle's own definitions keeps the typo guard intact during
	// LoadBundle.
	if err := ValidateHooks(bundle.Config.Hooks, KnownEventsFromDefinitions(bundle.Config.Events.Definitions), nil, bundle.Notifications); err != nil {
		return err
	}
	if err := validateTricksSettings(bundle.Config.Tricks, bundle.Config.Hooks); err != nil {
		return err
	}
	if err := validateSearchSettings(bundle.Config.Search); err != nil {
		return err
	}
	if err := validateTagSynonyms(bundle.Config.TagSynonyms); err != nil {
		return err
	}
	if err := validatePriorities(bundle.Config.Priorities); err != nil {
		return err
	}
	if err := validateSeverities(bundle.Config.Severities); err != nil {
		return err
	}
	if err := validateLanguageSettings(bundle.Config.Languages, bundle.Languages); err != nil {
		return err
	}
	if len(bundle.Config.TemplateDefaults) == 0 {
		return fmt.Errorf("config.template_defaults: required (declare the kinds the TUI picker offers; see defaults/omakiten.yaml)")
	}

	skillSet := slugSet(loadedSkillSlugs(loadedSkills))
	lawSet := slugSet(loadedLawSlugs(loadedLaws))
	personaSet := slugSet(loadedPersonaSlugs(loadedPersonas))
	templateSet := slugSet(loadedTemplateSlugs(loadedTemplates))

	for _, template := range bundle.Templates {
		if _, ok := templateSet[template.Slug]; !ok {
			return fmt.Errorf("templates: ref %q has no matching file", template.Slug)
		}
		seenLaw := map[string]struct{}{}
		for _, slug := range template.Laws {
			if _, dup := seenLaw[slug]; dup {
				return fmt.Errorf("templates.%s laws: duplicate %q", template.Slug, slug)
			}
			seenLaw[slug] = struct{}{}
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("templates.%s laws: ref %q has no matching law file", template.Slug, slug)
			}
		}
	}
	if err := validateTemplateDefaults(bundle); err != nil {
		return err
	}

	for _, skill := range bundle.Skills {
		if _, ok := skillSet[skill.Slug]; !ok {
			return fmt.Errorf("skills: ref %q has no matching file", skill.Slug)
		}
	}
	severityValues := effectiveSeverityValues(bundle)
	allowedSeveritySet := make(map[string]struct{}, len(severityValues))
	for _, v := range severityValues {
		allowedSeveritySet[v] = struct{}{}
	}
	for _, law := range bundle.Laws {
		if _, ok := lawSet[law.Slug]; !ok {
			return fmt.Errorf("laws: ref %q has no matching file", law.Slug)
		}
		if _, ok := allowedSeveritySet[law.Severity]; !ok {
			return fmt.Errorf("laws.%s has invalid severity %q (must match a value in config.severities)", law.Slug, law.Severity)
		}
	}
	for _, persona := range bundle.Personas {
		if _, ok := personaSet[persona.Slug]; !ok {
			return fmt.Errorf("personas: ref %q has no matching file", persona.Slug)
		}
		seenSkill := map[string]struct{}{}
		for _, slug := range persona.Skills {
			if _, dup := seenSkill[slug]; dup {
				return fmt.Errorf("personas.%s skills: duplicate %q", persona.Slug, slug)
			}
			seenSkill[slug] = struct{}{}
			if _, ok := skillSet[slug]; !ok {
				return fmt.Errorf("personas.%s skills: ref %q has no matching skill file", persona.Slug, slug)
			}
		}
		for _, slug := range persona.Laws {
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("personas.%s laws: ref %q has no matching law file", persona.Slug, slug)
			}
		}
	}

	for _, project := range bundle.Projects {
		if strings.TrimSpace(project.Slug) == "" {
			return fmt.Errorf("projects: slug is required")
		}
		if strings.TrimSpace(project.Name) == "" {
			return fmt.Errorf("projects.%s: name is required", project.Slug)
		}
		for _, slug := range project.Laws {
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("projects.%s laws: ref %q has no matching law file", project.Slug, slug)
			}
		}
	}

	if err := validateScopeUniqueness(bundle); err != nil {
		return err
	}

	if err := validateMCPCommands(bundle, personaSet, lawSet, templateSet); err != nil {
		return err
	}

	if err := validateMCPCommandSkillSubset(bundle); err != nil {
		return err
	}

	return validateWorkflows(bundle.Workflows, bundle.Config.Workflow.Active)
}

// validateMCPCommands enforces structural rules inside `mcp_commands`: empty
// command name and same-slug-in-both-laws-and-laws_disabled are hard errors
// because they encode a typo / contradiction the user cannot consciously
// want. Slug refs (persona/templates/laws) are NOT validated here — they
// are scanned by warnMCPCommandRefs and surfaced as bundle.Warnings so a
// missing persona or law file does not block the runtime from starting.
func validateMCPCommands(bundle Bundle, personaSet, lawSet, templateSet map[string]struct{}) error {
	_ = personaSet
	_ = lawSet
	_ = templateSet
	for name, spec := range bundle.MCPCommands {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("mcp_commands: empty command name")
		}
		if name != MCPCommandsGlobalKey {
			seen := map[string]struct{}{}
			for _, slug := range spec.Laws {
				seen[slug] = struct{}{}
			}
			for _, slug := range spec.LawsDisabled {
				if _, dup := seen[slug]; dup {
					return fmt.Errorf("mcp_commands.%s: law %q is in both laws and laws_disabled", name, slug)
				}
			}
		}
	}
	return nil
}

// validateMCPCommandSkillSubset enforces the schema-v2 rule (task #268):
// every slug in mcp_commands[name].skills must be a member of the bound
// persona's skill_repertoire. A command can only draw from skills the
// persona is equipped with. The error names the offending command, the
// persona, and the missing skills so the author can fix the wiring in one
// pass.
//
// Commands with no skills, or that bind no persona, are skipped — there is
// nothing to constrain. The reserved `global` key carries no persona/skills
// and is likewise skipped. Iteration order is sorted so the surfaced error
// is deterministic across runs.
func validateMCPCommandSkillSubset(bundle Bundle) error {
	if len(bundle.MCPCommands) == 0 {
		return nil
	}
	repertoire := map[string]map[string]struct{}{}
	for _, persona := range bundle.Personas {
		set := make(map[string]struct{}, len(persona.SkillRepertoire))
		for _, slug := range persona.SkillRepertoire {
			set[slug] = struct{}{}
		}
		repertoire[persona.Slug] = set
	}

	names := make([]string, 0, len(bundle.MCPCommands))
	for name := range bundle.MCPCommands {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if name == MCPCommandsGlobalKey {
			continue
		}
		spec := bundle.MCPCommands[name]
		if len(spec.Skills) == 0 {
			continue
		}
		personaSlug := strings.TrimSpace(spec.Persona)
		if personaSlug == "" {
			continue
		}
		pool := repertoire[personaSlug]
		var missing []string
		for _, slug := range spec.Skills {
			if _, ok := pool[slug]; !ok {
				missing = append(missing, slug)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf(
				"mcp_commands.%s.skills: %s not in persona %q skill_repertoire",
				name, strings.Join(missing, ", "), personaSlug,
			)
		}
	}
	return nil
}

// warnMCPCommandRefs collects soft warnings for every slug inside mcp_commands
// that has no matching loaded entity. Returns an empty slice on a clean
// bundle. Called from loader.go after ValidateBundle so the warnings ride
// along on the loaded bundle without aborting the load.
func warnMCPCommandRefs(bundle Bundle, personaSet, lawSet, templateSet map[string]struct{}) []SourceWarning {
	var warns []SourceWarning
	missingRef := func(scope, kind, slug string) {
		warns = append(warns, SourceWarning{Slug: slug, Message: fmt.Sprintf("%s: ref %q has no matching %s file", scope, slug, kind)})
	}
	for name, spec := range bundle.MCPCommands {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if name != MCPCommandsGlobalKey {
			if persona := strings.TrimSpace(spec.Persona); persona != "" {
				if _, ok := personaSet[persona]; !ok {
					missingRef("mcp_commands."+name+".persona", "persona", persona)
				}
			}
			for _, slug := range spec.Templates {
				if _, ok := templateSet[slug]; !ok {
					missingRef("mcp_commands."+name+".templates", "template", slug)
				}
			}
		}
		for _, slug := range spec.Laws {
			if _, ok := lawSet[slug]; !ok {
				missingRef("mcp_commands."+name+".laws", "law", slug)
			}
		}
		for _, slug := range spec.LawsDisabled {
			if _, ok := lawSet[slug]; !ok {
				missingRef("mcp_commands."+name+".laws_disabled", "law", slug)
			}
		}
	}
	return warns
}

// validateMCPSettings enforces that every MCP-shape knob is declared
// in the bundle. The runtime has no in-code fallback; the canonical
// values live in defaults/omakiten.yaml (the embedded kit YAML the
// installer materialises). A user who removes a field gets an error
// pointing at the kit so the fix is obvious.
func validateMCPSettings(m MCPSettings) error {
	if m.RecentCommentLimit <= 0 {
		return fmt.Errorf("config.mcp.recent_comment_limit: must be > 0 (see defaults/omakiten.yaml for canonical values)")
	}
	if m.MaxCommentChars < 0 {
		return fmt.Errorf("config.mcp.max_comment_chars: must be >= 0 (0 = no truncation)")
	}
	if m.IncludeWorkflowInContinue == nil {
		return fmt.Errorf("config.mcp.include_workflow_in_continue: required boolean (see defaults/omakiten.yaml)")
	}
	if m.CachePrompts == nil {
		return fmt.Errorf("config.mcp.cache_prompts: required boolean (see defaults/omakiten.yaml)")
	}
	if m.RecentContextLimit <= 0 {
		return fmt.Errorf("config.mcp.recent_context_limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	if m.NextWorkLimit <= 0 {
		return fmt.Errorf("config.mcp.next_work_limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	if m.SimilarTaskLimit <= 0 {
		return fmt.Errorf("config.mcp.similar_task_limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	return nil
}

// validateSQLiteSettings enforces the SQLite connection-tuning block.
// Required and non-zero; the kit ships the canonical busy_timeout the
// user inherits at install time.
func validateSQLiteSettings(s SQLiteSettings) error {
	if s.BusyTimeoutMs <= 0 {
		return fmt.Errorf("config.sqlite.busy_timeout_ms: must be > 0 (see defaults/omakiten.yaml)")
	}
	if s.CacheSizeKB <= 0 {
		return fmt.Errorf("config.sqlite.cache_size_kb: must be > 0 (see defaults/omakiten.yaml)")
	}
	if s.MmapSizeBytes < 0 {
		return fmt.Errorf("config.sqlite.mmap_size_bytes: must be >= 0 (0 disables mmap; see defaults/omakiten.yaml)")
	}
	return nil
}

// validateActivityLogSettings enforces the operation-log retention
// block. Both knobs are required and positive — disabling retention is
// not a supported mode (the activity log would grow unbounded).
func validateActivityLogSettings(a ActivityLogSettings) error {
	if a.MaxRows <= 0 {
		return fmt.Errorf("config.activity_log.max_rows: must be > 0 (see defaults/omakiten.yaml)")
	}
	if a.MaxAgeDays <= 0 {
		return fmt.Errorf("config.activity_log.max_age_days: must be > 0 (see defaults/omakiten.yaml)")
	}
	return nil
}

// validateSolutionsSettings enforces the solutions.list_top limits.
// Both required; max must be >= default so the validator catches an
// inverted range that would clamp every caller below the implicit
// floor.
func validateSolutionsSettings(s SolutionsSettings) error {
	if s.DefaultTopLimit <= 0 {
		return fmt.Errorf("config.solutions.default_top_limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	if s.MaxTopLimit <= 0 {
		return fmt.Errorf("config.solutions.max_top_limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	if s.MaxTopLimit < s.DefaultTopLimit {
		return fmt.Errorf("config.solutions: max_top_limit (%d) must be >= default_top_limit (%d)", s.MaxTopLimit, s.DefaultTopLimit)
	}
	return nil
}

// validateBackupSettings enforces non-negative RetentionCount. Zero is
// legal and means "no prune" — power users keeping snapshots manually
// opt out without touching files. Negative values are rejected since
// they have no defined semantic and would mask a config typo.
func validateBackupSettings(s BackupSettings) error {
	if s.RetentionCount < 0 {
		return fmt.Errorf("config.backup.retention_count: must be >= 0 (0 disables prune)")
	}
	return nil
}

// validateEventsSettings enforces the events.default_recent_limit
// fallback used by ListRecentEvents and the per-event channel policy
// shape: defaults must declare every channel explicitly so runtime
// behaviour is deterministic; overrides keys must be known event types
// (typos rejected at load time, not silently ignored).
func validateEventsSettings(e EventsSettings) error {
	if e.DefaultRecentLimit <= 0 {
		return fmt.Errorf("config.events.default_recent_limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	if e.Defaults.Log == nil {
		return fmt.Errorf("config.events.defaults.log: required (see defaults/omakiten.yaml)")
	}
	if e.Defaults.Broadcast == nil {
		return fmt.Errorf("config.events.defaults.broadcast: required (see defaults/omakiten.yaml)")
	}
	if e.Defaults.Hook == nil {
		return fmt.Errorf("config.events.defaults.hook: required (see defaults/omakiten.yaml)")
	}
	for key := range e.Overrides {
		if _, ok := e.Definitions[key]; !ok {
			return fmt.Errorf("config.events.overrides: unknown event_type %q (declare it under config.events.definitions in the active kit)", key)
		}
	}
	return nil
}

// KnownEventsFromDefinitions converts an EventsSettings.Definitions map
// into the set shape ValidateHooks expects. Empty input returns nil so
// callers without an events block (test fixtures) skip the typo guard.
// Exported because agentruntime composes hook validation against the
// project's bundle without going through ValidateBundle.
func KnownEventsFromDefinitions(defs map[string]EventDefinitionSettings) map[string]struct{} {
	if len(defs) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(defs))
	for k := range defs {
		out[k] = struct{}{}
	}
	return out
}

// validateTricksSettings enforces the shape of the tricks: block:
//   - every nav override code matches the positional 2-digit grammar
//     (^[1-9][1-9]$); typos fail at load time, not at first keypress
//   - every nav override route slug is a non-empty string (semantic
//     route-table check is owned by palette.New at TUI bind time so
//     this package stays free of an import cycle into internal/tui)
//   - no HookSpec.When["verb"] entry matches a domain.ReservedTrickVerbs
//     value — built-in dispatch for `nav`/`op` is hard-coded in the
//     palette handler, so a user hook filtering on those verbs would
//     shadow the built-in behaviour expected by every other user
func validateTricksSettings(t TricksSettings, hooks []HookSpec) error {
	for code, route := range t.Nav {
		if !trickNavCodePattern.MatchString(code) {
			return fmt.Errorf("config.tricks.nav: code %q is malformed (want 2 digits in 1-9, e.g. \"11\" or \"33\")", code)
		}
		if strings.TrimSpace(route) == "" {
			return fmt.Errorf("config.tricks.nav[%q]: route slug must be non-empty", code)
		}
	}
	for i, hook := range hooks {
		verb, ok := hook.When["verb"]
		if !ok {
			continue
		}
		if domain.IsReservedTrickVerb(verb) {
			return fmt.Errorf("config.hooks[%d].when.verb: %q is reserved by the trick palette built-in handler; pick a different verb (reserved: %v)", i, verb, domain.ReservedTrickVerbs)
		}
	}
	return nil
}

// trickNavCodePattern is the positional 2-digit grammar shared with the
// palette ScreenRegistry. Duplicated rather than imported because
// config cannot import internal/tui/palette (palette imports nothing
// from config either way, so a thin shared constant in domain would be
// the only alternative — not worth the indirection for a 12-char
// regex).
var trickNavCodePattern = regexp.MustCompile(`^[1-9][1-9]$`)

// validateSearchSettings enforces the stopwords block. Non-empty so
// multilingual users have a clear extension point: edit the YAML to
// add Portuguese/Spanish/etc. stopwords without touching code.
func validateSearchSettings(s SearchSettings) error {
	if len(s.Stopwords) == 0 {
		return fmt.Errorf("config.search.stopwords: required non-empty list (see defaults/omakiten.yaml)")
	}
	seen := map[string]struct{}{}
	for _, word := range s.Stopwords {
		trimmed := strings.TrimSpace(word)
		if trimmed == "" {
			return fmt.Errorf("config.search.stopwords: empty entry")
		}
		if trimmed != strings.ToLower(trimmed) {
			return fmt.Errorf("config.search.stopwords: entry %q must be lowercase (matching tokenizer output)", word)
		}
		if _, dup := seen[trimmed]; dup {
			return fmt.Errorf("config.search.stopwords: duplicate %q", trimmed)
		}
		seen[trimmed] = struct{}{}
	}
	return nil
}

// validateTagSynonyms enforces the alias map shape: keys and values
// are non-empty kebab-case slugs, no entry maps a slug to itself, and
// no value is itself a key (which would create a two-hop normalisation
// chain the runtime does not follow). Empty map is allowed — users may
// remove aliases entirely.
func validateTagSynonyms(syns map[string]string) error {
	if len(syns) == 0 {
		return fmt.Errorf("config.tag_synonyms: required non-empty map (see defaults/omakiten.yaml)")
	}
	for from, to := range syns {
		fromTrim := strings.TrimSpace(from)
		toTrim := strings.TrimSpace(to)
		if fromTrim == "" {
			return fmt.Errorf("config.tag_synonyms: empty key")
		}
		if toTrim == "" {
			return fmt.Errorf("config.tag_synonyms[%q]: empty target", from)
		}
		if fromTrim == toTrim {
			return fmt.Errorf("config.tag_synonyms[%q]: maps to itself", from)
		}
		if _, chain := syns[toTrim]; chain {
			return fmt.Errorf("config.tag_synonyms[%q]: target %q is itself a key (two-hop chains are not resolved)", from, to)
		}
	}
	return nil
}

// validateTUISettings enforces the TUI block. Currently scoped to the
// token-badge thresholds which the renderer needs at every paint; as
// more TUI knobs migrate from code to YAML, add their checks here.
func validateTUISettings(t TUISettings) error {
	if t.TokenBadge.YellowAt <= 0 {
		return fmt.Errorf("config.tui.token_badge.yellow_at: must be > 0 (see defaults/omakiten.yaml)")
	}
	if t.TokenBadge.RedAt <= 0 {
		return fmt.Errorf("config.tui.token_badge.red_at: must be > 0 (see defaults/omakiten.yaml)")
	}
	if t.TokenBadge.RedAt <= t.TokenBadge.YellowAt {
		return fmt.Errorf("config.tui.token_badge: red_at (%d) must be > yellow_at (%d)", t.TokenBadge.RedAt, t.TokenBadge.YellowAt)
	}
	return nil
}

// validateScopeUniqueness ensures a single law slug is not declared both as a
// top-level (global) ref and as a persona/project-scoped ref.
func validateScopeUniqueness(bundle Bundle) error {
	seenScope := map[string]string{}
	for _, law := range bundle.Laws {
		if existing, dup := seenScope[law.Slug]; dup {
			return fmt.Errorf("laws.%s declared in multiple scopes (%s and %s)", law.Slug, existing, law.Scope)
		}
		seenScope[law.Slug] = law.Scope
	}
	return nil
}

func loadedSkillSlugs(items []Skill) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

func loadedLawSlugs(items []Law) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

func loadedPersonaSlugs(items []Persona) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

// validateTemplateDefaults enforces the new default-binding model:
//   - every template's `default:` value must be in config.template_defaults
//   - at most one template per (default, project) pair (uniqueness)
//
// `project:` refs are NOT validated against bundle.Projects — that section
// only declares declarative wiring, while the live project a template
// scopes to may be tracked in SQLite (the runtime source of truth) and
// never appear in the yaml. The runtime resolver falls back to the global
// binding when a project ref does not match an active project.
//
// Templates without a `default:` are inactive and pass validation as-is.
func validateTemplateDefaults(bundle Bundle) error {
	allowed := map[string]struct{}{}
	for _, kind := range bundle.Config.TemplateKinds() {
		allowed[kind] = struct{}{}
	}

	type slot struct {
		kind, project string
	}
	seen := map[slot]string{}
	for _, t := range bundle.Templates {
		if t.Default == "" {
			continue
		}
		if _, ok := allowed[t.Default]; !ok {
			return fmt.Errorf("templates.%s: default %q is not in config.template_defaults", t.Slug, t.Default)
		}
		key := slot{kind: t.Default, project: t.ProjectSlug}
		if other, dup := seen[key]; dup {
			scope := "global"
			if t.ProjectSlug != "" {
				scope = "project=" + t.ProjectSlug
			}
			return fmt.Errorf("templates.%s and templates.%s both declare default=%q (%s) — only one may", other, t.Slug, t.Default, scope)
		}
		seen[key] = t.Slug
	}
	return nil
}

// validatePriorities enforces the shape of the configurable priority
// table: ids are positive and unique, values are non-empty and unique,
// and at most one entry may flag itself default. The block is
// required (non-empty); the kit YAML at defaults/omakiten.yaml ships
// the canonical 3-entry table.
//
// Also rejects declaration order that does not match ascending id
// order. The id is the SQL sort weight (`ORDER BY priority` reads
// `priority_id`), and the TUI cycle follows slice order — declaring
// `[{id:3, value:high}, {id:1, value:low}]` would silently invert
// both. Forcing ascending declaration order keeps the YAML readable
// and the runtime semantics predictable.
func validatePriorities(priorities []PriorityDefinition) error {
	if len(priorities) == 0 {
		return fmt.Errorf("config.priorities: required block (see defaults/omakiten.yaml for the canonical id↔value table)")
	}
	seenID := map[int]string{}
	seenValue := map[string]int{}
	defaults := 0
	prevID := 0
	for _, p := range priorities {
		if p.ID <= 0 {
			return fmt.Errorf("config.priorities: id must be positive, got %d for value %q", p.ID, p.Value)
		}
		value := strings.TrimSpace(p.Value)
		if value == "" {
			return fmt.Errorf("config.priorities[id=%d]: value is required", p.ID)
		}
		if existing, dup := seenID[p.ID]; dup {
			return fmt.Errorf("config.priorities: id %d declared twice (values %q and %q)", p.ID, existing, value)
		}
		seenID[p.ID] = value
		if otherID, dup := seenValue[value]; dup {
			return fmt.Errorf("config.priorities: value %q declared twice (ids %d and %d)", value, otherID, p.ID)
		}
		seenValue[value] = p.ID
		if p.Default {
			defaults++
		}
		if p.ID <= prevID {
			return fmt.Errorf("config.priorities: ids must be declared in ascending order (id %d after %d for value %q) — id is the SQL sort weight and the TUI cycle follows slice order", p.ID, prevID, value)
		}
		prevID = p.ID
	}
	if defaults > 1 {
		return fmt.Errorf("config.priorities: at most one entry may set default: true (got %d)", defaults)
	}
	return nil
}

// validateSeverities mirrors validatePriorities for law severities:
// positive unique ids, non-empty unique values, at most one default,
// and ascending declaration order. Block is required (non-empty);
// the kit YAML at defaults/omakiten.yaml ships the canonical 3-entry
// table.
func validateSeverities(severities []SeverityDefinition) error {
	if len(severities) == 0 {
		return fmt.Errorf("config.severities: required block (see defaults/omakiten.yaml for the canonical id↔value table)")
	}
	seenID := map[int]string{}
	seenValue := map[string]int{}
	defaults := 0
	prevID := 0
	for _, s := range severities {
		if s.ID <= 0 {
			return fmt.Errorf("config.severities: id must be positive, got %d for value %q", s.ID, s.Value)
		}
		value := strings.TrimSpace(s.Value)
		if value == "" {
			return fmt.Errorf("config.severities[id=%d]: value is required", s.ID)
		}
		if existing, dup := seenID[s.ID]; dup {
			return fmt.Errorf("config.severities: id %d declared twice (values %q and %q)", s.ID, existing, value)
		}
		seenID[s.ID] = value
		if otherID, dup := seenValue[value]; dup {
			return fmt.Errorf("config.severities: value %q declared twice (ids %d and %d)", value, otherID, s.ID)
		}
		seenValue[value] = s.ID
		if s.Default {
			defaults++
		}
		if s.ID <= prevID {
			return fmt.Errorf("config.severities: ids must be declared in ascending order (id %d after %d for value %q)", s.ID, prevID, value)
		}
		prevID = s.ID
	}
	if defaults > 1 {
		return fmt.Errorf("config.severities: at most one entry may set default: true (got %d)", defaults)
	}
	return nil
}

func loadedTemplateSlugs(items []TaskTemplate) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Slug)
	}
	return out
}

func slugSet(slugs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		out[slug] = struct{}{}
	}
	return out
}

func ValidateTheme(theme Theme) error {
	if theme.Version != 1 {
		return fmt.Errorf("theme.version must be 1")
	}
	if strings.TrimSpace(theme.Key) == "" {
		return fmt.Errorf("theme.key is required")
	}
	if strings.TrimSpace(theme.Name) == "" {
		return fmt.Errorf("theme.name is required")
	}
	if len(theme.Colors) == 0 {
		return fmt.Errorf("theme.colors is required")
	}
	return nil
}

func validateWorkflows(workflows []Workflow, activeKey string) error {
	if len(workflows) == 0 {
		return fmt.Errorf("workflows is required")
	}

	activeFound := false
	if err := validateItems("workflows", workflows, func(workflow Workflow) (int, string, string) {
		return workflow.ID, workflow.Key, workflow.Name
	}); err != nil {
		return err
	}

	for _, workflow := range workflows {
		if workflow.Key == activeKey {
			activeFound = true
		}
		if len(workflow.Buckets) == 0 {
			return fmt.Errorf("workflows.%s.buckets is required", workflow.Key)
		}

		bucketIDs := map[int]struct{}{}
		if err := validateItems("workflows."+workflow.Key+".buckets", workflow.Buckets, func(bucket Bucket) (int, string, string) {
			return bucket.ID, bucket.Key, bucket.Name
		}); err != nil {
			return err
		}
		for _, bucket := range workflow.Buckets {
			bucketIDs[bucket.ID] = struct{}{}
			if bucket.Position < 1 {
				return fmt.Errorf("workflows.%s.buckets.%s.position must be positive", workflow.Key, bucket.Key)
			}
		}

		bucketKeySet := make(map[string]struct{}, len(workflow.Buckets))
		for _, bucket := range workflow.Buckets {
			bucketKeySet[bucket.Key] = struct{}{}
		}

		seenTransitions := map[[2]int]struct{}{}
		for _, transition := range workflow.Transitions {
			if _, ok := bucketIDs[transition.From]; !ok {
				return fmt.Errorf("workflows.%s transitions from missing bucket id %d", workflow.Key, transition.From)
			}
			if _, ok := bucketIDs[transition.To]; !ok {
				return fmt.Errorf("workflows.%s transitions to missing bucket id %d", workflow.Key, transition.To)
			}
			key := [2]int{transition.From, transition.To}
			if _, exists := seenTransitions[key]; exists {
				return fmt.Errorf("workflows.%s has duplicated transition %d -> %d", workflow.Key, transition.From, transition.To)
			}
			seenTransitions[key] = struct{}{}

			if err := validateGuards(workflow.Key, fmt.Sprintf("transition %d→%d", transition.From, transition.To), transition.Guards, bucketKeySet); err != nil {
				return err
			}
		}

		if err := validatePermissionScopes(workflow); err != nil {
			return err
		}

		if err := validateGuards(workflow.Key, "operations.archive", workflow.Operations.Archive.Guards, bucketKeySet); err != nil {
			return err
		}
		if err := validateGuards(workflow.Key, "operations.delete", workflow.Operations.Delete.Guards, bucketKeySet); err != nil {
			return err
		}
		if err := validateGuards(workflow.Key, "operations.unarchive", workflow.Operations.Unarchive.Guards, bucketKeySet); err != nil {
			return err
		}
	}

	if !activeFound {
		return fmt.Errorf("config.workflow.active %q does not match any workflow", activeKey)
	}

	return nil
}

// validatePermissionScopes enforces the rule that per-scope sub-blocks
// (task/project/universal) are only meaningful on workflows[].defaults.comment.
// They are rejected anywhere else: on defaults.task, and on any bucket-level
// permission block (task or comment) — buckets have no scope dimension, and a
// scope sub-block there would silently never resolve. The comment scope key
// set is closed to {task, project, universal}; the strict YAML decoder rejects
// any other key at parse time, so this validator guards the structural
// placement rather than the key names themselves.
func validatePermissionScopes(workflow Workflow) error {
	if workflow.Defaults != nil {
		if hasScopeBlocks(workflow.Defaults.Task) {
			return fmt.Errorf("workflows.%s.defaults.task: scope sub-blocks (task/project/universal) are only valid under defaults.comment", workflow.Key)
		}
	}
	for _, bucket := range workflow.Buckets {
		if bucket.Permissions == nil {
			continue
		}
		if hasScopeBlocks(bucket.Permissions.Task) {
			return fmt.Errorf("workflows.%s.buckets.%s.permissions.task: scope sub-blocks are not valid at the bucket level", workflow.Key, bucket.Key)
		}
		if hasScopeBlocks(bucket.Permissions.Comment) {
			return fmt.Errorf("workflows.%s.buckets.%s.permissions.comment: scope sub-blocks (task/project/universal) are only valid under workflow.defaults.comment", workflow.Key, bucket.Key)
		}
	}
	return nil
}

// hasScopeBlocks reports whether any per-scope sub-block is declared on the
// permission. nil is treated as "no sub-blocks".
func hasScopeBlocks(p *EntityPermission) bool {
	return p != nil && (p.Task != nil || p.Project != nil || p.Universal != nil)
}

// validateGuards enforces the comments_tagged / comments_min / blockers_in
// shape uniformly across transition guards and operation guards. Operation
// guards in the MVP only support comments_tagged, but enforcement happens at
// the engine level — the validator stays permissive so operators can
// experiment with comments_min where it makes sense (e.g. delete needs ≥3
// comments).
func validateGuards(workflowKey, scope string, guards []TransitionGuard, bucketKeySet map[string]struct{}) error {
	for _, guard := range guards {
		switch guard.Type {
		case "blockers_in":
			if len(guard.Buckets) == 0 {
				return fmt.Errorf("workflows.%s %s guard blockers_in: buckets is required", workflowKey, scope)
			}
			for _, bKey := range guard.Buckets {
				if _, ok := bucketKeySet[bKey]; !ok {
					return fmt.Errorf("workflows.%s %s guard blockers_in: bucket key %q not found in workflow", workflowKey, scope, bKey)
				}
			}
		case "comments_min":
			if guard.Count < 1 {
				return fmt.Errorf("workflows.%s %s guard comments_min: count must be >= 1", workflowKey, scope)
			}
		case "comments_tagged":
			if strings.TrimSpace(guard.Tag) == "" {
				return fmt.Errorf("workflows.%s %s guard comments_tagged: tag is required", workflowKey, scope)
			}
			if guard.Count < 1 {
				return fmt.Errorf("workflows.%s %s guard comments_tagged: count must be >= 1", workflowKey, scope)
			}
		case "wave_gate":
			// wave_gate has no extra fields — pending count is derived
			// from the task's wave + plan and the workflow's final
			// bucket. The hint string is optional and validated by the
			// shared TransitionGuard shape, not here.
		case "subtasks_complete":
			// subtasks_complete has no extra fields either — the guard
			// reads tasks.parent_id and the workflow's final bucket
			// directly. Hint is optional like the others.
		default:
			return fmt.Errorf("workflows.%s %s: unknown guard type %q", workflowKey, scope, guard.Type)
		}
	}
	return nil
}

func validateItems[T any](section string, items []T, extract func(T) (int, string, string)) error {
	seenIDs := map[int]struct{}{}
	seenKeys := map[string]struct{}{}

	for _, item := range items {
		id, key, name := extract(item)
		if err := requireIDKeyName(section, id, key, name); err != nil {
			return err
		}
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("%s has duplicated id %d", section, id)
		}
		seenIDs[id] = struct{}{}

		if _, exists := seenKeys[key]; exists {
			return fmt.Errorf("%s has duplicated key %q", section, key)
		}
		seenKeys[key] = struct{}{}
	}
	return nil
}

func requireIDKeyName(section string, id int, key, name string) error {
	if id <= 0 {
		return fmt.Errorf("%s.id must be positive", section)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%s.key is required", section)
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s.name is required", section)
	}
	return nil
}

func requireKitFields(kit Kit) error {
	return requireIDKeyName("kit", kit.ID, kit.Key, kit.Name)
}

// validateViewSettings enforces per-view sort/filter rules. Every
// sort field/order is required — the kit YAML at defaults/omakiten.
// yaml ships the canonical values; the user file inherits at install
// time. Filter blocks default to empty lists (interpreted as "all
// values allowed").
//
// allowedPriorities is the resolved label set from config.priorities
// so authors who declare custom priority labels can reference them in
// `config.views.{board,table}.filter.priority` without a code change.
func validateViewSettings(v ViewSettings, workflows []Workflow, activeWorkflow string, allowedPriorities []string) error {
	if err := validateRequiredSort("config.views.board.sort", v.Board.Sort, allowedTaskSortFields, true); err != nil {
		return err
	}
	if err := validateStringSet("config.views.board.filter.priority", v.Board.Filter.Priority, allowedPriorities); err != nil {
		return err
	}

	if err := validateRequiredSort("config.views.table.sort", v.Table.Sort, allowedTaskSortFields, true); err != nil {
		return err
	}
	if err := validateStringSet("config.views.table.filter.priority", v.Table.Filter.Priority, allowedPriorities); err != nil {
		return err
	}
	if len(v.Table.Filter.Bucket) > 0 {
		if err := validateBucketKeys("config.views.table.filter.bucket", v.Table.Filter.Bucket, workflows, activeWorkflow); err != nil {
			return err
		}
	}

	if err := validateRequiredSort("config.views.graph.sort", v.Graph.Sort, allowedGraphSortFields, true); err != nil {
		return err
	}

	// Logs only carries an order — `field` is meaningless for time-series.
	if err := validateRequiredSort("config.views.logs.sort", v.Logs.Sort, nil, false); err != nil {
		return err
	}
	if v.Logs.Limit <= 0 {
		return fmt.Errorf("config.views.logs.limit: must be > 0 (see defaults/omakiten.yaml)")
	}
	if v.Logs.WindowDays <= 0 {
		return fmt.Errorf("config.views.logs.window_days: must be > 0 (see defaults/omakiten.yaml)")
	}

	if err := validateRequiredSort("config.views.task_activity.sort", v.TaskActivity.Sort, nil, false); err != nil {
		return err
	}

	return nil
}

// validateRequiredSort is the strict variant of the original sort
// validator: every sort block MUST declare its order, and views with
// `requireField=true` MUST also declare a field. Empty values used to
// fall through to canonical defaults; with the kit YAML now the only
// canonical source, omitted fields are an authoring error.
func validateRequiredSort(section string, sort SortSettings, allowedFields []string, requireField bool) error {
	if requireField {
		if sort.Field == "" {
			return fmt.Errorf("%s.field: required (see defaults/omakiten.yaml)", section)
		}
		if !containsString(allowedFields, sort.Field) {
			return fmt.Errorf("%s.field %q is not one of %v", section, sort.Field, allowedFields)
		}
	} else if sort.Field != "" {
		return fmt.Errorf("%s.field is not configurable", section)
	}
	if sort.Order == "" {
		return fmt.Errorf("%s.order: required (see defaults/omakiten.yaml)", section)
	}
	if _, ok := allowedSortOrders[sort.Order]; !ok {
		return fmt.Errorf("%s.order %q must be \"asc\" or \"desc\"", section, sort.Order)
	}
	return nil
}

func validateStringSet(section string, values, allowed []string) error {
	for _, value := range values {
		if !containsString(allowed, value) {
			return fmt.Errorf("%s value %q is not one of %v", section, value, allowed)
		}
	}
	return nil
}

func validateBucketKeys(section string, values []string, workflows []Workflow, activeWorkflow string) error {
	var keys map[string]struct{}
	for _, w := range workflows {
		if w.Key == activeWorkflow {
			keys = make(map[string]struct{}, len(w.Buckets))
			for _, b := range w.Buckets {
				keys[b.Key] = struct{}{}
			}
			break
		}
	}
	if keys == nil {
		// Active workflow is validated separately; if we got here without
		// finding it, the workflow validator will surface a clearer error.
		return nil
	}
	for _, value := range values {
		if _, ok := keys[value]; !ok {
			return fmt.Errorf("%s value %q is not a bucket key in workflow %q", section, value, activeWorkflow)
		}
	}
	return nil
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// validateLanguageSettings rejects unknown codes for config.languages.cli
// and config.languages.tui. agent_output stays free-form per task #82
// §9 — it is a directive consumed by the agent, not a catalog lookup,
// so any non-empty string is accepted.
//
// Empty cli/tui values pass through: EffectiveLanguages defaults them
// to "en" which itself is validated against the loaded catalog. So a
// missing bundled en pack is the failure surface when nothing else is
// configured — the validator points at the missing code, not at the
// empty config.
func validateLanguageSettings(ls LanguageSettings, loaded []Language) error {
	// When no languages are loaded at all (test bundles, legacy installs
	// pre-i18n materialization), skip validation entirely. The Catalog
	// degrades gracefully: missing keys return the key literal. Fresh
	// installs materialize defaults/languages/en.yaml so this branch is
	// the exception, not the norm.
	if len(loaded) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(loaded))
	codes := make([]string, 0, len(loaded))
	for _, lang := range loaded {
		available[lang.Code] = struct{}{}
		codes = append(codes, lang.Code)
	}
	sort.Strings(codes)
	check := func(field, value string) error {
		raw := strings.TrimSpace(value)
		resolved := raw
		defaulted := false
		if resolved == "" {
			resolved = "en"
			defaulted = true
		}
		if _, ok := available[resolved]; ok {
			return nil
		}
		// Defaulted values come from an unset config field — surface that
		// in the error so the user does not chase a literal they never
		// typed. The bare-field-name form mirrors the explicit case so
		// downstream parsers still see config.languages.<field>.
		if defaulted {
			return fmt.Errorf("config.languages.%s: field unset and default language %q is not loaded; available: %s", field, resolved, strings.Join(codes, ", "))
		}
		return fmt.Errorf("config.languages.%s: %q is not a loaded language code; available: %s", field, resolved, strings.Join(codes, ", "))
	}
	if err := check("cli", ls.CLI); err != nil {
		return err
	}
	if err := check("tui", ls.TUI); err != nil {
		return err
	}
	// agent_output is free-form by design; any value (including empty) is
	// accepted. Composer skips the directive line when empty.
	return nil
}
