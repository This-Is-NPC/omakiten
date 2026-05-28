package sqlite

import (
	"fmt"
	"os"
	"testing"

	"omakiten/internal/config"
)

// TestMain hydrates the domain event registry from the embedded omakase
// kit before any test in the sqlite package runs. Phase 1 of the YAML
// event registry refactor dropped the static EventCategoryOf switch, so
// repository helpers (list_events, event_category_counts) that resolve
// EventFilter.Categories through domain.EventTypesForCategory need the
// registry populated even for tests that bypass testfixtures.LoadBundle.
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
		return fmt.Errorf("sqlite testmain: load omakase kit: %w", err)
	}
	if err := config.LoadDomainEventRegistry(cfg.Events); err != nil {
		return fmt.Errorf("sqlite testmain: hydrate event registry: %w", err)
	}
	return nil
}
