package config_test

import (
	"reflect"
	"sort"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testutil"
)

// kitKeys is the closed set of kits whose YAMLs ship under
// defaults/config/. The parity test iterates every kit so a drift in
// one kit YAML cannot pass green by sampling another.
var kitKeys = []string{"izakaya", "kaiseki", "omakase", "shokunin"}

// refKit pins the kit the cross-kit identity subtest compares against.
// Hoisted to a package-level const so the choice is visible alongside
// kitKeys (rather than buried inside the subtest body) and so any
// future test that needs the canonical kit reads from the same source.
// Omakase is the reference because it is the default kit the runtime
// boots into and the one every TestMain hydrates the registry from.
const refKit = "omakase"

// TestEventRegistryYAMLParity asserts a four-way contract introduced by
// the YAML-driven event registry refactor (Phase 0):
//
//  1. const ↔ YAML key — every domain.EventType* const in
//     testutil.EventRegistry().Types() appears as a key in
//     Settings.Events.Definitions for every shipped kit, and every key
//     in Definitions matches a known const value. Either-side drift
//     fails the test with the offending diff.
//
//  2. formatter id resolves — every `formatter:` id in Definitions
//     resolves through domain.ResolveFormatter (i.e. an
//     event_summary_*.go init() registered the binding under that id).
//
//  3. cross-kit identity — the Definitions maps from the 4 kits are
//     deeply equal so a single canonical registry survives a future
//     pivot that swaps kits at runtime.
//
// The test loads each kit via the same LoadKitConfigByKey path the
// runtime uses, so a regression in either the YAML, the loader, or the
// embed packaging surfaces here before it reaches a user.
func TestEventRegistryYAMLParity(t *testing.T) {
	kits := loadEventRegistryKits(t)
	knownSet := hydrateKnownEventTypes(t)

	t.Run("const_yaml_key_parity", func(t *testing.T) {
		for _, key := range kitKeys {
			t.Run(key, func(t *testing.T) {
				assertEventRegistryKeys(t, key, kits[key], knownSet)
			})
		}
	})

	t.Run("formatter_id_resolves", func(t *testing.T) {
		for _, key := range kitKeys {
			t.Run(key, func(t *testing.T) {
				assertFormatterIDsResolve(t, key, kits[key])
			})
		}
	})

	t.Run("cross_kit_identity", func(t *testing.T) {
		assertCrossKitIdentity(t, kits)
	})
}

func loadEventRegistryKits(t *testing.T) map[string]map[string]config.EventDefinitionSettings {
	t.Helper()
	kits := make(map[string]map[string]config.EventDefinitionSettings, len(kitKeys))
	for _, key := range kitKeys {
		cfg, err := config.LoadKitConfigByKey(key)
		if err != nil {
			t.Fatalf("LoadKitConfigByKey(%q): %v", key, err)
		}
		if len(cfg.Events.Definitions) == 0 {
			t.Fatalf("kit %q: events.definitions block is empty", key)
		}
		kits[key] = cfg.Events.Definitions
	}
	return kits
}

func hydrateKnownEventTypes(t *testing.T) map[string]struct{} {
	t.Helper()
	if _, err := config.BuildEventRegistry(kitEvents(t, refKit)); err != nil {
		t.Fatalf("LoadDomainEventRegistry(%q): %v", refKit, err)
	}
	knownSet := make(map[string]struct{}, len(testutil.EventRegistry().Types()))
	for _, key := range testutil.EventRegistry().Types() {
		knownSet[key] = struct{}{}
	}
	return knownSet
}

func assertEventRegistryKeys(t *testing.T, key string, defs map[string]config.EventDefinitionSettings, knownSet map[string]struct{}) {
	t.Helper()
	var missingInYAML []string
	for _, known := range testutil.EventRegistry().Types() {
		if _, ok := defs[known]; !ok {
			missingInYAML = append(missingInYAML, known)
		}
	}
	var unknownInYAML []string
	for yamlKey := range defs {
		if _, ok := knownSet[yamlKey]; !ok {
			unknownInYAML = append(unknownInYAML, yamlKey)
		}
	}
	sort.Strings(missingInYAML)
	sort.Strings(unknownInYAML)
	if len(missingInYAML) > 0 {
		t.Errorf("kit %q: missing in YAML definitions: %v", key, missingInYAML)
	}
	if len(unknownInYAML) > 0 {
		t.Errorf("kit %q: YAML keys not in testutil.EventRegistry().Types(): %v", key, unknownInYAML)
	}
}

func assertFormatterIDsResolve(t *testing.T, key string, defs map[string]config.EventDefinitionSettings) {
	t.Helper()
	ordered := keysOf(defs)
	for _, definitionKey := range ordered {
		def := defs[definitionKey]
		if def.Formatter == "" {
			t.Errorf("kit %q: definition %q missing formatter id", key, definitionKey)
			continue
		}
		if _, ok := domain.ResolveFormatter(domain.FormatterID(def.Formatter)); !ok {
			t.Errorf("kit %q: definition %q formatter id %q not registered in domain.formatterRegistry",
				key, definitionKey, def.Formatter)
		}
	}
}

func assertCrossKitIdentity(t *testing.T, kits map[string]map[string]config.EventDefinitionSettings) {
	t.Helper()
	refDefs := kits[refKit]
	for _, key := range kitKeys {
		if key == refKit {
			continue
		}
		t.Run(refKit+"_vs_"+key, func(t *testing.T) {
			assertKitIdentity(t, key, refDefs, kits[key])
		})
	}
}

func assertKitIdentity(t *testing.T, key string, refDefs, other map[string]config.EventDefinitionSettings) {
	t.Helper()
	if reflect.DeepEqual(refDefs, other) {
		return
	}
	refKeys := keysOf(refDefs)
	otherKeys := keysOf(other)
	if missing := diff(refKeys, otherKeys); len(missing) > 0 {
		t.Errorf("kit %q missing keys present in %q: %v", key, refKit, missing)
	}
	if extra := diff(otherKeys, refKeys); len(extra) > 0 {
		t.Errorf("kit %q has keys absent from %q: %v", key, refKit, extra)
	}
	for _, definitionKey := range refKeys {
		otherDef, ok := other[definitionKey]
		if !ok {
			continue
		}
		reportFieldDiffs(t, refKit, key, definitionKey, refDefs[definitionKey], otherDef)
	}
}

// kitEvents loads a kit by key and returns its EventsSettings so the
// parity test can hand it to config.LoadDomainEventRegistry without
// re-running the LoadKitConfigByKey gymnastics for each access.
func kitEvents(t *testing.T, key string) config.EventsSettings {
	t.Helper()
	cfg, err := config.LoadKitConfigByKey(key)
	if err != nil {
		t.Fatalf("LoadKitConfigByKey(%q): %v", key, err)
	}
	return cfg.Events
}

func keysOf(m map[string]config.EventDefinitionSettings) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func diff(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, x := range b {
		set[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := set[x]; !ok {
			out = append(out, x)
		}
	}
	return out
}

func reportFieldDiffs(t *testing.T, refKit, otherKit, key string, ref, other config.EventDefinitionSettings) {
	t.Helper()
	if ref.Category != other.Category {
		t.Errorf("kit %q vs %q definition %q: category %q != %q", refKit, otherKit, key, ref.Category, other.Category)
	}
	if ref.Display != other.Display {
		t.Errorf("kit %q vs %q definition %q: display %q != %q", refKit, otherKit, key, ref.Display, other.Display)
	}
	if ref.Formatter != other.Formatter {
		t.Errorf("kit %q vs %q definition %q: formatter %q != %q", refKit, otherKit, key, ref.Formatter, other.Formatter)
	}
	if !ptrStringEqual(ref.EntityType, other.EntityType) {
		t.Errorf("kit %q vs %q definition %q: entity_type %s != %s", refKit, otherKit, key, ptrStringDescribe(ref.EntityType), ptrStringDescribe(other.EntityType))
	}
	if !ptrStringEqual(ref.Metric, other.Metric) {
		t.Errorf("kit %q vs %q definition %q: metric %s != %s", refKit, otherKit, key, ptrStringDescribe(ref.Metric), ptrStringDescribe(other.Metric))
	}
	if !ptrBoolEqual(ref.LogVisible, other.LogVisible) {
		t.Errorf("kit %q vs %q definition %q: log_visible %s != %s", refKit, otherKit, key, ptrBoolDescribe(ref.LogVisible), ptrBoolDescribe(other.LogVisible))
	}
}

func ptrStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func ptrBoolEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func ptrStringDescribe(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func ptrBoolDescribe(p *bool) string {
	if p == nil {
		return "<nil>"
	}
	if *p {
		return "true"
	}
	return "false"
}
