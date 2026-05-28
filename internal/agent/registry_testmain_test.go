package agent

import (
	"fmt"
	"os"
	"testing"

	"omakiten/internal/config"
)

// TestMain hydrates the domain event registry from the embedded omakase
// kit before any test in the agent package runs. Phase 1 of the YAML
// event registry refactor dropped the static EventCategoryOf switch, so
// helpers like logsRow (which call domain.EventCategoryOf and
// domain.SummarizeEvent) need the registry populated even for tests
// that do not go through testfixtures.LoadBundle.
func TestMain(m *testing.M) {
	if err := hydrateDomainEventRegistry(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func hydrateDomainEventRegistry() error {
	cfg, err := config.LoadKitConfigByKey("omakase")
	if err != nil {
		return fmt.Errorf("agent testmain: load omakase kit: %w", err)
	}
	if err := config.LoadDomainEventRegistry(cfg.Events); err != nil {
		return fmt.Errorf("agent testmain: hydrate event registry: %w", err)
	}
	return nil
}
