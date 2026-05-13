package domain

import (
	"encoding/json"
	"testing"
)

// TestEnumRegistryPriorityRoundTrip exercises the id↔label resolver in
// isolation: building a registry populates PriorityLabel / PriorityFromLabel
// and reports membership via IsPriorityRegistered.
func TestEnumRegistryPriorityRoundTrip(t *testing.T) {
	r := NewEnumRegistry([]PriorityPair{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "normal"},
		{ID: 3, Value: "high"},
	}, nil)

	if got := r.PriorityLabel(Priority(2)); got != "normal" {
		t.Fatalf("PriorityLabel(2) = %q, want %q", got, "normal")
	}
	if got, ok := r.PriorityFromLabel("high"); !ok || got != Priority(3) {
		t.Fatalf("PriorityFromLabel(high) = %d ok=%v, want 3 ok=true", got, ok)
	}
	if !r.IsPriorityRegistered(Priority(1)) {
		t.Fatal("IsPriorityRegistered(1) = false, want true (1=low)")
	}
	if r.IsPriorityRegistered(Priority(99)) {
		t.Fatal("IsPriorityRegistered(99) = true, want false (no entry)")
	}
}

// TestPriorityMarshalsAsInt locks the wire format: Priority always
// serializes as its raw int id. Label resolution is a boundary concern
// handled by DTOs that hold a registry.
func TestPriorityMarshalsAsInt(t *testing.T) {
	data, err := json.Marshal(Priority(3))
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	if string(data) != "3" {
		t.Fatalf("Marshal(3) = %s, want 3", data)
	}

	var got Priority
	if err := json.Unmarshal([]byte(`2`), &got); err != nil {
		t.Fatalf("Unmarshal(2) = %v", err)
	}
	if got != Priority(2) {
		t.Fatalf("Unmarshal(2) = %d, want 2", got)
	}
}

// TestEnumRegistryDefaultPriorityHonorsFlag was the regression for an
// earlier review item: before threading `Default` through, `default: true`
// in YAML was validator-checked but never consumed by the writer path —
// CreateTask requests with no priority fell back to the SQL column DEFAULT.
// With Default propagated, the registry returns the flagged id.
func TestEnumRegistryDefaultPriorityHonorsFlag(t *testing.T) {
	r := NewEnumRegistry([]PriorityPair{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "normal"},
		{ID: 3, Value: "high"},
		{ID: 4, Value: "urgent", Default: true},
	}, nil)
	if got := r.DefaultPriority(); got != Priority(4) {
		t.Fatalf("DefaultPriority() = %d, want 4 (urgent flagged default)", got)
	}
}

// TestEnumRegistryDefaultPriorityFallsBackToMiddle verifies the registry's
// safety net when no entry sets Default=true. The validator ALLOWS zero
// defaults (it only rejects more than one), and writers must still resolve
// PriorityZero → some sensible id.
func TestEnumRegistryDefaultPriorityFallsBackToMiddle(t *testing.T) {
	r := NewEnumRegistry([]PriorityPair{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "normal"},
		{ID: 3, Value: "high"},
	}, nil)
	if got := r.DefaultPriority(); got != Priority(2) {
		t.Fatalf("DefaultPriority() = %d, want 2 (middle entry of 3-element table)", got)
	}
}

// TestEnumRegistryEmptyReturnsZero exercises the empty-registry path: the
// methods are nil-safe and return zero values so partially-bootstrapped
// tests still write rows by falling through to the storage default.
func TestEnumRegistryEmptyReturnsZero(t *testing.T) {
	r := NewEnumRegistry(nil, nil)
	if got := r.DefaultPriority(); got != PriorityZero {
		t.Fatalf("DefaultPriority() with empty registry = %d, want PriorityZero (0)", got)
	}
	if got := r.PriorityLabel(Priority(7)); got != "" {
		t.Fatalf("PriorityLabel(7) with empty registry = %q, want \"\"", got)
	}
	if got, ok := r.PriorityFromLabel("low"); ok || got != PriorityZero {
		t.Fatalf("PriorityFromLabel(low) on empty registry = (%d, %v), want (0, false)", got, ok)
	}
}

// TestNilEnumRegistryIsSafe ensures a nil receiver does not panic — used by
// adapter layers that may project entities before a bundle is loaded.
func TestNilEnumRegistryIsSafe(t *testing.T) {
	var r *EnumRegistry
	if got := r.PriorityLabel(Priority(1)); got != "" {
		t.Fatalf("nil.PriorityLabel = %q, want \"\"", got)
	}
	if got, ok := r.PriorityFromLabel("low"); ok || got != PriorityZero {
		t.Fatalf("nil.PriorityFromLabel = (%d, %v), want (0, false)", got, ok)
	}
	if r.IsPriorityRegistered(Priority(1)) {
		t.Fatal("nil.IsPriorityRegistered = true, want false")
	}
	if got := r.DefaultPriority(); got != PriorityZero {
		t.Fatalf("nil.DefaultPriority = %d, want 0", got)
	}
}
