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

func ValidateBundle(bundle Bundle) error {
	if bundle.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if err := requireIDKeyName("kit", bundle.Kit.ID, bundle.Kit.Key, bundle.Kit.Name); err != nil {
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

	skillIDs := map[int]struct{}{}
	if err := validateItems("skills", bundle.Skills, func(skill Skill) (int, string, string) {
		return skill.ID, skill.Key, skill.Name
	}); err != nil {
		return err
	}
	for _, skill := range bundle.Skills {
		skillIDs[skill.ID] = struct{}{}
	}

	if err := validateItems("personas", bundle.Personas, func(persona Persona) (int, string, string) {
		return persona.ID, persona.Key, persona.Name
	}); err != nil {
		return err
	}
	for _, persona := range bundle.Personas {
		for _, skillID := range persona.SkillIDs {
			if _, ok := skillIDs[skillID]; !ok {
				return fmt.Errorf("personas.%s references missing skill id %d", persona.Key, skillID)
			}
		}
	}

	if err := validateItems("laws", bundle.Laws, func(law Law) (int, string, string) {
		return law.ID, law.Key, law.Body
	}); err != nil {
		return err
	}
	for _, law := range bundle.Laws {
		if _, ok := allowedSeverities[law.Severity]; !ok {
			return fmt.Errorf("laws.%s has invalid severity %q", law.Key, law.Severity)
		}
	}

	if err := validateWorkflows(bundle.Workflows, bundle.Config.Workflow.Active); err != nil {
		return err
	}

	return nil
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
