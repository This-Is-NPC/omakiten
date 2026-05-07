package config

// pickSkills filters the on-disk skill set against the wiring's allowlist.
// When the wiring omits the `skills:` slot, every loaded skill is auto-included.
func pickSkills(loaded []Skill, refs []string) []Skill {
	if len(refs) == 0 {
		out := make([]Skill, len(loaded))
		copy(out, loaded)
		return out
	}
	bySlug := map[string]Skill{}
	for _, s := range loaded {
		bySlug[s.Slug] = s
	}
	out := make([]Skill, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		if s, ok := bySlug[ref]; ok {
			out = append(out, s)
		}
	}
	return out
}

// pickLaws stamps scope/owner metadata on loaded laws based on where each slug
// is referenced in the wiring file.
func pickLaws(loaded []Law, global []string, personas []PersonaWiring, projects []ProjectWiring) []Law {
	bySlug := map[string]Law{}
	for _, l := range loaded {
		bySlug[l.Slug] = l
	}

	if len(global) == 0 {
		referenced := map[string]struct{}{}
		for _, persona := range personas {
			for _, slug := range persona.Laws {
				referenced[slug] = struct{}{}
			}
		}
		for _, project := range projects {
			for _, slug := range project.Laws {
				referenced[slug] = struct{}{}
			}
		}
		for _, law := range loaded {
			if _, scoped := referenced[law.Slug]; scoped {
				continue
			}
			global = append(global, law.Slug)
		}
	}

	type scoped struct {
		scope, owner string
	}
	scope := map[string]scoped{}
	for _, slug := range global {
		if _, present := scope[slug]; !present {
			scope[slug] = scoped{scope: "global"}
		}
	}
	for _, persona := range personas {
		for _, slug := range persona.Laws {
			if _, present := scope[slug]; !present {
				scope[slug] = scoped{scope: "persona", owner: persona.Slug}
			}
		}
	}
	for _, project := range projects {
		for _, slug := range project.Laws {
			if _, present := scope[slug]; !present {
				scope[slug] = scoped{scope: "project", owner: project.Slug}
			}
		}
	}

	out := make([]Law, 0, len(scope))
	emit := func(slug, scopeName, owner string) {
		if l, ok := bySlug[slug]; ok {
			l.Scope = scopeName
			switch scopeName {
			case "project":
				l.ProjectSlug = owner
			case "persona":
				l.PersonaSlug = owner
			}
			out = append(out, l)
		}
	}
	emitted := map[string]struct{}{}
	for _, slug := range global {
		if _, dup := emitted[slug]; dup {
			continue
		}
		emitted[slug] = struct{}{}
		emit(slug, "global", "")
	}
	for _, persona := range personas {
		for _, slug := range persona.Laws {
			if _, dup := emitted[slug]; dup {
				continue
			}
			emitted[slug] = struct{}{}
			emit(slug, "persona", persona.Slug)
		}
	}
	for _, project := range projects {
		for _, slug := range project.Laws {
			if _, dup := emitted[slug]; dup {
				continue
			}
			emitted[slug] = struct{}{}
			emit(slug, "project", project.Slug)
		}
	}
	return out
}

// pickPersonas filters loaded personas and stamps each with declared skill/law
// wiring. Laws from the persona's frontmatter are preserved and merged (union,
// dedup, frontmatter first) with any laws declared in the wiring entry, so the
// authoring file and the wiring file can both contribute bindings. Skills only
// flow through wiring — personas/<slug>.md does not carry a skills list.
func pickPersonas(loaded []Persona, refs []PersonaWiring) []Persona {
	if len(refs) == 0 {
		out := make([]Persona, 0, len(loaded))
		for _, p := range loaded {
			p.Skills = nil
			out = append(out, p)
		}
		return out
	}
	bySlug := map[string]Persona{}
	for _, p := range loaded {
		bySlug[p.Slug] = p
	}
	out := make([]Persona, 0, len(refs))
	for _, ref := range refs {
		if p, ok := bySlug[ref.Slug]; ok {
			p.Skills = append([]string(nil), ref.Skills...)
			p.Laws = mergeLawSlugs(p.Laws, ref.Laws)
			out = append(out, p)
		}
	}
	return out
}

// mergeLawSlugs returns the union of two slug slices, preserving first-seen
// order. Used to merge frontmatter-declared bindings with wiring-declared
// bindings without duplicating slugs.
func mergeLawSlugs(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, slug := range a {
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	for _, slug := range b {
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pickTemplates filters the on-disk template set against the wiring's allowlist.
func pickTemplates(loaded []TaskTemplate, refs []string) []TaskTemplate {
	if len(refs) == 0 {
		out := make([]TaskTemplate, len(loaded))
		copy(out, loaded)
		return out
	}
	bySlug := map[string]TaskTemplate{}
	for _, t := range loaded {
		bySlug[t.Slug] = t
	}
	out := make([]TaskTemplate, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		if t, ok := bySlug[ref]; ok {
			out = append(out, t)
		}
	}
	return out
}

func pickProjects(refs []ProjectWiring) []Project {
	out := make([]Project, 0, len(refs))
	for _, ref := range refs {
		out = append(out, Project{
			Slug:        ref.Slug,
			Name:        ref.Name,
			Description: ref.Description,
			Laws:        append([]string(nil), ref.Laws...),
		})
	}
	return out
}
