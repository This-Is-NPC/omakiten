package config

// buildCatalog folds the full on-disk entity set (`loaded`) and the active
// picked subset into one catalog slice: every loaded entry is emitted, but
// entries present in `picked` are taken from the picked copy (so scope/skill
// wiring metadata the pick* step stamped survives) and flagged Active=true;
// the rest are emitted as loaded with Active=false. slugOf keys both sides;
// withActive returns a copy with the Active flag set. Order follows `loaded`.
func buildCatalog[T any](loaded, picked []T, slugOf func(T) string, withActive func(T, bool) T) []T {
	bySlug := make(map[string]T, len(picked))
	for _, p := range picked {
		bySlug[slugOf(p)] = p
	}
	out := make([]T, 0, len(loaded))
	for _, l := range loaded {
		if p, ok := bySlug[slugOf(l)]; ok {
			out = append(out, withActive(p, true))
			continue
		}
		out = append(out, withActive(l, false))
	}
	return out
}

// catalogSkills / catalogLaws / catalogPersonas / catalogTemplates bind
// buildCatalog to each entity type's Slug accessor and Active setter, so the
// loader call site reads as one named call per kind instead of repeating the
// slugOf/withActive closures inline.
func catalogSkills(loaded, picked []Skill) []Skill {
	return buildCatalog(loaded, picked,
		func(s Skill) string { return s.Slug },
		func(s Skill, a bool) Skill { s.Active = a; return s })
}

func catalogLaws(loaded, picked []Law) []Law {
	return buildCatalog(loaded, picked,
		func(l Law) string { return l.Slug },
		func(l Law, a bool) Law { l.Active = a; return l })
}

func catalogPersonas(loaded, picked []Persona) []Persona {
	return buildCatalog(loaded, picked,
		func(p Persona) string { return p.Slug },
		func(p Persona, a bool) Persona { p.Active = a; return p })
}

func catalogTemplates(loaded, picked []TaskTemplate) []TaskTemplate {
	return buildCatalog(loaded, picked,
		func(t TaskTemplate) string { return t.Slug },
		func(t TaskTemplate, a bool) TaskTemplate { t.Active = a; return t })
}

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
		global = inferGlobalLawSlugs(loaded, personas, projects)
	}

	scope := collectLawScopes(global, personas, projects)
	out := make([]Law, 0, len(scope))
	emitted := map[string]struct{}{}
	emitLaws(&out, emitted, bySlug, global, "global", "")
	for _, persona := range personas {
		emitLaws(&out, emitted, bySlug, persona.Laws, "persona", persona.Slug)
	}
	for _, project := range projects {
		emitLaws(&out, emitted, bySlug, project.Laws, "project", project.Slug)
	}
	return out
}

type lawScope struct {
	scope, owner string
}

func inferGlobalLawSlugs(loaded []Law, personas []PersonaWiring, projects []ProjectWiring) []string {
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
	global := make([]string, 0, len(loaded))
	for _, law := range loaded {
		if _, scoped := referenced[law.Slug]; !scoped {
			global = append(global, law.Slug)
		}
	}
	return global
}

func collectLawScopes(global []string, personas []PersonaWiring, projects []ProjectWiring) map[string]lawScope {
	scope := map[string]lawScope{}
	add := func(slugs []string, scopeName, owner string) {
		for _, slug := range slugs {
			if _, present := scope[slug]; !present {
				scope[slug] = lawScope{scope: scopeName, owner: owner}
			}
		}
	}
	add(global, "global", "")
	for _, persona := range personas {
		add(persona.Laws, "persona", persona.Slug)
	}
	for _, project := range projects {
		add(project.Laws, "project", project.Slug)
	}
	return scope
}

func emitLaws(out *[]Law, emitted map[string]struct{}, bySlug map[string]Law, slugs []string, scopeName, owner string) {
	for _, slug := range slugs {
		if _, dup := emitted[slug]; dup {
			continue
		}
		emitted[slug] = struct{}{}
		law, ok := bySlug[slug]
		if !ok {
			continue
		}
		law.Scope = scopeName
		switch scopeName {
		case "project":
			law.ProjectSlug = owner
		case "persona":
			law.PersonaSlug = owner
		}
		*out = append(*out, law)
	}
}

// pickPersonas filters loaded personas and stamps each with declared skill/law
// wiring. Laws from the persona's frontmatter are preserved and merged (union,
// dedup, frontmatter first) with any laws declared in the wiring entry, so the
// authoring file and the wiring file can both contribute bindings. The wiring
// entry is the source of truth for the persona skill repertoire.
func pickPersonas(loaded []Persona, refs []PersonaWiring) []Persona {
	if len(refs) == 0 {
		return append([]Persona(nil), loaded...)
	}
	bySlug := map[string]Persona{}
	for _, p := range loaded {
		bySlug[p.Slug] = p
	}
	out := make([]Persona, 0, len(refs))
	for _, ref := range refs {
		if p, ok := bySlug[ref.Slug]; ok {
			// schema_version + skill_repertoire on the wiring entry win
			// over the frontmatter-declared values so omakiten.yaml stays
			// the single source of truth for persona ⇄ skill wiring.
			if ref.SchemaVersion != 0 {
				p.SchemaVersion = ref.SchemaVersion
			}
			if ref.SkillRepertoire != nil {
				p.SkillRepertoire = append([]string(nil), ref.SkillRepertoire...)
			}
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
