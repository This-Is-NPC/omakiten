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
func ValidateBundle(bundle Bundle, loadedSkills []Skill, loadedLaws []Law, loadedPersonas []Persona) error {
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

	skillSet := slugSet(loadedSkillSlugs(loadedSkills))
	lawSet := slugSet(loadedLawSlugs(loadedLaws))
	personaSet := slugSet(loadedPersonaSlugs(loadedPersonas))

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

	return validateWorkflows(bundle.Workflows, bundle.Config.Workflow.Active)
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
