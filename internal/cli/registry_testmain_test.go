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
	testHome, err := os.MkdirTemp("", "omakiten-cli-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain temp home: %w", err))
		os.Exit(1)
	}
	// Keep t.TempDir roots below an isolated home so repo-local discovery
	// cannot walk into an external /tmp/.omakiten install.
	if err := os.Setenv("HOME", testHome); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain HOME: %w", err))
		os.Exit(1)
	}
	if err := os.Setenv("TMPDIR", testHome); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain TMPDIR: %w", err))
		os.Exit(1)
	}
	if err := testutil.HydrateDomainEventRegistry(); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("cli testmain: %w", err))
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(testHome)
	os.Exit(code)
}
