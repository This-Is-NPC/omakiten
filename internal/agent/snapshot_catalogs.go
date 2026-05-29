package agent

import (
	"omakiten/internal/config"
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
// MCP catalog call (already O(n) before the migration via the bundle
// snapshots taken at runtime build time).

func snapshotTemplateCatalog(snap *config.Snapshot) TemplateCatalog {
	return func() []TemplateSummary {
		templates := snap.Templates()
		out := make([]TemplateSummary, 0, len(templates))
		for _, t := range templates {
			out = append(out, TemplateSummary{
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
	return func(projectSlug string) *TaskTemplateSummary {
		t, ok := snap.ActiveDefault("task", projectSlug)
		if !ok {
			return nil
		}
		return &TaskTemplateSummary{
			Slug:        t.Slug,
			Name:        t.Name,
			Description: t.Description,
			Body:        t.Body,
		}
	}
}

func snapshotSkillCatalog(snap *config.Snapshot) SkillCatalog {
	return func() []SkillInfo {
		skills := snap.Skills()
		out := make([]SkillInfo, 0, len(skills))
		for _, s := range skills {
			out = append(out, SkillInfo{
				Slug:        s.Slug,
				Name:        s.Name,
				Description: s.Description,
				Body:        s.Body,
			})
		}
		return out
	}
}

func snapshotLawCatalog(snap *config.Snapshot) LawCatalog {
	return func() []LawInfo {
		laws := snap.Laws()
		out := make([]LawInfo, 0, len(laws))
		for _, l := range laws {
			out = append(out, LawInfo{
				Slug:     l.Slug,
				Name:     l.Name,
				Severity: l.Severity,
				Body:     l.Body,
				Scope:    l.Scope,
			})
		}
		return out
	}
}

func snapshotPersonaCatalog(snap *config.Snapshot) PersonaCatalog {
	return func() []PersonaInfo {
		personas := snap.Personas()
		out := make([]PersonaInfo, 0, len(personas))
		for _, p := range personas {
			out = append(out, PersonaInfo{
				Slug:            p.Slug,
				Name:            p.Name,
				Description:     p.Description,
				Body:            p.Body,
				Skills:          append([]string(nil), p.Skills...),
				SkillRepertoire: append([]string(nil), p.SkillRepertoire...),
				Laws:            append([]string(nil), p.Laws...),
			})
		}
		return out
	}
}

func snapshotCommandCatalog(snap *config.Snapshot) CommandCatalog {
	return func() map[string]MCPCommandBinding {
		commands := snap.MCPCommands()
		out := make(map[string]MCPCommandBinding, len(commands))
		for name, spec := range commands {
			out[name] = MCPCommandBinding{
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
