package domain

import "testing"

func TestKnownEventTypesCoversCatalog(t *testing.T) {
	want := map[string]struct{}{
		EventTypeComment:           {},
		EventTypeCommentEdited:     {},
		EventTypeCommentRemoved:    {},
		EventTypeTaskCreated:       {},
		EventTypeTaskMoved:           {},
		EventTypeTaskMigrated:        {},
		EventTypeTaskBucketOrphaned:  {},
		EventTypeTaskCompleted:       {},
		EventTypeTaskEdited:        {},
		EventTypeTaskRemoved:       {},
		EventTypeTaskArchived:      {},
		EventTypeTaskUnarchived:    {},
		EventTypeTaskAssigned:      {},
		EventTypeTaskUnassigned:    {},
		EventTypeProjectRemoved:    {},
		EventTypePlanCreated:       {},
		EventTypePlanWaveAdded:     {},
		EventTypePlanGoalEdited:    {},
		EventTypePlanDone:          {},
		EventTypePlanAbandoned:     {},
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
		EventTypeHookExecuted:             {},
		EventTypeBundleSwapped:            {},
		EventTypeBundleImported:           {},
		EventTypeSubtaskKitNoticeEmitted:  {},
		EventTypeConfirmationGranted:      {},
		EventTypeCLIToolCall:         {},
		EventTypeMCPToolCall:         {},
		EventTypeTUIToolCall:         {},
		EventTypeTrickExecuted:       {},
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

func TestToolCallEventTypeForSource(t *testing.T) {
	cases := []struct {
		in   ActivitySource
		want string
	}{
		{ActivitySourceCLI, EventTypeCLIToolCall},
		{ActivitySourceMCP, EventTypeMCPToolCall},
		{ActivitySourceTUI, EventTypeTUIToolCall},
	}
	for _, c := range cases {
		got := ToolCallEventTypeForSource(c.in)
		if got != c.want {
			t.Fatalf("ToolCallEventTypeForSource(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if ToolCallEventTypeForSource(ActivitySource("unknown")) != "" {
		t.Fatalf("unknown source should map to empty string")
	}
}

func TestToolCallEventTypesAreKnown(t *testing.T) {
	for _, ev := range []string{EventTypeCLIToolCall, EventTypeMCPToolCall, EventTypeTUIToolCall} {
		if !IsKnownEventType(ev) {
			t.Fatalf("IsKnownEventType(%q) = false, want true (hooks must accept it)", ev)
		}
	}
}
