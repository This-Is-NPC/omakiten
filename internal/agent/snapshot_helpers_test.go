package agent

import (
	"testing"

	"omakiten/internal/config"
)

// agentBundleWithTemplates returns the canonical agent test bundle
// with its Templates slice replaced by the supplied list. The helper
// preserves the workflow / kit / mcp settings the fixture seeds via
// agentTestBundle so the resulting snapshot still answers
// Workflow() / Settings() correctly — replacing only the catalog the
// test cares about.
func agentBundleWithTemplates(t *testing.T, templates []TemplateSummary) config.Bundle {
	t.Helper()
	bundle := agentTestBundle(t)
	bundle.Templates = nil
	for _, tpl := range templates {
		bundle.Templates = append(bundle.Templates, config.TaskTemplate{
			Slug:        tpl.Slug,
			Name:        tpl.Name,
			Description: tpl.Description,
			Entity:      tpl.Entity,
			Default:     tpl.Default,
			ProjectSlug: tpl.Project,
			Laws:        append([]string(nil), tpl.Laws...),
			Body:        tpl.Body,
			SourcePath:  tpl.SourcePath,
			IsCustom:    tpl.IsCustom,
		})
	}
	return bundle
}

// snapshotWithTemplates builds a per-project Snapshot whose Templates()
// catalog mirrors the supplied TemplateSummary slice while preserving
// the rest of the canonical agent test bundle.
func snapshotWithTemplates(t *testing.T, templates []TemplateSummary) *config.Snapshot {
	t.Helper()
	return config.BuildSnapshot(agentBundleWithTemplates(t, templates))
}

// snapshotWithTaskTemplate builds a snapshot whose active task-default
// resolves to the supplied scaffold, preserving the rest of the
// canonical agent test bundle.
func snapshotWithTaskTemplate(t *testing.T, projectSlug string, scaffold TaskTemplateSummary) *config.Snapshot {
	t.Helper()
	bundle := agentTestBundle(t)
	bundle.Templates = []config.TaskTemplate{{
		Slug:        scaffold.Slug,
		Name:        scaffold.Name,
		Description: scaffold.Description,
		Default:     "task",
		ProjectSlug: projectSlug,
		Body:        scaffold.Body,
	}}
	return config.BuildSnapshot(bundle)
}

// snapshotWithEntities builds a snapshot whose Skills(), Laws(),
// Personas(), MCPCommands() reflect the supplied inline values. Empty
// slices/maps are tolerated and produce an empty catalog of that
// kind. Used by tests that drove the per-field SetXCatalog setters
// independently.
func snapshotWithEntities(
	t *testing.T,
	skills []SkillInfo,
	laws []LawInfo,
	personas []PersonaInfo,
	templates []TemplateSummary,
	commands map[string]MCPCommandBinding,
) *config.Snapshot {
	t.Helper()
	bundle := config.Bundle{}
	for _, s := range skills {
		bundle.Skills = append(bundle.Skills, config.Skill{
			Slug:        s.Slug,
			Name:        s.Name,
			Description: s.Description,
			Body:        s.Body,
		})
	}
	for _, l := range laws {
		bundle.Laws = append(bundle.Laws, config.Law{
			Slug:     l.Slug,
			Name:     l.Name,
			Severity: l.Severity,
			Body:     l.Body,
			Scope:    l.Scope,
		})
	}
	for _, p := range personas {
		bundle.Personas = append(bundle.Personas, config.Persona{
			Slug:        p.Slug,
			Name:        p.Name,
			Description: p.Description,
			Body:        p.Body,
			Skills:      append([]string(nil), p.Skills...),
			Laws:        append([]string(nil), p.Laws...),
		})
	}
	for _, tpl := range templates {
		bundle.Templates = append(bundle.Templates, config.TaskTemplate{
			Slug:        tpl.Slug,
			Name:        tpl.Name,
			Description: tpl.Description,
			Entity:      tpl.Entity,
			Default:     tpl.Default,
			ProjectSlug: tpl.Project,
			Laws:        append([]string(nil), tpl.Laws...),
			Body:        tpl.Body,
			SourcePath:  tpl.SourcePath,
			IsCustom:    tpl.IsCustom,
		})
	}
	if len(commands) > 0 {
		bundle.MCPCommands = make(map[string]config.MCPCommandSpec, len(commands))
		for name, binding := range commands {
			bundle.MCPCommands[name] = config.MCPCommandSpec{
				Persona:      binding.Persona,
				Laws:         append([]string(nil), binding.Laws...),
				LawsDisabled: append([]string(nil), binding.LawsDisabled...),
				Templates:    append([]string(nil), binding.Templates...),
			}
		}
	}
	return config.BuildSnapshot(bundle)
}
