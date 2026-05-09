package domain

import "testing"

func TestKnownEventTypesCoversCatalog(t *testing.T) {
	want := map[string]struct{}{
		EventTypeComment:           {},
		EventTypeCommentEdited:     {},
		EventTypeCommentRemoved:    {},
		EventTypeTaskCreated:       {},
		EventTypeTaskMoved:         {},
		EventTypeTaskCompleted:     {},
		EventTypeTaskEdited:        {},
		EventTypeTaskRemoved:       {},
		EventTypeTaskArchived:      {},
		EventTypeTaskUnarchived:    {},
		EventTypeTagAdded:          {},
		EventTypeTagRemoved:        {},
		EventTypeDependencyAdded:   {},
		EventTypeDependencyRemoved: {},
		EventTypeGuardViolated:     {},
		EventTypeErrorRecorded:     {},
		EventTypeErrorSearched:     {},
		EventTypeSolutionAdded:     {},
		EventTypeSolutionConfirmed: {},
		EventTypeSolutionLiked:     {},
		EventTypeSolutionFailed:    {},
		EventTypeSolutionViewedTop: {},
		EventTypeHookExecuted:      {},
	}
	if len(KnownEventTypes) != len(want) {
		t.Fatalf("KnownEventTypes len = %d, want %d", len(KnownEventTypes), len(want))
	}
	got := map[string]struct{}{}
	for _, ev := range KnownEventTypes {
		if _, dup := got[ev]; dup {
			t.Fatalf("duplicate event type %q", ev)
		}
		got[ev] = struct{}{}
	}
	for ev := range want {
		if _, ok := got[ev]; !ok {
			t.Fatalf("KnownEventTypes missing %q", ev)
		}
	}
}

func TestIsKnownEventType(t *testing.T) {
	for _, ev := range KnownEventTypes {
		if !IsKnownEventType(ev) {
			t.Fatalf("IsKnownEventType(%q) = false, want true", ev)
		}
	}
	// EventTypeOperation is excluded from KnownEventTypes because it's
	// written by activity.Track, not the domain emit path.
	for _, ev := range []string{"", "task.unknown", EventTypeOperation} {
		if IsKnownEventType(ev) {
			t.Fatalf("IsKnownEventType(%q) = true, want false", ev)
		}
	}
}

func TestCommentAliasesAreEqual(t *testing.T) {
	if EventTypeCommentCreated != EventTypeComment {
		t.Fatalf("EventTypeCommentCreated = %q, want alias of %q", EventTypeCommentCreated, EventTypeComment)
	}
	if EventTypeCommentLegacy != EventTypeComment {
		t.Fatalf("EventTypeCommentLegacy = %q, want alias of %q", EventTypeCommentLegacy, EventTypeComment)
	}
}
