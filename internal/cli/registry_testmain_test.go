package cli

import (
	"fmt"
	"os"
	"testing"

	"omakiten/internal/testutil"
)

// TestMain hydrates the domain event registry from the embedded omakase
// kit before any test in the cli package runs. Phase 1 of the YAML
// event registry refactor dropped the static EventCategoryOf switch, so
// CLI command bodies (e.g. logs projection in cli/logs.go) that call
// domain.EventCategoryOf and domain.SummarizeEvent need the registry
// populated even for tests that bypass testfixtures.LoadBundle.
//
// The hydration body lives in internal/testutil so the agent, cli,
// sqlite, and tui packages share a single source of truth.
func TestMain(m *testing.M) {
	if err := testutil.HydrateDomainEventRegistry(); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain: %w", err))
		os.Exit(1)
	}
	os.Exit(m.Run())
}
