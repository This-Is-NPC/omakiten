package config

import (
	"fmt"
)

// warnDanglingRefs scans every slug referenced by omakiten.yaml against the
// loaded entity sets and returns one SourceWarning per missing ref.
// Dangling refs are soft: the app keeps loading and the user gets the
// diagnostics in bundle.Warnings rather than a config_invalid error. The
// author of the config is responsible for keeping wiring and entity files
// in sync.
//
// Structural problems (missing required fields, type errors, empty names,
// duplicate-in-both-lists) remain hard errors elsewhere — soft validation
// only applies to "the slug you named is not loaded".
func warnDanglingRefs(w wiring, skills []Skill, laws []Law, personas []Persona, templates []TaskTemplate) []SourceWarning {
	skillSet := slugSet(loadedSkillSlugs(skills))
	lawSet := slugSet(loadedLawSlugs(laws))
	personaSet := slugSet(loadedPersonaSlugs(personas))
	templateSet := slugSet(loadedTemplateSlugs(templates))

	var warns []SourceWarning
	warns = appendMissingRefs(warns, "skills", w.Skills, skillSet)
	warns = appendMissingRefs(warns, "laws", w.Laws, lawSet)
	warns = appendMissingRefs(warns, "templates", w.Templates, templateSet)
	for _, persona := range w.Personas {
		warns = appendMissingRefs(warns, "personas", []string{persona.Slug}, personaSet)
		warns = appendMissingRefs(warns, fmt.Sprintf("personas.%s laws", persona.Slug), persona.Laws, lawSet)
		warns = appendMissingRefs(warns, fmt.Sprintf("personas.%s skill_repertoire", persona.Slug), persona.SkillRepertoire, skillSet)
	}
	for _, project := range w.Projects {
		warns = appendMissingRefs(warns, fmt.Sprintf("projects.%s laws", project.Slug), project.Laws, lawSet)
	}
	return warns
}

func appendMissingRefs(warns []SourceWarning, scope string, slugs []string, loaded map[string]struct{}) []SourceWarning {
	for _, slug := range slugs {
		if _, ok := loaded[slug]; !ok {
			warns = append(warns, SourceWarning{Slug: slug, Message: fmt.Sprintf("%s: ref %q has no matching file", scope, slug)})
		}
	}
	return warns
}
