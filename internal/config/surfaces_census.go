package config

// SurfaceKind classifies a census slug as product (user-facing) or
// wiring (composition injectors/getters). Scaffold and the default
// table derive cli/tui/mcp from this: product starts all-true, wiring
// starts all-false with DeniedWiringReason.
type SurfaceKind string

const (
	SurfaceKindProduct SurfaceKind = "product"
	SurfaceKindWiring  SurfaceKind = "wiring"

	// CanonicalSurfaceCount is the closed size of operation.Service.
	CanonicalSurfaceCount = 70

	// DeniedWiringReason is the default reason token for wiring rows.
	// Stored as an intl token; not resolved at load.
	DeniedWiringReason = "${{intl:operations.denied.wiring}}"
)

// SurfaceCensusEntry is one row of the canonical 70-slug census.
type SurfaceCensusEntry struct {
	Slug string
	Kind SurfaceKind
}

// CanonicalSurfaceCensus declares the supported product and wiring operations.
var CanonicalSurfaceCensus = []SurfaceCensusEntry{
	{Slug: "command.list", Kind: SurfaceKindProduct},
	{Slug: "command.resolve", Kind: SurfaceKindProduct},
	{Slug: "comment.add", Kind: SurfaceKindProduct},
	{Slug: "comment.delete", Kind: SurfaceKindProduct},
	{Slug: "comment.edit", Kind: SurfaceKindProduct},
	{Slug: "comment.list", Kind: SurfaceKindProduct},
	{Slug: "dependency.add", Kind: SurfaceKindProduct},
	{Slug: "dependency.list", Kind: SurfaceKindProduct},
	{Slug: "dependency.remove", Kind: SurfaceKindProduct},
	{Slug: "error.record", Kind: SurfaceKindProduct},
	{Slug: "insights.summary", Kind: SurfaceKindProduct},
	{Slug: "law.get", Kind: SurfaceKindProduct},
	{Slug: "law.list", Kind: SurfaceKindProduct},
	{Slug: "logs.list", Kind: SurfaceKindProduct},
	{Slug: "metrics.summary", Kind: SurfaceKindProduct},
	{Slug: "orphans.migrate", Kind: SurfaceKindProduct},
	{Slug: "persona.get", Kind: SurfaceKindProduct},
	{Slug: "persona.list", Kind: SurfaceKindProduct},
	{Slug: "plan.continue", Kind: SurfaceKindProduct},
	{Slug: "plan.create", Kind: SurfaceKindProduct},
	{Slug: "plan.delete", Kind: SurfaceKindProduct},
	{Slug: "plan.edit", Kind: SurfaceKindProduct},
	{Slug: "plan.list", Kind: SurfaceKindProduct},
	{Slug: "plan.show", Kind: SurfaceKindProduct},
	{Slug: "plan.task.assign", Kind: SurfaceKindProduct},
	{Slug: "plan.task.claim_next", Kind: SurfaceKindProduct},
	{Slug: "plan.task.unassign", Kind: SurfaceKindProduct},
	{Slug: "plan.wave.add", Kind: SurfaceKindProduct},
	{Slug: "plan.wave.remove", Kind: SurfaceKindProduct},
	{Slug: "plan.wave.rename", Kind: SurfaceKindProduct},
	{Slug: "plan.wave.reorder", Kind: SurfaceKindProduct},
	{Slug: "progress.record", Kind: SurfaceKindProduct},
	{Slug: "project.edit", Kind: SurfaceKindProduct},
	{Slug: "project.overview", Kind: SurfaceKindProduct},
	{Slug: "project.resume", Kind: SurfaceKindProduct},
	{Slug: "search", Kind: SurfaceKindProduct},
	{Slug: "skill.get", Kind: SurfaceKindProduct},
	{Slug: "skill.list", Kind: SurfaceKindProduct},
	{Slug: "solution.add", Kind: SurfaceKindProduct},
	{Slug: "solution.confirm", Kind: SurfaceKindProduct},
	{Slug: "solution.list_top", Kind: SurfaceKindProduct},
	{Slug: "tag.add", Kind: SurfaceKindProduct},
	{Slug: "tag.list", Kind: SurfaceKindProduct},
	{Slug: "tag.list_all", Kind: SurfaceKindProduct},
	{Slug: "tag.merge", Kind: SurfaceKindProduct},
	{Slug: "tag.remove", Kind: SurfaceKindProduct},
	{Slug: "task.archive", Kind: SurfaceKindProduct},
	{Slug: "task.assign", Kind: SurfaceKindProduct},
	{Slug: "task.continue", Kind: SurfaceKindProduct},
	{Slug: "task.create", Kind: SurfaceKindProduct},
	{Slug: "task.create_intent", Kind: SurfaceKindProduct},
	{Slug: "task.delete", Kind: SurfaceKindProduct},
	{Slug: "task.edit", Kind: SurfaceKindProduct},
	{Slug: "task.list", Kind: SurfaceKindProduct},
	{Slug: "task.transition", Kind: SurfaceKindProduct},
	{Slug: "task.unarchive", Kind: SurfaceKindProduct},
	{Slug: "task_activity.list", Kind: SurfaceKindProduct},
	{Slug: "template.list", Kind: SurfaceKindProduct},
	{Slug: "template.show", Kind: SurfaceKindProduct},
	{Slug: "workflow.show", Kind: SurfaceKindProduct},
	{Slug: "wiring.command_description", Kind: SurfaceKindWiring},
	{Slug: "wiring.selector", Kind: SurfaceKindWiring},
	{Slug: "wiring.set_now", Kind: SurfaceKindWiring},
	{Slug: "wiring.set_orphan_service", Kind: SurfaceKindWiring},
	{Slug: "wiring.set_project_selector", Kind: SurfaceKindWiring},
	{Slug: "wiring.set_settings", Kind: SurfaceKindWiring},
	{Slug: "wiring.set_snapshot", Kind: SurfaceKindWiring},
	{Slug: "wiring.settings_cache_prompts", Kind: SurfaceKindWiring},
	{Slug: "wiring.snapshot", Kind: SurfaceKindWiring},
	{Slug: "wiring.synonyms", Kind: SurfaceKindWiring},
}

// CanonicalSurfaceTable returns the shipped default 70-row table:
// product all-true, wiring all-false with DeniedWiringReason. Each call
// allocates a fresh map and fresh bool pointers so tests can mutate a
// row without aliasing the rest of the table.
func CanonicalSurfaceTable() SurfaceTable {
	table := make(SurfaceTable, len(CanonicalSurfaceCensus))
	for _, e := range CanonicalSurfaceCensus {
		if e.Kind == SurfaceKindWiring {
			table[e.Slug] = SurfacePolicy{
				CLI:    surfaceBool(false),
				TUI:    surfaceBool(false),
				MCP:    surfaceBool(false),
				Reason: DeniedWiringReason,
			}
			continue
		}
		table[e.Slug] = SurfacePolicy{
			CLI: surfaceBool(true),
			TUI: surfaceBool(true),
			MCP: surfaceBool(true),
		}
	}
	return table
}

func surfaceBool(v bool) *bool { return &v }

func canonicalSurfaceSlugSet() map[string]struct{} {
	out := make(map[string]struct{}, len(CanonicalSurfaceCensus))
	for _, e := range CanonicalSurfaceCensus {
		out[e.Slug] = struct{}{}
	}
	return out
}
