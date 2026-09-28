package operation

import (
	"fmt"

	"omakiten/internal/config"
)

// Surface is the construction-time identity of a facade consumer.
// It is never taken from input — a caller cannot claim to be CLI
// while speaking MCP. Zero value means unrestricted, which keeps
// tests that construct NewService directly green.
type Surface string

const (
	SurfaceCLI Surface = "cli"
	SurfaceTUI Surface = "tui"
	SurfaceMCP Surface = "mcp"
)

// OperationDenied is returned when the surfaces table turns the
// caller's construction-time surface off for this census slug.
type OperationDenied struct {
	Surface Surface
	Op      string
	Reason  string
}

func (e OperationDenied) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("operation %q denied on %s: %s", e.Op, e.Surface, e.Reason)
	}
	return fmt.Sprintf("operation %q denied on %s", e.Op, e.Surface)
}

// ForCLI returns a shallow copy pinned to the CLI surface. The
// original Service is left with a zero surface so a shared
// ProjectRuntime.Service is never mutated.
func (s *Service) ForCLI() *Service {
	if s == nil {
		return nil
	}
	c := *s
	c.surface = SurfaceCLI
	return &c
}

// ForTUI returns a shallow copy pinned to the TUI surface.
func (s *Service) ForTUI() *Service {
	if s == nil {
		return nil
	}
	c := *s
	c.surface = SurfaceTUI
	return &c
}

// ForMCP returns a shallow copy pinned to the MCP surface.
func (s *Service) ForMCP() *Service {
	if s == nil {
		return nil
	}
	c := *s
	c.surface = SurfaceMCP
	return &c
}

// allow is the first line of every product method. Wiring methods and
// TUI-shaped extras that are not in the 70-slug census must not call it.
// Zero surface, a nil snapshot, or an empty table are unrestricted.
func (s *Service) allow(op string) error {
	if s == nil || s.surface == "" {
		return nil
	}
	snap := s.Snapshot()
	if snap == nil {
		return nil
	}
	table := snap.Surfaces()
	if len(table) == 0 {
		return nil
	}
	row, ok := table[op]
	if !ok {
		return OperationDenied{Surface: s.surface, Op: op}
	}
	enabled := surfaceFlag(row, s.surface)
	if enabled == nil || !*enabled {
		return OperationDenied{Surface: s.surface, Op: op, Reason: row.Reason}
	}
	return nil
}

func surfaceFlag(row config.SurfacePolicy, surface Surface) *bool {
	switch surface {
	case SurfaceCLI:
		return row.CLI
	case SurfaceTUI:
		return row.TUI
	case SurfaceMCP:
		return row.MCP
	default:
		return nil
	}
}

// CensusSlugForMCPTool maps an MCP registry tool name (tasks.move) to
// the canonical surfaces: census slug (task.transition). Wave 4.4 uses
// this to omit denied tools; 4.3 CallTool already fails via the
// method-level allow() gate.
var CensusSlugForMCPTool = map[string]string{
	"commands.list":       "command.list",
	"commands.resolve":    "command.resolve",
	"comments.add":        "comment.add",
	"comments.delete":     "comment.delete",
	"comments.edit":       "comment.edit",
	"comments.list":       "comment.list",
	"dependencies.add":    "dependency.add",
	"dependencies.list":   "dependency.list",
	"dependencies.remove": "dependency.remove",
	"errors.record":       "error.record",
	"insights.summary":    "insights.summary",
	"laws.get":            "law.get",
	"laws.list":           "law.list",
	"logs.list":           "logs.list",
	"metrics.summary":     "metrics.summary",
	"orphans.migrate":     "orphans.migrate",
	"personas.get":        "persona.get",
	"personas.list":       "persona.list",
	"plans.continue":      "plan.continue",
	"plans.create":        "plan.create",
	"plans.delete":        "plan.delete",
	"plans.edit":          "plan.edit",
	"plans.list":          "plan.list",
	"plans.show":          "plan.show",
	"plans.assign_task":   "plan.task.assign",
	"plans.claim_next":    "plan.task.claim_next",
	"plans.unassign":      "plan.task.unassign",
	"plans.add_wave":      "plan.wave.add",
	"plans.remove_wave":   "plan.wave.remove",
	"plans.rename_wave":   "plan.wave.rename",
	"plans.reorder_wave":  "plan.wave.reorder",
	"progress.record":     "progress.record",
	"project.edit":        "project.edit",
	"project.overview":    "project.overview",
	"project.resume":      "project.resume",
	"search":              "search",
	"skills.get":          "skill.get",
	"skills.list":         "skill.list",
	"solutions.add":       "solution.add",
	"solutions.confirm":   "solution.confirm",
	"solutions.list_top":  "solution.list_top",
	"tags.add":            "tag.add",
	"tags.list":           "tag.list",
	"tags.list_all":       "tag.list_all",
	"tags.merge":          "tag.merge",
	"tags.remove":         "tag.remove",
	"tasks.archive":       "task.archive",
	"tasks.continue":      "task.continue",
	"tasks.create":        "task.create",
	"tasks.create_intent": "task.create_intent",
	"tasks.delete":        "task.delete",
	"tasks.edit":          "task.edit",
	"tasks.list":          "task.list",
	"tasks.move":          "task.transition",
	"tasks.unarchive":     "task.unarchive",
	"task_activity.list":  "task_activity.list",
	"templates.list":      "template.list",
	"templates.show":      "template.show",
	"workflow.show":       "workflow.show",
}
