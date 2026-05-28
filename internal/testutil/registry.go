// Package testutil provides shared helpers for tests across packages.
//
// Phase 1 of the YAML event registry refactor removed the static
// EventCategoryOf switch in internal/domain, so every package that
// resolves event categories or summaries (agent, cli, sqlite, tui)
// needs the registry hydrated before tests run. HydrateDomainEventRegistry
// centralises the boot-time hydration so each package's TestMain can
// call a single helper instead of duplicating the load-and-hydrate
// dance.
//
// The package deliberately depends on internal/config (which depends
// on internal/domain). It therefore cannot be imported from
// internal/domain itself — the domain package owns its own embedded
// YAML fixture under testdata/ and bootstraps the registry directly
// via domain.LoadEventRegistryFromYAML.
package testutil

import (
	"fmt"

	"omakiten/internal/config"
)

// HydrateDomainEventRegistry loads the embedded "omakase" kit and
// hydrates the domain event registry (EventDefinitions, EventDefByKey,
// KnownEventTypes). Intended to be called from TestMain in packages
// whose tests resolve event categories / summaries via the domain
// registry without going through testfixtures.LoadBundle.
//
// Returns a wrapped error suitable for fmt.Fprintln + os.Exit(1) in
// TestMain. Callers should not invoke from inside individual test
// functions — the registry is package-level state, so repeated calls
// from concurrent tests would race.
func HydrateDomainEventRegistry() error {
	cfg, err := config.LoadKitConfigByKey("omakase")
	if err != nil {
		return fmt.Errorf("testutil: load omakase kit: %w", err)
	}
	if err := config.LoadDomainEventRegistry(cfg.Events); err != nil {
		return fmt.Errorf("testutil: hydrate event registry: %w", err)
	}
	return nil
}
