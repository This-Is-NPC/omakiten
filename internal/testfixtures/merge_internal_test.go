package testfixtures

import (
	"testing"

	"omakiten/internal/config"
)

// TestMergeKitDefaultsPartialOverride locks the key-level merge for
// cfg.Events.Definitions: a fixture that declares one override (here,
// task.created with a custom Display) must keep its override AND
// inherit the remaining kit entries (here, task.moved).
//
// The pre-fix behaviour was an all-or-nothing swap — len(cfg)==0 →
// copy kit, else leave cfg alone. A fixture with a single-entry
// override would lose every other kit definition, leaving the
// validator with an incomplete registry. The bug surfaced on task #358
// review (W3).
func TestMergeKitDefaultsPartialOverride(t *testing.T) {
	const (
		taskCreatedKey = "task.created"
		taskMovedKey   = "task.moved"
	)
	customDisplay := "fixture-override task.created"

	bundle := config.Bundle{
		Config: config.Settings{
			Events: config.EventsSettings{
				Definitions: map[string]config.EventDefinitionSettings{
					taskCreatedKey: {
						Category: "task",
						Display:  customDisplay,
						// formatter intentionally left as the kit default
						// — the merge must not stomp on it.
					},
				},
			},
		},
	}

	mergeKitDefaults(&bundle)

	got := bundle.Config.Events.Definitions
	created, ok := got[taskCreatedKey]
	if !ok {
		t.Fatalf("merged definitions lost cfg override for %q", taskCreatedKey)
	}
	if created.Display != customDisplay {
		t.Fatalf("cfg override clobbered: %q.Display = %q, want %q",
			taskCreatedKey, created.Display, customDisplay)
	}
	if _, ok := got[taskMovedKey]; !ok {
		t.Fatalf("kit definition %q was not inherited; partial override dropped the rest of the registry", taskMovedKey)
	}
	// The merged map must carry strictly more entries than the
	// pre-merge cfg (1 cfg entry + at least one kit-inherited entry).
	if len(got) < 2 {
		t.Fatalf("merged definitions have only %d entries; expected fixture override + kit inheritance", len(got))
	}
}
