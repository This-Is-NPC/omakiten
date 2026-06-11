// Package testfixtures wires test packages to the YAML config files that
// live next to them under testdata/. Each fixture is a partial scenario;
// the helper merges the embedded kit YAML (`defaults/omakiten.yaml`) on
// top so missing canonical blocks (priorities, severities, mcp, views,
// tui, template_defaults) inherit from the shipped kit. This mirrors
// the production install pipeline where the kit YAML is materialised
// into the user's config root on first run.
//
// Why this matters: production has NO in-code canonical defaults. The
// validator rejects bundles that omit required fields. Without the
// merge step here, every fixture would have to repeat ~50 lines of
// canonical boilerplate; with it, fixtures stay focused on the
// scenario they exercise.
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
	// /MCPCommands) are loaded by production from per-entity folders,
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
	if err := config.LoadDomainEventRegistry(bundle.Config.Events); err != nil {
		t.Fatalf("testfixtures: hydrate domain event registry: %v", err)
	}

	// Build the bundle-scoped EnumRegistry tests inject into services.
	// No process-global state involved.
	registry := registryFromBundle(bundle)
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

	// MCP block — fill missing scalar fields and pointer-bools.
	if cfg.MCP.RecentCommentLimit == 0 {
		cfg.MCP.RecentCommentLimit = kit.MCP.RecentCommentLimit
	}
	if cfg.MCP.NextWorkLimit == 0 {
		cfg.MCP.NextWorkLimit = kit.MCP.NextWorkLimit
	}
	if cfg.MCP.SimilarTaskLimit == 0 {
		cfg.MCP.SimilarTaskLimit = kit.MCP.SimilarTaskLimit
	}
	// MaxCommentChars: kit ships 0 as canonical (no truncation); the
	// only invalid value is negative, which the validator catches. So
	// we don't need to merge here unless the fixture explicitly set a
	// negative — leave alone.
	if cfg.MCP.IncludeWorkflowInContinue == nil {
		cfg.MCP.IncludeWorkflowInContinue = kit.MCP.IncludeWorkflowInContinue
	}
	if cfg.MCP.CachePrompts == nil {
		cfg.MCP.CachePrompts = kit.MCP.CachePrompts
	}

	// TUI badge thresholds.
	if cfg.TUI.TokenBadge.YellowAt == 0 {
		cfg.TUI.TokenBadge.YellowAt = kit.TUI.TokenBadge.YellowAt
	}
	if cfg.TUI.TokenBadge.RedAt == 0 {
		cfg.TUI.TokenBadge.RedAt = kit.TUI.TokenBadge.RedAt
	}

	// SQLite knobs.
	if cfg.SQLite.BusyTimeoutMs == 0 {
		cfg.SQLite.BusyTimeoutMs = kit.SQLite.BusyTimeoutMs
	}
	if cfg.SQLite.CacheSizeKB == 0 {
		cfg.SQLite.CacheSizeKB = kit.SQLite.CacheSizeKB
	}
	// MmapSizeBytes intentionally falls through: 0 is the valid
	// "disabled" sentinel.

	// Events retention inherits kit defaults when the fixture omits the
	// block (validator-required on full bundles).
	config.NormalizeEventsRetention(cfg, kit)

	// Solutions limits.
	if cfg.Solutions.DefaultTopLimit == 0 {
		cfg.Solutions.DefaultTopLimit = kit.Solutions.DefaultTopLimit
	}
	if cfg.Solutions.MaxTopLimit == 0 {
		cfg.Solutions.MaxTopLimit = kit.Solutions.MaxTopLimit
	}

	// Events fallback + channel policy. Defaults must be fully declared
	// (validator-required), so fixtures inherit the kit values when their
	// override left them nil.
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

	// Search stopwords.
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

	// Views: each sub-block fills its omitted fields.
	mergeViewSettings(&cfg.Views, kit.Views)
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

// registryFromBundle builds an instance-scoped EnumRegistry from the
// bundle's priority + severity tables. Used by LoadBundle so every
// fixture-driven test runs with the same id↔value mapping the production
// composition roots build.
func registryFromBundle(bundle config.Bundle) *domain.EnumRegistry {
	priorityPairs := make([]domain.PriorityPair, len(bundle.Config.Priorities))
	for i, p := range bundle.Config.Priorities {
		priorityPairs[i] = domain.PriorityPair{ID: p.ID, Value: p.Value, Default: p.Default}
	}
	severityPairs := make([]domain.SeverityPair, len(bundle.Config.Severities))
	for i, s := range bundle.Config.Severities {
		severityPairs[i] = domain.SeverityPair{ID: s.ID, Value: s.Value, Default: s.Default}
	}
	return domain.NewEnumRegistry(priorityPairs, severityPairs)
}
