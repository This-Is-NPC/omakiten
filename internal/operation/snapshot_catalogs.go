package operation

import (
	"omakiten/internal/config"
	"omakiten/internal/contract"
)

// This file holds the projections that derive the agent-facing
// catalogs (skills, laws, personas, templates, commands) from the
// per-project *config.Snapshot the runtime installs via SetSnapshot.
// Each helper closes over the snapshot pointer at SetSnapshot time so
// the closures return the same data every call until the snapshot
// rotates — that mirrors the immutability contract Snapshot already
// guarantees.
//
// Snapshots arrive immutable; the closures defensively copy slices /
// maps before returning so callers that mutate the returned value
// cannot leak into other readers. The cost is one O(n) copy per
// agent catalog call (already O(n) before the migration via the bundle
// snapshots taken at runtime build time).

func snapshotTemplateCatalog(snap *config.Snapshot) TemplateCatalog {
	return func() []contract.TemplateSummary {
		templates := snap.Templates()
		out := make([]contract.TemplateSummary, 0, len(templates))
		for _, t := range templates {
			out = append(out, contract.TemplateSummary{
				Slug:        t.Slug,
				Name:        t.Name,
				Description: t.Description,
				Entity:      t.Entity,
				Default:     t.Default,
				Project:     t.ProjectSlug,
				Laws:        append([]string(nil), t.Laws...),
				IsCustom:    t.IsCustom,
				Body:        t.Body,
				SourcePath:  t.SourcePath,
			})
		}
		return out
	}
}

func snapshotTaskTemplateLookup(snap *config.Snapshot) TaskTemplateLookup {
	return func(projectSlug string) *contract.TaskTemplateSummary {
		t, ok := snap.ActiveDefault("task", projectSlug)
		if !ok {
			return nil
		}
		return &contract.TaskTemplateSummary{
			Slug:        t.Slug,
			Name:        t.Name,
			Description: t.Description,
			Body:        t.Body,
		}
	}
}

func snapshotSkillCatalog(snap *config.Snapshot) contract.SkillCatalog {
	return func() []contract.SkillInfo {
		skills := snap.Skills()
		out := make([]contract.SkillInfo, 0, len(skills))
		for _, s := range skills {
			out = append(out, contract.SkillInfo{
				Slug:        s.Slug,
				Name:        s.Name,
				Description: s.Description,
				Body:        s.Body,
			})
		}
		return out
	}
}

func snapshotLawCatalog(snap *config.Snapshot) contract.LawCatalog {
	return func() []contract.LawInfo {
		laws := snap.Laws()
		out := make([]contract.LawInfo, 0, len(laws))
		for _, l := range laws {
			out = append(out, contract.LawInfo{
				Slug:     l.Slug,
				Name:     l.Name,
				Severity: l.Severity,
				Body:     l.Body,
				Scope:    l.Scope,
				Project:  l.ProjectSlug,
				Persona:  l.PersonaSlug,
			})
		}
		return out
	}
}

func snapshotPersonaCatalog(snap *config.Snapshot) contract.PersonaCatalog {
	return func() []contract.PersonaInfo {
		personas := snap.Personas()
		out := make([]contract.PersonaInfo, 0, len(personas))
		for _, p := range personas {
			out = append(out, contract.PersonaInfo{
				Slug:            p.Slug,
				Name:            p.Name,
				Description:     p.Description,
				Body:            p.Body,
				SkillRepertoire: append([]string(nil), p.SkillRepertoire...),
				Laws:            append([]string(nil), p.Laws...),
			})
		}
		return out
	}
}

func snapshotCommandCatalog(snap *config.Snapshot) contract.CommandCatalog {
	return func() map[string]contract.CommandBinding {
		commands := snap.Commands()
		out := make(map[string]contract.CommandBinding, len(commands))
		for name, spec := range commands {
			out[name] = contract.CommandBinding{
				Persona:      spec.Persona,
				Laws:         append([]string(nil), spec.Laws...),
				LawsDisabled: append([]string(nil), spec.LawsDisabled...),
				Templates:    append([]string(nil), spec.Templates...),
				Skills:       append([]string(nil), spec.Skills...),
			}
		}
		return out
	}
}
