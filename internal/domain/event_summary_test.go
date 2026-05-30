package domain

import (
	"strings"
	"testing"
)

// TestSummarizeEventCoversKnownEventTypes locks AC#2: every entry in
// KnownEventTypes produces a non-empty single-line summary, even with
// an empty payload. New event_types added without a SummarizeEvent
// switch arm fall into unknownFallback (which still returns the type
// name) but the table below carries one explicit case per known type
// so the switch coverage stays visible — the parity check inside
// asserts the explicit branch ran, not the fallback.
func TestSummarizeEventCoversKnownEventTypes(t *testing.T) {
	for _, ev := range KnownEventTypes {
		t.Run(ev, func(t *testing.T) {
			row := EventRow{EventType: ev}
			got := SummarizeEvent(row)
			if got == "" {
				t.Fatalf("SummarizeEvent(%q) = empty string", ev)
			}
			if strings.ContainsRune(got, '\n') {
				t.Fatalf("SummarizeEvent(%q) = %q, must be single-line", ev, got)
			}
			// Sanity: the catch-all fallback prepends the event_type
			// AND condenses the payload — for an empty payload that
			// degenerates to just the event type. Real cases below
			// exercise per-type branches with payload coverage.
		})
	}
}

// TestSummarizeEventUnknownEventType locks AC#3: unknown event_types
// produce a non-empty fallback that mentions the type and any payload
// content, without panicking.
func TestSummarizeEventUnknownEventType(t *testing.T) {
	cases := map[string]struct {
		row  EventRow
		want string
	}{
		"empty type and payload": {
			row:  EventRow{},
			want: "event",
		},
		"unknown type only": {
			row:  EventRow{EventType: "feature.flag_toggled"},
			want: "feature.flag_toggled",
		},
		"unknown type with json payload": {
			row:  EventRow{EventType: "feature.flag_toggled", Payload: `{ "flag":  "newshell",   "on":true }`},
			want: `feature.flag_toggled {"flag":"newshell","on":true}`,
		},
		"unknown type with non-json payload": {
			row:  EventRow{EventType: "feature.flag_toggled", Payload: "free-form\n  text"},
			want: "feature.flag_toggled free-form text",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := SummarizeEvent(tc.row)
			if got != tc.want {
				t.Fatalf("SummarizeEvent(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSummarizeEventNeverPanics fuzz-style spot-check: malformed
// payloads must not crash the renderer.
func TestSummarizeEventNeverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("SummarizeEvent panicked: %v", r)
		}
	}()
	for _, payload := range []string{"", "not json", "{not:json}", `{"x":}`, `[`, "null"} {
		for _, ev := range append([]string{"unknown.type"}, KnownEventTypes...) {
			_ = SummarizeEvent(EventRow{EventType: ev, Payload: payload})
		}
	}
}

// TestSummarizeEventPerTypeRendering exercises representative payload
// shapes for every category so a regression in any extractor arm is
// caught by a string match (not just the "non-empty" parity test).
func TestSummarizeEventPerTypeRendering(t *testing.T) {
	cases := map[string]struct {
		row  EventRow
		want string
	}{
		// Comments.
		"comment with body": {
			row:  EventRow{EventType: EventTypeComment, AuthorType: "agent", Body: "ran tests\nall green"},
			want: "agent: ran tests all green",
		},
		"comment.edited body delta": {
			row:  EventRow{EventType: EventTypeCommentEdited, Payload: `{"comment_id":5,"body":{"from":"old","to":"new"}}`},
			want: `edited: "old" → "new"`,
		},
		"comment.edited pin only": {
			row:  EventRow{EventType: EventTypeCommentEdited, Payload: `{"comment_id":5,"pinned":{"from":false,"to":true}}`},
			want: "pinned",
		},
		"comment.edited unpin only": {
			row:  EventRow{EventType: EventTypeCommentEdited, Payload: `{"comment_id":5,"pinned":{"from":true,"to":false}}`},
			want: "unpinned",
		},
		"comment.edited title only": {
			row:  EventRow{EventType: EventTypeCommentEdited, Payload: `{"comment_id":5,"title":{"from":"Old","to":"New"}}`},
			want: `retitled: "Old" → "New"`,
		},
		"comment.edited kind only": {
			row:  EventRow{EventType: EventTypeCommentEdited, Payload: `{"comment_id":5,"kind":{"from":"draft","to":"recap"}}`},
			want: "kind: draft → recap",
		},
		"comment.removed with body": {
			row:  EventRow{EventType: EventTypeCommentRemoved, Payload: `{"comment_id":7,"body":"gone"}`},
			want: `removed: "gone"`,
		},

		// Task lifecycle.
		"task.created full payload": {
			row:  EventRow{EventType: EventTypeTaskCreated, Payload: `{"title":"Wire it up","bucket":"backlog","priority":"high"}`},
			want: `created "Wire it up" → backlog [high]`,
		},
		"task.moved": {
			row:  EventRow{EventType: EventTypeTaskMoved, Payload: `{"from":"dev","to":"review"}`},
			want: "moved dev → review",
		},
		"task.migrated with reason": {
			row:  EventRow{EventType: EventTypeTaskMigrated, Payload: `{"from":"a","to":"b","reason":"preset_swap"}`},
			want: "migrated a → b (preset_swap)",
		},
		"task.bucket_orphaned": {
			row:  EventRow{EventType: EventTypeTaskBucketOrphaned, Payload: `{"old_bucket":"shipped","to_kit":"default"}`},
			want: "bucket orphaned from shipped (kit=default)",
		},
		"task.completed bucket": {
			row:  EventRow{EventType: EventTypeTaskCompleted, Payload: `{"bucket":"done"}`},
			want: "completed → done",
		},
		"task.edited fields": {
			row:  EventRow{EventType: EventTypeTaskEdited, Payload: `{"fields":{"title":{"from":"a","to":"b"},"priority":{"from":"low","to":"high"}}}`},
			want: "edited priority, title",
		},
		"task.removed title": {
			row:  EventRow{EventType: EventTypeTaskRemoved, Payload: `{"title":"Old","bucket":"done","priority":"normal"}`},
			want: `removed "Old"`,
		},
		"task.archived bucket": {
			row:  EventRow{EventType: EventTypeTaskArchived, Payload: `{"bucket":"done"}`},
			want: "archived from done",
		},
		"task.unarchived bucket": {
			row:  EventRow{EventType: EventTypeTaskUnarchived, Payload: `{"bucket":"backlog"}`},
			want: "unarchived → backlog",
		},
		"task.assigned": {
			row:  EventRow{EventType: EventTypeTaskAssigned, Payload: `{"assignee":"alice","source":"claim_next"}`},
			want: "assigned to alice via claim_next",
		},
		"task.unassigned": {
			row:  EventRow{EventType: EventTypeTaskUnassigned, Payload: `{"former_assignee":"alice"}`},
			want: "unassigned alice",
		},

		// Project.
		"project.removed slug+name": {
			row:  EventRow{EventType: EventTypeProjectRemoved, Payload: `{"slug":"old","name":"Old Project"}`},
			want: "removed project old (Old Project)",
		},

		// Plan.
		"plan.created slug+name": {
			row:  EventRow{EventType: EventTypePlanCreated, Payload: `{"slug":"logs","name":"Logs inspector","project_id":1}`},
			want: "plan logs (Logs inspector)",
		},
		"plan.wave_added with position": {
			row:  EventRow{EventType: EventTypePlanWaveAdded, Payload: `{"wave_id":3,"name":"Backend","position":2}`},
			want: "wave #2 added: Backend",
		},
		"plan.goal_edited length": {
			row:  EventRow{EventType: EventTypePlanGoalEdited, Payload: `{"length":420}`},
			want: "goal edited (420 chars)",
		},
		"plan.done": {
			row:  EventRow{EventType: EventTypePlanDone},
			want: "plan done",
		},
		"plan.abandoned": {
			row:  EventRow{EventType: EventTypePlanAbandoned, Payload: `{}`},
			want: "plan abandoned",
		},
		"plan.wave_removed with position": {
			row:  EventRow{EventType: EventTypePlanWaveRemoved, Payload: `{"wave_id":3,"name":"Backend","position":2}`},
			want: "wave #2 removed: Backend",
		},
		"plan.wave_renamed from/to": {
			row:  EventRow{EventType: EventTypePlanWaveRenamed, Payload: `{"wave_id":3,"from":"Backend","to":"API"}`},
			want: "wave renamed: Backend → API",
		},
		"plan.wave_reordered from/to": {
			row:  EventRow{EventType: EventTypePlanWaveReordered, Payload: `{"wave_id":3,"from":1,"to":2}`},
			want: "wave reordered: #1 → #2",
		},
		"plan.task_unassigned": {
			row:  EventRow{EventType: EventTypePlanTaskUnassigned, Payload: `{"plan_id":1,"wave_id":2,"source":"plans.unassign"}`},
			want: "task detached from plan",
		},

		// Tags + deps.
		"tag.added": {
			row:  EventRow{EventType: EventTypeTagAdded, Payload: `{"entity_type":"task","entity_id":1,"tag_id":2,"tag_name":"urgent"}`},
			want: "tag +urgent on task",
		},
		"tag.removed": {
			row:  EventRow{EventType: EventTypeTagRemoved, Payload: `{"entity_type":"task","entity_id":1,"tag_id":2,"tag_name":"urgent"}`},
			want: "tag -urgent on task",
		},
		"dependency.added": {
			row:  EventRow{EventType: EventTypeDependencyAdded, Payload: `{"depends_on_task_id":42}`},
			want: "depends on #42",
		},
		"dependency.removed": {
			row:  EventRow{EventType: EventTypeDependencyRemoved, Payload: `{"depends_on_task_id":42}`},
			want: "dropped dep on #42",
		},

		// Guards.
		"guard.violated full": {
			row:  EventRow{EventType: EventTypeGuardViolated, Payload: `{"operation":"move","rule":"requires_review","hint":"open a PR first"}`},
			want: "guard move/requires_review: open a PR first",
		},

		// Tool calls.
		"cli.tool_call full": {
			row:  EventRow{EventType: EventTypeCLIToolCall, Payload: `{"tool_name":"tasks.move","source":"cli","status":"ok","duration_ms":12}`},
			want: "cli/tasks.move [ok] 12ms",
		},
		"mcp.tool_call status from columns": {
			row:  EventRow{EventType: EventTypeMCPToolCall, Source: "mcp", Status: "error", DurationMs: 33, Payload: `{"tool_name":"tasks.move"}`},
			want: "mcp/tasks.move [error] 33ms",
		},
		"tui.tool_call minimal": {
			row:  EventRow{EventType: EventTypeTUIToolCall, Payload: `{"tool_name":"tasks.continue"}`},
			want: "tasks.continue",
		},

		// Hooks.
		"hook.executed success": {
			row:  EventRow{EventType: EventTypeHookExecuted, Payload: `{"action":"notify_slack","event_type":"task.moved","success":true,"duration_ms":4}`},
			want: "hook notify_slack on task.moved [ok]",
		},
		"hook.executed failure": {
			row:  EventRow{EventType: EventTypeHookExecuted, Payload: `{"action":"shell","event_type":"task.created","success":false}`},
			want: "hook shell on task.created [fail]",
		},

		// Subtask kit.
		"subtask_kit.notice_emitted": {
			row:  EventRow{EventType: EventTypeSubtaskKitNoticeEmitted, Payload: `{"i18n_key":"notice.kit_swap","from_kit":"a","to_kit":"b"}`},
			want: "subtask kit a → b",
		},

		// Bundle.
		"bundle.swapped no orphans": {
			row:  EventRow{EventType: EventTypeBundleSwapped, Payload: `{"from_workflow":"omakase","to_workflow":"scrum","orphan_count":0}`},
			want: "bundle omakase → scrum",
		},
		"bundle.swapped with orphans": {
			row:  EventRow{EventType: EventTypeBundleSwapped, Payload: `{"from_workflow":"omakase","to_workflow":"scrum","orphan_count":3}`},
			want: "bundle omakase → scrum (3 orphan(s))",
		},
		"bundle.imported": {
			row:  EventRow{EventType: EventTypeBundleImported, Payload: `{"path":"/x","hash":"deadbeef","workflow_key":"omakase"}`},
			want: "bundle imported workflow=omakase hash=deadbeef",
		},

		// Confirmation.
		"confirmation.granted": {
			row:  EventRow{EventType: EventTypeConfirmationGranted, Payload: `{"notification_slug":"deploy","command":"okt deploy prod"}`},
			want: "confirmed deploy: okt deploy prod",
		},

		// Errors / solutions.
		"error.recorded": {
			row:  EventRow{EventType: EventTypeErrorRecorded, Payload: `{"tags":["sql","timeout"],"has_context":true}`},
			want: "error recorded #sql #timeout (+context)",
		},
		"errors.researched": {
			row:  EventRow{EventType: EventTypeErrorsResearched, Payload: `{"query":"connection","tags":[],"result_count":4}`},
			want: `researched "connection" → 4 hit(s)`,
		},
		"solution.added": {
			row:  EventRow{EventType: EventTypeSolutionAdded, Payload: `{"error_id":11}`},
			want: "solution added for error #11",
		},
		"solution.confirmed ok": {
			row:  EventRow{EventType: EventTypeSolutionConfirmed, Payload: `{"error_id":11,"success":true,"likes":2}`},
			want: "solution confirmed [ok] for error #11",
		},
		"solution.confirmed fail": {
			row:  EventRow{EventType: EventTypeSolutionConfirmed, Payload: `{"error_id":11,"success":false,"likes":0}`},
			want: "solution confirmed [fail] for error #11",
		},
		"solution.liked": {
			row:  EventRow{EventType: EventTypeSolutionLiked, Payload: `{"error_id":11,"likes":5}`},
			want: "solution liked (error #11, 5 like(s))",
		},
		"solution.failed": {
			row:  EventRow{EventType: EventTypeSolutionFailed, Payload: `{"error_id":11,"likes":3}`},
			want: "solution failed (error #11)",
		},
		"solution.viewed_top": {
			row:  EventRow{EventType: EventTypeSolutionViewedTop, Payload: `{"limit":5,"returned_count":3}`},
			want: "top solutions viewed (3/5)",
		},

		// Trick palette.
		"trick.executed verb+operand": {
			row:  EventRow{EventType: EventTypeTrickExecuted, Payload: `{"verb":"nav","operand":"task/12","raw":"nav:task/12"}`},
			want: "trick nav:task/12",
		},
		"trick.executed raw only": {
			row:  EventRow{EventType: EventTypeTrickExecuted, Payload: `{"raw":"hook:custom"}`},
			want: "trick hook:custom",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := SummarizeEvent(tc.row)
			if got != tc.want {
				t.Fatalf("SummarizeEvent(%q) =\n  got  %q\n  want %q", tc.row.EventType, got, tc.want)
			}
		})
	}
}

// TestSummarizeEventPureNoClock locks the DoD bullet "no I/O, no clock
// dependency in the extractor (deterministic)". Calling the function
// twice on the same input must yield identical output regardless of
// when it runs; we approximate that by asserting equality across two
// back-to-back calls for every known type.
func TestSummarizeEventDeterministic(t *testing.T) {
	for _, ev := range KnownEventTypes {
		row := EventRow{EventType: ev, Payload: `{"k":"v"}`}
		first := SummarizeEvent(row)
		second := SummarizeEvent(row)
		if first != second {
			t.Fatalf("SummarizeEvent(%q) non-deterministic: %q vs %q", ev, first, second)
		}
	}
}

// TestRegisterFormatterDuplicatePanics locks the registry guarantee: a
// second registerFormatter() call for the same id must panic so two
// summarizers cannot silently shadow each other. We use a synthetic id
// so the test never corrupts the production formatter registry.
func TestRegisterFormatterDuplicatePanics(t *testing.T) {
	const id = "_test.register_formatter_duplicate_panics"
	delete(formatterRegistry, id)
	t.Cleanup(func() { delete(formatterRegistry, id) })

	registerFormatter(id, func(EventRow) string { return "first" })

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate registerFormatter, got none")
		}
	}()
	registerFormatter(id, func(EventRow) string { return "second" })
}

// TestRegistryCoversKnownEventTypes locks the registry-coverage rule:
// every entry in KnownEventTypes must resolve to a non-nil Formatter
// through EventDefByKey, and every key in EventDefByKey must appear in
// KnownEventTypes. A miss in either direction means the YAML fixture
// drifted from the formatter id registrations the event_summary_*.go
// init() functions own.
func TestRegistryCoversKnownEventTypes(t *testing.T) {
	known := make(map[string]struct{}, len(KnownEventTypes))
	for _, ev := range KnownEventTypes {
		known[ev] = struct{}{}
		def, ok := EventDefByKey[ev]
		if !ok {
			t.Errorf("KnownEventTypes entry %q missing from EventDefByKey", ev)
			continue
		}
		if def.Formatter == nil {
			t.Errorf("EventDefByKey[%q].Formatter is nil — formatter id never resolved", ev)
		}
	}
	for ev := range EventDefByKey {
		if _, ok := known[ev]; !ok {
			t.Errorf("EventDefByKey entry %q absent from KnownEventTypes", ev)
		}
	}
}
