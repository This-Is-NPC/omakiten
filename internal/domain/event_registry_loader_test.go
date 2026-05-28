package domain

import (
	"strings"
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

func TestLoadEventRegistryFromYAML_PopulatesRegistry(t *testing.T) {
	const fmtID = "__test.fmt.load"
	setupTestFormatter(t, fmtID, func(EventRow) string { return "load-ok" })

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
