package domain

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestEventCategoryOfCoversKnownEventTypes locks AC#1: every entry in
// KnownEventTypes must map to a concrete category — never the unknown
// fallback. When a new event_type lands in event.go, this test fails
// until the EventCategoryOf switch grows an arm.
func TestEventCategoryOfCoversKnownEventTypes(t *testing.T) {
	for _, ev := range KnownEventTypes {
		t.Run(ev, func(t *testing.T) {
			cat := EventCategoryOf(ev)
			if cat == EventCategoryUnknown {
				t.Fatalf("EventCategoryOf(%q) returned EventCategoryUnknown — add a switch arm in event_category.go", ev)
			}
			if !categoryInKnownSet(cat) {
				t.Fatalf("EventCategoryOf(%q) = %q which is not in KnownEventCategories", ev, cat)
			}
		})
	}
}

// TestEventCategoryOfUnknownReturnsUnknown locks the fallback for
// event_type values the catalog does not know about.
func TestEventCategoryOfUnknownReturnsUnknown(t *testing.T) {
	for _, ev := range []string{"", "task.does_not_exist", "garbage", EventTypeOperation} {
		if got := EventCategoryOf(ev); got != EventCategoryUnknown {
			t.Fatalf("EventCategoryOf(%q) = %q, want EventCategoryUnknown", ev, got)
		}
	}
}

// TestKnownEventCategoriesNoDuplicates locks the category catalog
// itself: every entry is unique and EventCategoryUnknown is excluded.
func TestKnownEventCategoriesNoDuplicates(t *testing.T) {
	seen := map[EventCategory]struct{}{}
	for _, c := range KnownEventCategories {
		if c == EventCategoryUnknown {
			t.Fatalf("EventCategoryUnknown must not appear in KnownEventCategories")
		}
		if _, dup := seen[c]; dup {
			t.Fatalf("duplicate category %q in KnownEventCategories", c)
		}
		seen[c] = struct{}{}
	}
}

func categoryInKnownSet(c EventCategory) bool {
	for _, k := range KnownEventCategories {
		if k == c {
			return true
		}
	}
	return false
}

// TestEventTypesForCategoryMatchesEventCategoryOf locks the inverse
// mapping: every event_type in KnownEventTypes must appear under the
// category EventCategoryOf assigns to it. Failure means the cached
// inversion drifted from the canonical switch — should be impossible
// because the map is computed from EventCategoryOf at init time, but
// the assertion guards against future hand-edits of the cache.
func TestEventTypesForCategoryMatchesEventCategoryOf(t *testing.T) {
	for _, ev := range KnownEventTypes {
		cat := EventCategoryOf(ev)
		got := EventTypesForCategory(cat)
		found := false
		for _, e := range got {
			if e == ev {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EventTypesForCategory(%q) = %v; missing %q (EventCategoryOf says it belongs there)",
				cat, got, ev)
		}
	}
}

// TestEventTypesForCategoryReturnsNilForUnknown — categories outside
// KnownEventCategories (including EventCategoryUnknown by construction
// — it has no event_types because no arm returns it as a primary
// classification) yield nil so callers can treat the result as a
// distinguishable "no matches" sentinel.
func TestEventTypesForCategoryReturnsNilForUnknown(t *testing.T) {
	if got := EventTypesForCategory(EventCategory("not-a-category")); got != nil {
		t.Fatalf("EventTypesForCategory(\"not-a-category\") = %v, want nil", got)
	}
}

// TestEventTypesForCategoryIsDeterministic locks the SQL IN-list
// ordering invariant. Go map iteration order is non-deterministic, so
// EventTypesForCategory must sort each category bucket at init time.
// Without this lock, two callers building the same query could produce
// different SQL strings, breaking statement caches and EXPLAIN-plan
// tests. Repeated calls must return slices with identical contents and
// ordering.
func TestEventTypesForCategoryIsDeterministic(t *testing.T) {
	for _, c := range KnownEventCategories {
		a := EventTypesForCategory(c)
		b := EventTypesForCategory(c)
		if diff := cmp.Diff(a, b); diff != "" {
			t.Errorf("EventTypesForCategory(%q) non-deterministic (-first +second):\n%s", c, diff)
		}
	}
}

// TestEventTypesForCategoryMemoizes locks the memoization contract:
// repeated calls hit the cached category index instead of re-walking
// EventDefinitions, and each call returns a fresh slice (so callers
// can mutate the result without corrupting the cache).
//
// The cache identity is asserted indirectly: mutating one returned
// slice must not change the contents of a slice returned by a later
// call. With the pre-memoization implementation this assertion still
// passed (each call built its own slice), but combined with the
// non-emptiness check it guards both the freshness invariant and the
// rebuild-on-load contract (a stale empty index would fail here).
func TestEventTypesForCategoryMemoizes(t *testing.T) {
	for _, c := range KnownEventCategories {
		first := EventTypesForCategory(c)
		if len(first) == 0 {
			t.Fatalf("EventTypesForCategory(%q) returned empty — categoryIndex not built", c)
		}
		// Defensive-copy contract: mutating the returned slice must
		// not affect later calls.
		first[0] = "__sentinel__"
		second := EventTypesForCategory(c)
		if second[0] == "__sentinel__" {
			t.Fatalf("EventTypesForCategory(%q) leaked the cached slice — mutation visible across calls", c)
		}
	}
}
