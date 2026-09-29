// Package testfixtures loads focused scenarios with settings from the Omakase
// fixture. It also provides a local Git repository for preset installation tests
// so the suite needs neither network access nor published workflow repositories.
package testfixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// CanonicalRegistry builds an EnumRegistry from the embedded kit YAML's
// priority and severity tables. Used by tests that construct services
// directly without going through LoadBundle (which builds a registry
// from the merged bundle).
func CanonicalRegistry() *domain.EnumRegistry {
	kit := config.MustLoadKitConfig()
	priorityPairs := make([]domain.PriorityPair, len(kit.Priorities))
	for i, d := range kit.Priorities {
		priorityPairs[i] = domain.PriorityPair{ID: d.ID, Value: d.Value, Default: d.Default}
	}
	severityPairs := make([]domain.SeverityPair, len(kit.Severities))
	for i, d := range kit.Severities {
		severityPairs[i] = domain.SeverityPair{ID: d.ID, Value: d.Value, Default: d.Default}
	}
	return domain.NewEnumRegistry(priorityPairs, severityPairs)
}

// LoadBundle reads <package-dir>/testdata/<name>, parses strictly, and
// merges the embedded kit YAML (`defaults/omakiten.yaml`) for any
// canonical block the fixture omitted. Returns the merged bundle, an
// instance-scoped EnumRegistry, and auto-registers its enum tables into
// the domain registries. Failures terminate the test via t.Fatalf.
func LoadBundle(t testing.TB, name string) (config.Bundle, *domain.EnumRegistry) {
	t.Helper()
	if filepath.IsAbs(name) {
		return loadFromPath(t, name)
	}
	return loadFromPath(t, filepath.Join("testdata", name))
}

// LoadBundleFromAbsPath is the explicit-path variant for the rare test
// that wants to point at a fixture outside its own testdata/ dir (e.g.
// integration tests that load `defaults/omakiten.yaml` directly).
func LoadBundleFromAbsPath(t testing.TB, path string) (config.Bundle, *domain.EnumRegistry) {
	t.Helper()
	return loadFromPath(t, path)
}

func loadFromPath(t testing.TB, path string) (config.Bundle, *domain.EnumRegistry) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testfixtures: read %q: %v", path, err)
	}
	// Strict decoding so typos or removed-but-still-declared keys fail
	// loudly. Yaml:"-" fields (Skills/Personas/Laws/Templates/Projects
	// /Commands) are loaded by production from per-entity folders,
	// not from the wiring file — fixtures that need them wire inline.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var bundle config.Bundle
	if err := dec.Decode(&bundle); err != nil {
		t.Fatalf("testfixtures: parse %q: %v", path, err)
	}

	// Merge the kit YAML for any canonical block the fixture omitted.
	// Production has no in-code defaults; the installer materialises
	// the kit, and the validator rejects incomplete bundles. This step
	// simulates the install pipeline so test fixtures can stay focused
	// on the scenario under exercise without copying canonical boilerplate.
	mergeKitDefaults(&bundle)

	// Hydrate the domain event registry from the merged bundle so the
	// pure-domain helpers (EventCategoryOf, SummarizeEvent, KnownEventTypes)
	// behave as they do in production. Boot wires this through
	// config.LoadDomainEventRegistry; testfixtures mirrors the call here
	// because Phase 1 of the YAML-registry refactor dropped the static
	// switch fallback. Idempotent and process-global by design — every
	// fixture installs the same canonical 41-entry table.

	// Build the bundle-scoped EnumRegistry tests inject into services.
	// No process-global state involved.
	registry := config.BuildEnumRegistry(bundle)
	return bundle, registry
}

// mergeKitDefaults overlays the embedded kit's canonical blocks onto
// the bundle for any field the fixture didn't declare. This is the
// test-only equivalent of the install-time materialisation: production
// reads the user's complete YAML; tests use partial fixtures + this
// merge so each scenario file stays minimal.
func mergeKitDefaults(b *config.Bundle) {
	kit := config.MustLoadKitConfig()
	cfg := &b.Config
	mergeCollectionDefaults(cfg, kit)
	mergeagentDefaults(cfg, kit)
	mergeTUIDefaults(cfg, kit)
	mergeSQLiteDefaults(cfg, kit)
	mergeEventDefaults(cfg, kit)
	mergeServiceDefaults(cfg, kit)
	mergeViewSettings(&cfg.Views, kit.Views)

	if len(b.Surfaces) == 0 {
		b.Surfaces = config.CanonicalSurfaceTable()
	}
}

func mergeCollectionDefaults(cfg *config.Settings, kit config.Settings) {
	// Priorities / severities — fixture wins when present.
	if len(cfg.Priorities) == 0 {
		cfg.Priorities = append([]config.PriorityDefinition(nil), kit.Priorities...)
	}
	if len(cfg.Severities) == 0 {
		cfg.Severities = append([]config.SeverityDefinition(nil), kit.Severities...)
	}

	// Template defaults.
	if len(cfg.TemplateDefaults) == 0 {
		cfg.TemplateDefaults = append([]string(nil), kit.TemplateDefaults...)
	}
}

func mergeagentDefaults(cfg *config.Settings, kit config.Settings) {
	if cfg.Agent.RecentCommentLimit == 0 {
		cfg.Agent.RecentCommentLimit = kit.Agent.RecentCommentLimit
	}
	if cfg.Agent.NextWorkLimit == 0 {
		cfg.Agent.NextWorkLimit = kit.Agent.NextWorkLimit
	}
	if cfg.Agent.SimilarTaskLimit == 0 {
		cfg.Agent.SimilarTaskLimit = kit.Agent.SimilarTaskLimit
	}
	// MaxCommentChars: kit ships 0 as canonical (no truncation); the
	// only invalid value is negative, which the validator catches. So
	// we don't need to merge here unless the fixture explicitly set a
	// negative — leave alone.
	if cfg.Agent.IncludeWorkflowInContinue == nil {
		cfg.Agent.IncludeWorkflowInContinue = kit.Agent.IncludeWorkflowInContinue
	}
}

func mergeTUIDefaults(cfg *config.Settings, kit config.Settings) {
	if cfg.TUI.TokenBadge.YellowAt == 0 {
		cfg.TUI.TokenBadge.YellowAt = kit.TUI.TokenBadge.YellowAt
	}
	if cfg.TUI.TokenBadge.RedAt == 0 {
		cfg.TUI.TokenBadge.RedAt = kit.TUI.TokenBadge.RedAt
	}
}

func mergeSQLiteDefaults(cfg *config.Settings, kit config.Settings) {
	if cfg.SQLite.BusyTimeoutMs == 0 {
		cfg.SQLite.BusyTimeoutMs = kit.SQLite.BusyTimeoutMs
	}
	if cfg.SQLite.CacheSizeKB == 0 {
		cfg.SQLite.CacheSizeKB = kit.SQLite.CacheSizeKB
	}
	// MmapSizeBytes intentionally falls through: 0 is the valid
	// "disabled" sentinel.
}

func mergeEventDefaults(cfg *config.Settings, kit config.Settings) {
	config.NormalizeEventsRetention(cfg, kit)
	config.NormalizeEventsOrphanSweep(cfg, kit)
	mergeEventChannelDefaults(cfg, kit)
	mergeEventDefinitions(cfg, kit)
}

func mergeEventChannelDefaults(cfg *config.Settings, kit config.Settings) {
	if cfg.Events.DefaultRecentLimit == 0 {
		cfg.Events.DefaultRecentLimit = kit.Events.DefaultRecentLimit
	}
	if cfg.Events.Defaults.Log == nil {
		cfg.Events.Defaults.Log = kit.Events.Defaults.Log
	}
	if cfg.Events.Defaults.Broadcast == nil {
		cfg.Events.Defaults.Broadcast = kit.Events.Defaults.Broadcast
	}
	if cfg.Events.Defaults.LogVisible == nil {
		cfg.Events.Defaults.LogVisible = kit.Events.Defaults.LogVisible
	}
	if cfg.Events.Defaults.Metric == "" {
		cfg.Events.Defaults.Metric = kit.Events.Defaults.Metric
	}
	if cfg.Events.Defaults.EntityType == "" {
		cfg.Events.Defaults.EntityType = kit.Events.Defaults.EntityType
	}
	if cfg.Events.Defaults.Hook == nil {
		cfg.Events.Defaults.Hook = kit.Events.Defaults.Hook
	}
	if len(cfg.Events.Overrides) == 0 && len(kit.Events.Overrides) > 0 {
		cfg.Events.Overrides = make(map[string]config.EventChannelSettings, len(kit.Events.Overrides))
		for k, v := range kit.Events.Overrides {
			cfg.Events.Overrides[k] = v
		}
	}
}

func mergeEventDefinitions(cfg *config.Settings, kit config.Settings) {
	// Definitions inherit from the kit so validateEventsSettings (which
	// now resolves `overrides:` keys against the local definitions map)
	// accepts fixtures that omit the 41-entry block. Phase 1 of the YAML
	// event registry refactor relies on this kit-local set both at
	// LoadBundle time and inside ValidateHooks.
	//
	// Merge is key-level so a fixture that declares one override (e.g.
	// a custom Display for task.created) keeps its override AND inherits
	// the remaining 40 kit entries. The previous all-or-nothing swap
	// would have silently dropped the kit definitions whenever the
	// fixture's Definitions map was non-empty, leaving the fixture with
	// a single-entry registry the validator rejects.
	if len(kit.Events.Definitions) > 0 {
		if cfg.Events.Definitions == nil {
			cfg.Events.Definitions = make(map[string]config.EventDefinitionSettings, len(kit.Events.Definitions))
		}
		for k, v := range kit.Events.Definitions {
			if _, ok := cfg.Events.Definitions[k]; !ok {
				cfg.Events.Definitions[k] = v
			}
		}
	}
}

func mergeServiceDefaults(cfg *config.Settings, kit config.Settings) {
	if cfg.Solutions.DefaultTopLimit == 0 {
		cfg.Solutions.DefaultTopLimit = kit.Solutions.DefaultTopLimit
	}
	if cfg.Solutions.MaxTopLimit == 0 {
		cfg.Solutions.MaxTopLimit = kit.Solutions.MaxTopLimit
	}

	if len(cfg.Search.Stopwords) == 0 {
		cfg.Search.Stopwords = append([]string(nil), kit.Search.Stopwords...)
	}

	// Tag synonyms.
	if len(cfg.TagSynonyms) == 0 {
		cfg.TagSynonyms = make(map[string]string, len(kit.TagSynonyms))
		for k, v := range kit.TagSynonyms {
			cfg.TagSynonyms[k] = v
		}
	}
}

func mergeViewSettings(v *config.ViewSettings, kit config.ViewSettings) {
	if v.Board.Sort.Field == "" {
		v.Board.Sort.Field = kit.Board.Sort.Field
	}
	if v.Board.Sort.Order == "" {
		v.Board.Sort.Order = kit.Board.Sort.Order
	}
	if v.Table.Sort.Field == "" {
		v.Table.Sort.Field = kit.Table.Sort.Field
	}
	if v.Table.Sort.Order == "" {
		v.Table.Sort.Order = kit.Table.Sort.Order
	}
	if v.Graph.Sort.Field == "" {
		v.Graph.Sort.Field = kit.Graph.Sort.Field
	}
	if v.Graph.Sort.Order == "" {
		v.Graph.Sort.Order = kit.Graph.Sort.Order
	}
	if v.Logs.Sort.Order == "" {
		v.Logs.Sort.Order = kit.Logs.Sort.Order
	}
	if v.Logs.Limit == 0 {
		v.Logs.Limit = kit.Logs.Limit
	}
	if v.Logs.WindowDays == 0 {
		v.Logs.WindowDays = kit.Logs.WindowDays
	}
	if v.TaskActivity.Sort.Order == "" {
		v.TaskActivity.Sort.Order = kit.TaskActivity.Sort.Order
	}
}
