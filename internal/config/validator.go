package config

import (
	"fmt"
	"strings"
)

var allowedSeverities = map[string]struct{}{
	"info":    {},
	"warning": {},
	"error":   {},
}

var allowedSortOrders = map[string]struct{}{
	"asc":  {},
	"desc": {},
}

var (
	allowedTaskSortFields  = []string{"id", "title", "priority", "created_at"}
	allowedGraphSortFields = []string{"id", "title"}
	allowedPriorities      = []string{"low", "normal", "high"}
	allowedLogsSources     = []string{"cli", "tui", "mcp"}
)

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
	if err := validateViewSettings(bundle.Config.Views, bundle.Workflows, bundle.Config.Workflow.Active); err != nil {
		return err
	}
	if err := validateMCPSettings(bundle.Config.MCP); err != nil {
		return err
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
	for _, law := range bundle.Laws {
		if _, ok := lawSet[law.Slug]; !ok {
			return fmt.Errorf("laws: ref %q has no matching file", law.Slug)
		}
		if _, ok := allowedSeverities[law.Severity]; !ok {
			return fmt.Errorf("laws.%s has invalid severity %q", law.Slug, law.Severity)
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

	return validateWorkflows(bundle.Workflows, bundle.Config.Workflow.Active)
}

// validateMCPCommands enforces that every persona/law/template slug referenced
// inside `mcp_commands` resolves to a loaded entity. Per-command entries may
// declare `laws:` (added) and `laws_disabled:` (removed from global); both
// must be slugs of loaded laws. The reserved `global` key only contributes
// laws — its persona/templates fields are tolerated but unused.
func validateMCPCommands(bundle Bundle, personaSet, lawSet, templateSet map[string]struct{}) error {
	for name, spec := range bundle.MCPCommands {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("mcp_commands: empty command name")
		}
		if name != MCPCommandsGlobalKey {
			if persona := strings.TrimSpace(spec.Persona); persona != "" {
				if _, ok := personaSet[persona]; !ok {
					return fmt.Errorf("mcp_commands.%s persona: ref %q has no matching persona file", name, persona)
				}
			}
			if err := validateSlugList("mcp_commands."+name+".templates", spec.Templates, templateSet, "template"); err != nil {
				return err
			}
		}
		if err := validateSlugList("mcp_commands."+name+".laws", spec.Laws, lawSet, "law"); err != nil {
			return err
		}
		if err := validateSlugList("mcp_commands."+name+".laws_disabled", spec.LawsDisabled, lawSet, "law"); err != nil {
			return err
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

// validateMCPSettings catches obviously-wrong tuning values at parse time.
// Non-positive `recent_comment_limit` and negative `max_comment_chars` are
// silently coerced to defaults via the Effective* accessors, but values that
// the user clearly intended (e.g. a typo'd negative number) surface here so
// the user gets a descriptive error instead of a silent fallback.
func validateMCPSettings(m MCPSettings) error {
	if m.RecentCommentLimit < 0 {
		return fmt.Errorf("config.mcp.recent_comment_limit cannot be negative")
	}
	if m.MaxCommentChars < 0 {
		return fmt.Errorf("config.mcp.max_comment_chars cannot be negative")
	}
	return nil
}

func validateSlugList(section string, slugs []string, set map[string]struct{}, kind string) error {
	seen := map[string]struct{}{}
	for _, slug := range slugs {
		if strings.TrimSpace(slug) == "" {
			return fmt.Errorf("%s: empty slug", section)
		}
		if _, dup := seen[slug]; dup {
			return fmt.Errorf("%s: duplicate %q", section, slug)
		}
		seen[slug] = struct{}{}
		if _, ok := set[slug]; !ok {
			return fmt.Errorf("%s: ref %q has no matching %s file", section, slug, kind)
		}
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

			for _, guard := range transition.Guards {
				switch guard.Type {
				case "blockers_in":
					if len(guard.Buckets) == 0 {
						return fmt.Errorf("workflows.%s guard blockers_in: buckets is required", workflow.Key)
					}
					for _, bKey := range guard.Buckets {
						if _, ok := bucketKeySet[bKey]; !ok {
							return fmt.Errorf("workflows.%s guard blockers_in: bucket key %q not found in workflow", workflow.Key, bKey)
						}
					}
				case "comments_min":
					if guard.Count < 1 {
						return fmt.Errorf("workflows.%s guard comments_min: count must be >= 1", workflow.Key)
					}
				case "comments_tagged":
					if strings.TrimSpace(guard.Tag) == "" {
						return fmt.Errorf("workflows.%s guard comments_tagged: tag is required", workflow.Key)
					}
					if guard.Count < 1 {
						return fmt.Errorf("workflows.%s guard comments_tagged: count must be >= 1", workflow.Key)
					}
				default:
					return fmt.Errorf("workflows.%s: unknown guard type %q", workflow.Key, guard.Type)
				}
			}
		}
	}

	if !activeFound {
		return fmt.Errorf("config.workflow.active %q does not match any workflow", activeKey)
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

// validateViewSettings enforces per-view sort/filter rules. Empty fields are
// fine — EffectiveViews fills them in with the canonical defaults — but any
// value the user does provide must be in the allowed set, otherwise the TUI
// would silently ignore typos and the user would never get feedback.
func validateViewSettings(v ViewSettings, workflows []Workflow, activeWorkflow string) error {
	if err := validateSort("config.views.board.sort", v.Board.Sort, allowedTaskSortFields, true); err != nil {
		return err
	}
	if err := validateStringSet("config.views.board.filter.priority", v.Board.Filter.Priority, allowedPriorities); err != nil {
		return err
	}

	if err := validateSort("config.views.table.sort", v.Table.Sort, allowedTaskSortFields, true); err != nil {
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

	if err := validateSort("config.views.graph.sort", v.Graph.Sort, allowedGraphSortFields, true); err != nil {
		return err
	}

	// Logs only carries an order — `field` is meaningless for time-series, so
	// we pass requireField=false to skip the field-allowed check.
	if err := validateSort("config.views.logs.sort", v.Logs.Sort, nil, false); err != nil {
		return err
	}
	if v.Logs.Limit < 0 {
		return fmt.Errorf("config.views.logs.limit cannot be negative")
	}
	if err := validateStringSet("config.views.logs.filter.source", v.Logs.Filter.Source, allowedLogsSources); err != nil {
		return err
	}

	if err := validateSort("config.views.task_activity.sort", v.TaskActivity.Sort, nil, false); err != nil {
		return err
	}

	return nil
}

func validateSort(section string, sort SortSettings, allowedFields []string, requireField bool) error {
	if sort.Field != "" && requireField {
		if !containsString(allowedFields, sort.Field) {
			return fmt.Errorf("%s.field %q is not one of %v", section, sort.Field, allowedFields)
		}
	}
	if sort.Field != "" && !requireField {
		return fmt.Errorf("%s.field is not configurable", section)
	}
	if sort.Order != "" {
		if _, ok := allowedSortOrders[sort.Order]; !ok {
			return fmt.Errorf("%s.order %q must be \"asc\" or \"desc\"", section, sort.Order)
		}
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
