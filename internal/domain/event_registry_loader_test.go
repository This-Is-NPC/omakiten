package domain

import (
	"strings"
	"sync"
	"testing"
)

// setupTestFormatter registers a test formatter id and queues cleanup so the
// global formatterRegistry stays isolated across tests.
func setupTestFormatter(t *testing.T, id string, fn func(EventRow) string) {
	t.Helper()
	registerFormatter(id, fn)
	t.Cleanup(func() {
		delete(formatterRegistry, id)
	})
}

// restoreFixtureRegistry queues a Cleanup hook that re-loads the embedded
// fixture registry once the current test returns. Loader tests overwrite
// EventDefinitions/EventDefByKey/KnownEventTypes wholesale, so without
// this hook a later test in the same `go test` run would see a one-entry
// registry instead of the 41-entry fixture TestMain installed.
func restoreFixtureRegistry(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := loadFixtureRegistry(); err != nil {
			t.Fatalf("restore fixture registry: %v", err)
		}
	})
}

func TestLoadEventRegistryFromYAML_PopulatesRegistry(t *testing.T) {
	const fmtID = "__test.fmt.load"
	setupTestFormatter(t, fmtID, func(EventRow) string { return "load-ok" })
	restoreFixtureRegistry(t)

	yaml := `defaults:
  log_visible: true
  metric: events
  entity_type: task
definitions:
  task_created:
    category: task
    display: Task created
    formatter: __test.fmt.load
`
	if err := LoadEventRegistryFromYAML([]byte(yaml)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(EventDefinitions) != 1 {
		t.Fatalf("want 1 entry, got %d", len(EventDefinitions))
	}
	def := EventDefinitions[0]
	if def.Key != "task_created" {
		t.Fatalf("Key: got %q, want %q", def.Key, "task_created")
	}
	if def.Category != EventCategoryTask {
		t.Fatalf("Category: got %q, want %q", def.Category, EventCategoryTask)
	}
	if def.Display != "Task created" {
		t.Fatalf("Display: got %q", def.Display)
	}
	if def.Formatter == nil || def.Formatter(EventRow{}) != "load-ok" {
		t.Fatalf("Formatter binding lost")
	}
	got, ok := EventDefByKey["task_created"]
	if !ok || got.Key != "task_created" {
		t.Fatalf("EventDefByKey missing entry: %+v", EventDefByKey)
	}
}

func TestLoadEventRegistryFromYAML_DefaultsMerge(t *testing.T) {
	const fmtID = "__test.fmt.defaults"
	setupTestFormatter(t, fmtID, func(EventRow) string { return "" })
	restoreFixtureRegistry(t)

	yaml := `defaults:
  log_visible: true
  metric: foo
  entity_type: task
definitions:
  inherits_all:
    category: task
    display: Inherits all
    formatter: __test.fmt.defaults
  overrides_metric:
    category: task
    display: Overrides metric
    metric: bar
    formatter: __test.fmt.defaults
`
	if err := LoadEventRegistryFromYAML([]byte(yaml)); err != nil {
		t.Fatalf("load: %v", err)
	}
	inh, ok := EventDefByKey["inherits_all"]
	if !ok {
		t.Fatalf("inherits_all missing")
	}
	if inh.Metric != "foo" {
		t.Fatalf("inherits_all.Metric: got %q, want %q", inh.Metric, "foo")
	}
	if !inh.LogVisible {
		t.Fatalf("inherits_all.LogVisible: got false, want true")
	}
	if inh.EntityType != "task" {
		t.Fatalf("inherits_all.EntityType: got %q, want %q", inh.EntityType, "task")
	}
	ovr, ok := EventDefByKey["overrides_metric"]
	if !ok {
		t.Fatalf("overrides_metric missing")
	}
	if ovr.Metric != "bar" {
		t.Fatalf("overrides_metric.Metric: got %q, want %q", ovr.Metric, "bar")
	}
	if !ovr.LogVisible {
		t.Fatalf("overrides_metric.LogVisible: got false, want true (inherited)")
	}
}

func TestLoadEventRegistryFromYAML_UnknownFormatterIdErrors(t *testing.T) {
	yaml := `defaults:
  log_visible: true
definitions:
  task_created:
    category: task
    display: Task created
    formatter: __test.fmt.does_not_exist
`
	err := LoadEventRegistryFromYAML([]byte(yaml))
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "__test.fmt.does_not_exist") {
		t.Fatalf("error does not mention missing id: %v", err)
	}
}

// TestLoadEventRegistryFromYAML_ReloadResetsRegistry locks the loader's
// reset-on-reload contract: a second LoadEventRegistryFromYAML call
// replaces the prior registry wholesale instead of merging into it. The
// production runtime never hot-reloads today, but the loader owns the
// EventDefinitions slice + EventDefByKey map (see registryMu doc) and
// future hot-reload paths rely on the reset behaviour to drop stale
// keys.
func TestLoadEventRegistryFromYAML_ReloadResetsRegistry(t *testing.T) {
	const (
		fmtA = "__test.fmt.reload_a"
		fmtB = "__test.fmt.reload_b"
	)
	setupTestFormatter(t, fmtA, func(EventRow) string { return "A" })
	setupTestFormatter(t, fmtB, func(EventRow) string { return "B" })
	restoreFixtureRegistry(t)

	yamlA := `defaults:
  log_visible: true
  metric: events
  entity_type: task
definitions:
  reload_only_a:
    category: task
    display: Only A
    formatter: __test.fmt.reload_a
`
	if err := LoadEventRegistryFromYAML([]byte(yamlA)); err != nil {
		t.Fatalf("load A: %v", err)
	}
	if len(EventDefinitions) != 1 {
		t.Fatalf("after load A want 1 entry, got %d", len(EventDefinitions))
	}
	if _, ok := EventDefByKey["reload_only_a"]; !ok {
		t.Fatalf("after load A missing reload_only_a")
	}

	yamlB := `defaults:
  log_visible: true
  metric: events
  entity_type: task
definitions:
  reload_only_b:
    category: comment
    display: Only B
    formatter: __test.fmt.reload_b
  reload_also_b:
    category: plan
    display: Also B
    formatter: __test.fmt.reload_b
`
	if err := LoadEventRegistryFromYAML([]byte(yamlB)); err != nil {
		t.Fatalf("load B: %v", err)
	}
	if len(EventDefinitions) != 2 {
		t.Fatalf("after load B want 2 entries, got %d", len(EventDefinitions))
	}
	if _, ok := EventDefByKey["reload_only_a"]; ok {
		t.Fatalf("after load B the prior reload_only_a entry leaked through; loader did not reset")
	}
	for _, key := range []string{"reload_only_b", "reload_also_b"} {
		if _, ok := EventDefByKey[key]; !ok {
			t.Fatalf("after load B missing %q", key)
		}
	}
}

// TestLoadEventRegistryFromYAML_MalformedYAMLReturnsError locks the
// loader's parse-error path: invalid YAML must surface a wrapped error
// (so callers can attribute the failure) and must not partially mutate
// the registry. With the loader's eager reset-before-validate shape,
// only a successful parse + validate sequence reaches the global
// assignments — a malformed payload is rejected before any global
// touches happen.
func TestLoadEventRegistryFromYAML_MalformedYAMLReturnsError(t *testing.T) {
	restoreFixtureRegistry(t)

	// Capture pre-call registry length so we can verify the loader
	// does not partially mutate on a malformed payload.
	preLen := len(EventDefinitions)

	// `[unclosed` is invalid YAML — the flow-sequence opener never
	// closes, so gopkg.in/yaml.v3 fails with a parse error.
	err := LoadEventRegistryFromYAML([]byte("definitions: [unclosed"))
	if err == nil {
		t.Fatalf("want error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parse yaml") {
		t.Fatalf("error does not wrap the parse failure with the loader prefix: %v", err)
	}
	if len(EventDefinitions) != preLen {
		t.Fatalf("EventDefinitions mutated on malformed input: want len %d, got %d", preLen, len(EventDefinitions))
	}
}

func TestLoadEventRegistryFromYAML_MissingDefinitionsErrors(t *testing.T) {
	yaml := `defaults:
  log_visible: true
  metric: foo
`
	err := LoadEventRegistryFromYAML([]byte(yaml))
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "definitions") {
		t.Fatalf("error does not mention definitions: %v", err)
	}
}

// TestLoadEventRegistryConcurrentReloadIsSerialized exercises the
// registryMu write lock. Spawns N goroutines that each call
// LoadEventRegistryFromYAML with a valid payload; without the mutex,
// `go test -race` flags the concurrent writes to EventDefinitions /
// EventDefByKey / KnownEventTypes / categoryIndex. The assertion that
// the registry remains non-empty after the dust settles guards against
// a partial-write race nullifying the final state.
//
// Documents the loader's defensive concurrency posture: production
// callers run the loader once at boot, but the lock keeps the
// invariant true under accidental concurrent reload paths.
func TestLoadEventRegistryConcurrentReloadIsSerialized(t *testing.T) {
	const fmtID = "__test.fmt.concurrent"
	setupTestFormatter(t, fmtID, func(EventRow) string { return "concurrent" })
	restoreFixtureRegistry(t)

	yaml := `defaults:
  log_visible: true
  metric: events
  entity_type: task
definitions:
  concurrent_load:
    category: task
    display: Concurrent load
    formatter: __test.fmt.concurrent
`
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if err := LoadEventRegistryFromYAML([]byte(yaml)); err != nil {
				t.Errorf("load: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(EventDefinitions) != 1 {
		t.Fatalf("after concurrent reloads want 1 entry, got %d", len(EventDefinitions))
	}
	if _, ok := EventDefByKey["concurrent_load"]; !ok {
		t.Fatalf("EventDefByKey missing concurrent_load after race")
	}
}

func TestLoadEventRegistryFromYAML_MissingCategoryErrors(t *testing.T) {
	const fmtID = "__test.fmt.missing_cat"
	setupTestFormatter(t, fmtID, func(EventRow) string { return "" })

	yaml := `defaults:
  log_visible: true
definitions:
  no_category:
    display: No category
    formatter: __test.fmt.missing_cat
`
	err := LoadEventRegistryFromYAML([]byte(yaml))
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "no_category") {
		t.Fatalf("error does not mention offending key: %v", err)
	}
	if !strings.Contains(err.Error(), "category") {
		t.Fatalf("error does not mention category: %v", err)
	}
}
