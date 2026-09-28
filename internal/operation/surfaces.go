package operation

import (
	"fmt"

	"omakiten/internal/config"
)

// Surface is the construction-time identity of a facade consumer.
// It is never taken from input — a caller cannot claim to be CLI
// while speaking agent. Zero value means unrestricted, which keeps
// tests that construct NewService directly green.
type Surface string

const (
	SurfaceCLI Surface = "cli"
	SurfaceTUI Surface = "tui"
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

// allow is the first line of every product method. Wiring methods and
// TUI-shaped extras that are not in the 74-slug census must not call it.
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
	default:
		return nil
	}
}
