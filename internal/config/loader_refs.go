package config

import "fmt"

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
	missingRef := func(scope, slug string) {
		warns = append(warns, SourceWarning{Slug: slug, Message: fmt.Sprintf("%s: ref %q has no matching file", scope, slug)})
	}

	for _, slug := range w.Skills {
		if _, ok := skillSet[slug]; !ok {
			missingRef("skills", slug)
		}
	}
	for _, slug := range w.Laws {
		if _, ok := lawSet[slug]; !ok {
			missingRef("laws", slug)
		}
	}
	for _, slug := range w.Templates {
		if _, ok := templateSet[slug]; !ok {
			missingRef("templates", slug)
		}
	}
	for _, persona := range w.Personas {
		if _, ok := personaSet[persona.Slug]; !ok {
			missingRef("personas", persona.Slug)
		}
		for _, slug := range persona.Laws {
			if _, ok := lawSet[slug]; !ok {
				missingRef(fmt.Sprintf("personas.%s laws", persona.Slug), slug)
			}
		}
		for _, slug := range persona.Skills {
			if _, ok := skillSet[slug]; !ok {
				missingRef(fmt.Sprintf("personas.%s skills", persona.Slug), slug)
			}
		}
	}
	for _, project := range w.Projects {
		for _, slug := range project.Laws {
			if _, ok := lawSet[slug]; !ok {
				missingRef(fmt.Sprintf("projects.%s laws", project.Slug), slug)
			}
		}
	}
	return warns
}
