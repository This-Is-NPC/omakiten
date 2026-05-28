package domain

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"testing"
)

// eventRegistryFixture mirrors the 41-entry `definitions:` block + `defaults:`
// block every shipped kit YAML carries (defaults/config/<kit>.yaml::events).
// Embedded so the domain package's unit tests can hydrate EventDefinitions /
// EventDefByKey / KnownEventTypes without depending on the boot wiring or
// pulling in internal/config.
//
//go:embed testdata/event_registry_fixture.yaml
var eventRegistryFixture []byte

// fixtureKnownEventTypes caches the sorted key list the fixture installs.
// Captured once at TestMain so tests that mutate the registry (the loader
// tests) can restore the canonical state on cleanup without re-parsing
// the YAML each time.
var fixtureKnownEventTypes []string

// loadFixtureRegistry hydrates the package-level EventDefinitions /
// EventDefByKey / KnownEventTypes from the embedded fixture. Used by
// TestMain at startup and by loader-test cleanup hooks to restore the
// canonical state after a test installs a synthetic registry.
func loadFixtureRegistry() error {
	if err := LoadEventRegistryFromYAML(eventRegistryFixture); err != nil {
		return fmt.Errorf("domain testmain: load fixture: %w", err)
	}
	return nil
}

// TestMain bootstraps the YAML event registry from the embedded fixture
// before any test in the domain package runs. After Phase 1 the static
// `register()` + summarizers table is gone, so SummarizeEvent /
// EventCategoryOf / EventTypesForCategory / KnownEventTypes all return
// empty until the registry is populated — every domain test relies on
// this hook running first.
func TestMain(m *testing.M) {
	if err := loadFixtureRegistry(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cached := make([]string, len(KnownEventTypes))
	copy(cached, KnownEventTypes)
	sort.Strings(cached)
	fixtureKnownEventTypes = cached
	os.Exit(m.Run())
}
