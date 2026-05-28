package domain

import (
	"testing"
	"time"
)

// TestEventFilterZeroValueSemantics locks AC#4 — every axis of
// EventFilter degrades to "no filter" when left at its zero value so
// callers can compose subsets without sentinel constants.
func TestEventFilterZeroValueSemantics(t *testing.T) {
	var f EventFilter

	if f.ProjectID != 0 {
		t.Fatalf("zero EventFilter.ProjectID = %d, want 0", f.ProjectID)
	}
	if len(f.Categories) != 0 {
		t.Fatalf("zero EventFilter.Categories len = %d, want 0", len(f.Categories))
	}
	if !f.Since.IsZero() {
		t.Fatalf("zero EventFilter.Since = %v, want zero-value time", f.Since)
	}
	if f.Limit != 0 {
		t.Fatalf("zero EventFilter.Limit = %d, want 0 (treated as no cap)", f.Limit)
	}
	if f.Order != "" {
		t.Fatalf("zero EventFilter.Order = %q, want empty (adapter default)", f.Order)
	}
}

// TestEventFilterCarriesValues — once populated, the struct holds the
// caller's values unchanged. Pure value semantics; documents the
// contract for adapter implementers.
func TestEventFilterCarriesValues(t *testing.T) {
	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	f := EventFilter{
		ProjectID:  7,
		Categories: []EventCategory{EventCategoryTask, EventCategoryComment},
		Since:      since,
		Limit:      100,
		Order:      "asc",
	}
	if f.ProjectID != 7 || f.Limit != 100 || f.Order != "asc" {
		t.Fatalf("EventFilter scalar fields lost values: %+v", f)
	}
	if !f.Since.Equal(since) {
		t.Fatalf("EventFilter.Since = %v, want %v", f.Since, since)
	}
	if len(f.Categories) != 2 || f.Categories[0] != EventCategoryTask || f.Categories[1] != EventCategoryComment {
		t.Fatalf("EventFilter.Categories lost values: %+v", f.Categories)
	}
}

// TestEventRowZeroValue — basic sanity that the zero value is usable
// (no pointer fields needing initialisation).
func TestEventRowZeroValue(t *testing.T) {
	var r EventRow
	if r.ID != 0 || r.EventType != "" || r.Payload != "" {
		t.Fatalf("zero EventRow has non-zero fields: %+v", r)
	}
}
