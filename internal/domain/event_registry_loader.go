package domain

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// EventDef is the YAML-loaded metadata for one event_type. All fields except
// Key + Category + Formatter are optional and inherit from the YAML defaults
// block when omitted.
type EventDef struct {
	Key        string
	Category   EventCategory
	Display    string
	EntityType string
	Metric     string
	LogVisible bool
	Formatter  func(EventRow) string
}

// EventDefinitions is the populated registry. Empty until LoadEventRegistryFromYAML runs.
var EventDefinitions []EventDef

// EventDefByKey is the lookup map built alongside EventDefinitions.
var EventDefByKey = map[string]EventDef{}

// eventRegistryYAML is the on-disk shape parsed by the loader. Mirrors the
// config.events block extended in each kit YAML for Phase 0.
type eventRegistryYAML struct {
	Defaults    eventRegistryDefaults              `yaml:"defaults"`
	Definitions map[string]eventRegistryDefinition `yaml:"definitions"`
}

type eventRegistryDefaults struct {
	LogVisible bool   `yaml:"log_visible"`
	Metric     string `yaml:"metric"`
	EntityType string `yaml:"entity_type"`
}

type eventRegistryDefinition struct {
	Category   string  `yaml:"category"`
	Display    string  `yaml:"display"`
	EntityType *string `yaml:"entity_type,omitempty"`
	Metric     *string `yaml:"metric,omitempty"`
	LogVisible *bool   `yaml:"log_visible,omitempty"`
	Formatter  string  `yaml:"formatter"`
}

// LoadEventRegistryFromYAML parses a YAML byte payload (typically the
// config.events block of a kit YAML) and populates EventDefinitions +
// EventDefByKey. Per-entry fields override the defaults block; missing
// formatter ids in the global formatterRegistry cause a boot-time error.
// Repeated calls reset the registry (the loader owns the slice).
func LoadEventRegistryFromYAML(payload []byte) error {
	var parsed eventRegistryYAML
	if err := yaml.Unmarshal(payload, &parsed); err != nil {
		return fmt.Errorf("event_registry_loader: parse yaml: %w", err)
	}
	if len(parsed.Definitions) == 0 {
		return fmt.Errorf("event_registry_loader: definitions block is empty")
	}
	defs := make([]EventDef, 0, len(parsed.Definitions))
	byKey := make(map[string]EventDef, len(parsed.Definitions))
	keys := make([]string, 0, len(parsed.Definitions))
	for key, raw := range parsed.Definitions {
		if raw.Category == "" {
			return fmt.Errorf("event_registry_loader: %q missing category", key)
		}
		if raw.Formatter == "" {
			return fmt.Errorf("event_registry_loader: %q missing formatter id", key)
		}
		fn, ok := ResolveFormatter(raw.Formatter)
		if !ok {
			return fmt.Errorf("event_registry_loader: %q formatter id %q not registered", key, raw.Formatter)
		}
		def := EventDef{
			Key:        key,
			Category:   EventCategory(raw.Category),
			Display:    raw.Display,
			EntityType: pickString(raw.EntityType, parsed.Defaults.EntityType),
			Metric:     pickString(raw.Metric, parsed.Defaults.Metric),
			LogVisible: pickBool(raw.LogVisible, parsed.Defaults.LogVisible),
			Formatter:  fn,
		}
		defs = append(defs, def)
		byKey[key] = def
		keys = append(keys, key)
	}
	sort.Strings(keys)
	EventDefinitions = defs
	EventDefByKey = byKey
	KnownEventTypes = keys
	return nil
}

func pickString(override *string, fallback string) string {
	if override != nil {
		return *override
	}
	return fallback
}

func pickBool(override *bool, fallback bool) bool {
	if override != nil {
		return *override
	}
	return fallback
}
