package config

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"omakiten/internal/domain"
)

// LoadDomainEventRegistry hydrates domain.EventDefinitions and
// domain.EventDefByKey from the kit-loaded events block. The
// EventsSettings YAML tags (`defaults`, `definitions`) match the
// shape the domain loader expects, so we round-trip through
// yaml.Marshal — extra fields such as `default_recent_limit` and
// `overrides` are silently dropped by the domain decoder.
//
// Called once at boot from every runtime entry point (CLI/TUI via
// runtimeOptions.open and MCP via agentruntime.Open) right after
// LoadBundle succeeds, before services that consume the registry
// are constructed. Round-trip cost is negligible for boot and keeps
// the wiring footprint to a single helper.
//
// No-op when the events block carries no definitions — keeps
// minimal test fixtures (which omit the block) loadable without
// forcing every scenario to repeat the canonical 41-entry table.
func LoadDomainEventRegistry(events EventsSettings) error {
	if len(events.Definitions) == 0 {
		return nil
	}
	payload, err := yaml.Marshal(events)
	if err != nil {
		return fmt.Errorf("config: marshal events for domain registry: %w", err)
	}
	if err := domain.LoadEventRegistryFromYAML(payload); err != nil {
		return fmt.Errorf("config: hydrate domain event registry: %w", err)
	}
	return nil
}
