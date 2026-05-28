package domain

import "testing"

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
