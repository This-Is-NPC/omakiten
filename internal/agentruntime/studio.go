package agentruntime

import (
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/operation"
)

func ResolveCommandPreview(bundle config.Bundle, name string) (string, error) {
	resp, err := operation.ResolveCommandFromCatalog(
		name,
		studioAgentCommands(bundle.Commands),
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

func studioAgentCommands(commands map[string]config.CommandSpec) map[string]contract.CommandBinding {
	out := make(map[string]contract.CommandBinding, len(commands))
	for key, spec := range commands {
		out[key] = contract.CommandBinding{Persona: spec.Persona, Laws: append([]string(nil), spec.Laws...), LawsDisabled: append([]string(nil), spec.LawsDisabled...), Templates: append([]string(nil), spec.Templates...), Skills: append([]string(nil), spec.Skills...)}
	}
	return out
}

func studioAgentPersonas(personas []config.Persona) map[string]contract.PersonaInfo {
	out := map[string]contract.PersonaInfo{}
	for _, p := range personas {
		out[p.Slug] = contract.PersonaInfo{Slug: p.Slug, Name: p.Name, Description: p.Description, Body: p.Body, SkillRepertoire: append([]string(nil), p.SkillRepertoire...), Laws: append([]string(nil), p.Laws...)}
	}
	return out
}

func studioAgentSkills(skills []config.Skill) map[string]contract.SkillInfo {
	out := map[string]contract.SkillInfo{}
	for _, s := range skills {
		out[s.Slug] = contract.SkillInfo{Slug: s.Slug, Name: s.Name, Description: s.Description, Body: s.Body}
	}
	return out
}

func studioAgentLaws(laws []config.Law) map[string]contract.LawInfo {
	out := map[string]contract.LawInfo{}
	for _, l := range laws {
		out[l.Slug] = contract.LawInfo{Slug: l.Slug, Name: l.Name, Severity: l.Severity, Body: l.Body, Scope: l.Scope}
	}
	return out
}

func studioAgentTemplates(templates []config.TaskTemplate) map[string]contract.TemplateInfo {
	out := map[string]contract.TemplateInfo{}
	for _, t := range templates {
		out[t.Slug] = contract.TemplateInfo{Slug: t.Slug, Name: t.Name, Description: t.Description, Default: t.Default, Project: t.ProjectSlug, Laws: append([]string(nil), t.Laws...), Body: t.Body}
	}
	return out
}
