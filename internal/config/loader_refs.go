package config

import "fmt"

// assertRefsResolve checks every slug referenced by omakiten.yaml against the
// loaded entity sets and fails fast on a dangling ref.
func assertRefsResolve(w wiring, skills []Skill, laws []Law, personas []Persona, templates []TaskTemplate) error {
	skillSet := slugSet(loadedSkillSlugs(skills))
	lawSet := slugSet(loadedLawSlugs(laws))
	personaSet := slugSet(loadedPersonaSlugs(personas))
	templateSet := slugSet(loadedTemplateSlugs(templates))

	for _, slug := range w.Skills {
		if _, ok := skillSet[slug]; !ok {
			return fmt.Errorf("skills: ref %q has no matching file", slug)
		}
	}
	for _, slug := range w.Laws {
		if _, ok := lawSet[slug]; !ok {
			return fmt.Errorf("laws: ref %q has no matching file", slug)
		}
	}
	for _, slug := range w.Templates {
		if _, ok := templateSet[slug]; !ok {
			return fmt.Errorf("templates: ref %q has no matching file", slug)
		}
	}
	for _, persona := range w.Personas {
		if _, ok := personaSet[persona.Slug]; !ok {
			return fmt.Errorf("personas: ref %q has no matching file", persona.Slug)
		}
		for _, slug := range persona.Laws {
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("personas.%s laws: ref %q has no matching file", persona.Slug, slug)
			}
		}
		for _, slug := range persona.Skills {
			if _, ok := skillSet[slug]; !ok {
				return fmt.Errorf("personas.%s skills: ref %q has no matching file", persona.Slug, slug)
			}
		}
	}
	for _, project := range w.Projects {
		for _, slug := range project.Laws {
			if _, ok := lawSet[slug]; !ok {
				return fmt.Errorf("projects.%s laws: ref %q has no matching file", project.Slug, slug)
			}
		}
	}
	return nil
}
