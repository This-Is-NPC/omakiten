package domain

import (
	"encoding/json"
	"testing"
)

// TestPriorityRegistryRoundTrip exercises the id↔label resolver in
// isolation: registering a table populates Label / FromLabel, and
// MarshalJSON emits the configured label string while UnmarshalJSON
// accepts both label strings and numeric ids. This is the core
// contract the rest of the codebase relies on, so a regression here
// cascades into every render boundary.
func TestPriorityRegistryRoundTrip(t *testing.T) {
	RegisterPriorities([]PriorityPair{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "normal"},
		{ID: 3, Value: "high"},
	})
	t.Cleanup(func() { RegisterPriorities(nil) })

	if Priority(2).Label() != "normal" {
		t.Fatalf("Label(2) = %q, want %q", Priority(2).Label(), "normal")
	}
	if got, ok := PriorityFromLabel("high"); !ok || got != Priority(3) {
		t.Fatalf("FromLabel(high) = %d ok=%v, want 3 ok=true", got, ok)
	}
	if !Priority(1).IsRegistered() {
		t.Fatal("Priority(1).IsRegistered() = false, want true (1=low)")
	}
	if Priority(99).IsRegistered() {
		t.Fatal("Priority(99).IsRegistered() = true, want false (no entry)")
	}

	data, err := json.Marshal(Priority(3))
	if err != nil {
		t.Fatalf("MarshalJSON = %v", err)
	}
	if string(data) != `"high"` {
		t.Fatalf("MarshalJSON(3) = %s, want \"high\"", data)
	}

	var fromString Priority
	if err := json.Unmarshal([]byte(`"low"`), &fromString); err != nil {
		t.Fatalf("UnmarshalJSON(\"low\") = %v", err)
	}
	if fromString != Priority(1) {
		t.Fatalf("UnmarshalJSON(\"low\") = %d, want 1", fromString)
	}

	var fromInt Priority
	if err := json.Unmarshal([]byte(`2`), &fromInt); err != nil {
		t.Fatalf("UnmarshalJSON(2) = %v", err)
	}
	if fromInt != Priority(2) {
		t.Fatalf("UnmarshalJSON(2) = %d, want 2", fromInt)
	}

	var unknown Priority
	if err := json.Unmarshal([]byte(`"urgent"`), &unknown); err == nil {
		t.Fatalf("UnmarshalJSON(\"urgent\") = nil, want error for unknown label")
	}
}

// TestPriorityRegistryEmptyFallback verifies the marshaler degrades to
// raw integer output when the registry has not been wired (test
// fixtures, partial bootstraps). Without this fallback consumers would
// see empty strings instead of unambiguous data.
func TestPriorityRegistryEmptyFallback(t *testing.T) {
	RegisterPriorities(nil)

	if got := Priority(7).Label(); got != "" {
		t.Fatalf("Label() with empty registry = %q, want \"\"", got)
	}
	if got := Priority(7).String(); got != "7" {
		t.Fatalf("String() with empty registry = %q, want \"7\"", got)
	}
	data, err := json.Marshal(Priority(7))
	if err != nil {
		t.Fatalf("MarshalJSON = %v", err)
	}
	if string(data) != "7" {
		t.Fatalf("MarshalJSON(7) without registry = %s, want 7", data)
	}
}
