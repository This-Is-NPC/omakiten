package domain

import (
	"testing"
)

func TestKnownEventTypesCoversCatalog(t *testing.T) {
	want := map[string]struct{}{
		EventTypeComment:                      {},
		EventTypeCommentEdited:                {},
		EventTypeCommentRemoved:               {},
		EventTypeTaskCreated:                  {},
		EventTypeTaskMoved:                    {},
		EventTypeTaskMigrated:                 {},
		EventTypeTaskBucketOrphaned:           {},
		EventTypeTaskCompleted:                {},
		EventTypeTaskEdited:                   {},
		EventTypeTaskRemoved:                  {},
		EventTypeTaskArchived:                 {},
		EventTypeTaskUnarchived:               {},
		EventTypeTaskAssigned:                 {},
		EventTypeTaskUnassigned:               {},
		EventTypeProjectRemoved:               {},
		EventTypePlanCreated:                  {},
		EventTypePlanWaveAdded:                {},
		EventTypePlanGoalEdited:               {},
		EventTypePlanEdited:                   {},
		EventTypePlanDeleted:                  {},
		EventTypePlanWaveRemoved:              {},
		EventTypePlanWaveRenamed:              {},
		EventTypePlanWaveReordered:            {},
		EventTypePlanTaskUnassigned:           {},
		EventTypePlanDone:                     {},
		EventTypePlanAbandoned:                {},
		EventTypeTagAdded:                     {},
		EventTypeTagRemoved:                   {},
		EventTypeDependencyAdded:              {},
		EventTypeDependencyRemoved:            {},
		EventTypeGuardViolated:                {},
		EventTypeErrorRecorded:                {},
		EventTypeErrorsResearched:             {},
		EventTypeSolutionAdded:                {},
		EventTypeSolutionConfirmed:            {},
		EventTypeSolutionLiked:                {},
		EventTypeSolutionFailed:               {},
		EventTypeSolutionViewedTop:            {},
		EventTypeHookExecuted:                 {},
		EventTypeNotificationShown:            {},
		EventTypeExternalPrefix + "ci_failed": {},
		EventTypeBundleSwapped:                {},
		EventTypeBundleImported:               {},
		EventTypeSubtaskKitNoticeEmitted:      {},
		EventTypeConfirmationGranted:          {},
		EventTypeCLIToolCall:                  {},
		EventTypeTUIToolCall:                  {},
		EventTypeTrickExecuted:                {},
		EventTypeUpdateHealthCheckPassed:      {},
		EventTypeUpdateHealthCheckFailed:      {},
		EventTypeUpdateSwapCompleted:          {},
		EventTypeUpdateSwapAborted:            {},
		EventTypeTUIHealthCheckFailed:         {},
	}
	if len(fixtureRegistry().Types()) != len(want) {
		t.Fatalf("fixtureRegistry().Types() len = %d, want %d", len(fixtureRegistry().Types()), len(want))
	}
	got := map[string]struct{}{}
	for _, ev := range fixtureRegistry().Types() {
		if _, dup := got[ev]; dup {
			t.Fatalf("duplicate event type %q", ev)
		}
		got[ev] = struct{}{}
	}
	for ev := range want {
		if _, ok := got[ev]; !ok {
			t.Fatalf("fixtureRegistry().Types() missing %q", ev)
		}
	}
}

func TestIsKnownEventType(t *testing.T) {
	for _, ev := range fixtureRegistry().Types() {
		if fixtureRegistry().CategoryOf(ev) == EventCategoryUnknown {
			t.Fatalf("IsKnownEventType(%q) = false, want true", ev)
		}
	}
	// EventTypeOperation is excluded from fixtureRegistry().Types() because it's
	// written by activity.Track, not the domain emit path.
	for _, ev := range []string{"", "task.unknown", EventTypeOperation} {
		if fixtureRegistry().CategoryOf(ev) != EventCategoryUnknown {
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
	for _, ev := range []string{EventTypeCLIToolCall, EventTypeTUIToolCall} {
		if fixtureRegistry().CategoryOf(ev) == EventCategoryUnknown {
			t.Fatalf("IsKnownEventType(%q) = false, want true (hooks must accept it)", ev)
		}
	}
}
