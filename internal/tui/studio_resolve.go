package tui

import (
	"omakiten/internal/config"
	"omakiten/internal/operation"
)

func resolveStudioCommand(bundle config.Bundle, name string) (string, error) {
	resp, err := operation.ResolveCommandFromCatalog(
		name,
		studioAgentCommands(bundle.MCPCommands),
		studioAgentPersonas(bundle.Personas),
		studioAgentSkills(bundle.Skills),
		studioAgentLaws(bundle.Laws),
		studioAgentTemplates(bundle.Templates),
		bundle.Config.EffectiveLanguages().AgentOutput,
	)
	if err != nil {
		return "", err
	}
	return resp.Markdown, nil
}

func studioAgentCommands(commands map[string]config.MCPCommandSpec) map[string]operation.MCPCommandBinding {
	out := make(map[string]operation.MCPCommandBinding, len(commands))
	for key, spec := range commands {
		out[key] = operation.MCPCommandBinding{Persona: spec.Persona, Laws: append([]string(nil), spec.Laws...), LawsDisabled: append([]string(nil), spec.LawsDisabled...), Templates: append([]string(nil), spec.Templates...), Skills: append([]string(nil), spec.Skills...)}
	}
	return out
}

func studioAgentPersonas(personas []config.Persona) map[string]operation.PersonaInfo {
	out := map[string]operation.PersonaInfo{}
	for _, p := range personas {
		out[p.Slug] = operation.PersonaInfo{Slug: p.Slug, Name: p.Name, Description: p.Description, Body: p.Body, SkillRepertoire: append([]string(nil), p.SkillRepertoire...), Laws: append([]string(nil), p.Laws...)}
	}
	return out
}

func studioAgentSkills(skills []config.Skill) map[string]operation.SkillInfo {
	out := map[string]operation.SkillInfo{}
	for _, s := range skills {
		out[s.Slug] = operation.SkillInfo{Slug: s.Slug, Name: s.Name, Description: s.Description, Body: s.Body}
	}
	return out
}

func studioAgentLaws(laws []config.Law) map[string]operation.LawInfo {
	out := map[string]operation.LawInfo{}
	for _, l := range laws {
		out[l.Slug] = operation.LawInfo{Slug: l.Slug, Name: l.Name, Severity: l.Severity, Body: l.Body, Scope: l.Scope}
	}
	return out
}

func studioAgentTemplates(templates []config.TaskTemplate) map[string]operation.TemplateInfo {
	out := map[string]operation.TemplateInfo{}
	for _, t := range templates {
		out[t.Slug] = operation.TemplateInfo{Slug: t.Slug, Name: t.Name, Description: t.Description, Default: t.Default, Project: t.ProjectSlug, Laws: append([]string(nil), t.Laws...), Body: t.Body}
	}
	return out
}
