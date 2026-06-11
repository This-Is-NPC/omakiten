package config

import "testing"

func TestResolveRetentionInheritance(t *testing.T) {
	days7 := 7
	rows500 := 500
	days90 := 90
	days30 := 30

	events := EventsSettings{
		Retention: EventsRetentionBlock{
			Defaults: EventRetentionDefaults{MaxAgeDays: 0, MaxRows: 0},
			ByCategory: map[string]EventRetentionSettings{
				"tool_call": {MaxAgeDays: &days7, MaxRows: &rows500},
				"guard":     {MaxAgeDays: &days90},
			},
			Overrides: map[string]EventRetentionSettings{
				"hook.executed": {MaxAgeDays: &days30},
			},
		},
		Definitions: map[string]EventDefinitionSettings{
			"cli.tool_call":   {Category: "tool_call"},
			"guard.violated":  {Category: "guard"},
			"hook.executed":   {Category: "hook"},
			"task.created":    {Category: "task"},
		},
	}

	tc := events.ResolveRetention("cli.tool_call")
	if tc.MaxAgeDays != 7 || tc.MaxRows != 500 {
		t.Fatalf("tool_call = %+v, want 7/500", tc)
	}
	guard := events.ResolveRetention("guard.violated")
	if guard.MaxAgeDays != 90 || guard.MaxRows != 0 {
		t.Fatalf("guard = %+v, want 90/0", guard)
	}
	hook := events.ResolveRetention("hook.executed")
	if hook.MaxAgeDays != 30 || hook.MaxRows != 0 {
		t.Fatalf("hook override = %+v, want 30/0", hook)
	}
	task := events.ResolveRetention("task.created")
	if task.MaxAgeDays != 0 || task.MaxRows != 0 {
		t.Fatalf("task = %+v, want unlimited", task)
	}
}

func TestNormalizeEventsRetentionMergesLegacyActivityLog(t *testing.T) {
	cfg := Settings{
		ActivityLog: ActivityLogSettings{MaxRows: 250, MaxAgeDays: 14},
		Events: EventsSettings{
			Retention: EventsRetentionBlock{
				Defaults: EventRetentionDefaults{MaxAgeDays: 0, MaxRows: 0},
			},
		},
	}
	kit := Settings{
		Events: EventsSettings{
			Retention: EventsRetentionBlock{
				Defaults: EventRetentionDefaults{MaxAgeDays: 0, MaxRows: 0},
				ByCategory: map[string]EventRetentionSettings{
					"audit": {MaxAgeDays: ptrInt(365)},
				},
			},
		},
	}

	NormalizeEventsRetention(&cfg, kit)
	tc := cfg.Events.Retention.ByCategory["tool_call"]
	if tc.MaxAgeDays == nil || *tc.MaxAgeDays != 14 {
		t.Fatalf("tool_call max_age_days = %v, want 14", tc.MaxAgeDays)
	}
	if tc.MaxRows == nil || *tc.MaxRows != 250 {
		t.Fatalf("tool_call max_rows = %v, want 250", tc.MaxRows)
	}
	if cfg.Events.Retention.ByCategory["audit"].MaxAgeDays == nil || *cfg.Events.Retention.ByCategory["audit"].MaxAgeDays != 365 {
		t.Fatalf("inherited audit policy missing")
	}
}

func ptrInt(v int) *int { return &v }
