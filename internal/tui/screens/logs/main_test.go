package logs

import (
	"fmt"
	"os"
	"testing"

	"omakiten/internal/tui/screens/screentest"
)

// TestMain hydrates the domain event registry from the embedded omakase kit
// before any test in this package runs: the renderer calls
// domain.EventCategoryOf and domain.SummarizeEvent, both of which read the
// YAML-loaded registry.
func TestMain(m *testing.M) {
	if err := screentest.HydrateEventRegistry(); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("logs testmain: %w", err))
		os.Exit(1)
	}
	os.Exit(m.Run())
}
