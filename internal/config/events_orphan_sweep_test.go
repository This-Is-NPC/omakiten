package config

import (
	"testing"
	"time"
)

func TestResolveOrphanSweepAppliesCanonicalFloor(t *testing.T) {
	resolved := EventsSettings{}.ResolveOrphanSweep()

	if !resolved.Enabled {
		t.Fatalf("Enabled = false, want true (sweep is on by default)")
	}
	if resolved.Interval != 24*time.Hour {
		t.Fatalf("Interval = %v, want 24h", resolved.Interval)
	}
	if resolved.RetryInterval != time.Hour {
		t.Fatalf("RetryInterval = %v, want 1h", resolved.RetryInterval)
	}
	if resolved.BatchRows != 100 {
		t.Fatalf("BatchRows = %d, want 100", resolved.BatchRows)
	}
	if resolved.MaxRowsPerPass != 500 {
		t.Fatalf("MaxRowsPerPass = %d, want 500", resolved.MaxRowsPerPass)
	}
	if resolved.MaxDuration != 50*time.Millisecond {
		t.Fatalf("MaxDuration = %v, want 50ms", resolved.MaxDuration)
	}
}

func TestResolveOrphanSweepHonoursDeclaredLeaves(t *testing.T) {
	off := false
	resolved := EventsSettings{OrphanSweep: EventsOrphanSweepSettings{
		Enabled:              &off,
		IntervalMinutes:      5,
		RetryIntervalMinutes: 2,
		BatchRows:            7,
		MaxRowsPerPass:       9,
		MaxDurationMs:        11,
	}}.ResolveOrphanSweep()

	if resolved.Enabled {
		t.Fatalf("Enabled = true, want false (explicit disable must win)")
	}
	if resolved.Interval != 5*time.Minute || resolved.RetryInterval != 2*time.Minute {
		t.Fatalf("cadence = %v/%v, want 5m/2m", resolved.Interval, resolved.RetryInterval)
	}
	if resolved.BatchRows != 7 || resolved.MaxRowsPerPass != 9 {
		t.Fatalf("rows = %d/%d, want 7/9", resolved.BatchRows, resolved.MaxRowsPerPass)
	}
	if resolved.MaxDuration != 11*time.Millisecond {
		t.Fatalf("MaxDuration = %v, want 11ms", resolved.MaxDuration)
	}
}

func TestNormalizeEventsOrphanSweepInheritsKit(t *testing.T) {
	off := false
	kit := Settings{Events: EventsSettings{OrphanSweep: EventsOrphanSweepSettings{
		Enabled:              &off,
		IntervalMinutes:      30,
		RetryIntervalMinutes: 3,
		BatchRows:            4,
		MaxRowsPerPass:       40,
		MaxDurationMs:        12,
	}}}
	cfg := Settings{Events: EventsSettings{OrphanSweep: EventsOrphanSweepSettings{BatchRows: 8}}}

	NormalizeEventsOrphanSweep(&cfg, kit)

	got := cfg.Events.OrphanSweep
	if got.BatchRows != 8 {
		t.Fatalf("BatchRows = %d, want 8 (declared leaf must survive)", got.BatchRows)
	}
	if got.IntervalMinutes != 30 || got.RetryIntervalMinutes != 3 || got.MaxRowsPerPass != 40 || got.MaxDurationMs != 12 {
		t.Fatalf("kit inheritance failed: %+v", got)
	}
	if got.Enabled == nil || *got.Enabled {
		t.Fatalf("Enabled = %v, want inherited false", got.Enabled)
	}
	// The kit pointer must not be aliased — editing the resolved config
	// would otherwise mutate the shared kit baseline.
	*got.Enabled = true
	if kit.Events.OrphanSweep.Enabled == nil || *kit.Events.OrphanSweep.Enabled {
		t.Fatalf("kit Enabled pointer was aliased into cfg")
	}
}

func TestValidateEventsOrphanSweepBounds(t *testing.T) {
	cases := []struct {
		name     string
		settings EventsOrphanSweepSettings
		wantErr  bool
	}{
		{name: "empty inherits floor", settings: EventsOrphanSweepSettings{}},
		{name: "canonical", settings: EventsOrphanSweepSettings{
			IntervalMinutes: 1440, RetryIntervalMinutes: 60, BatchRows: 100, MaxRowsPerPass: 500, MaxDurationMs: 50,
		}},
		{name: "negative interval", settings: EventsOrphanSweepSettings{IntervalMinutes: -1}, wantErr: true},
		{name: "negative retry", settings: EventsOrphanSweepSettings{RetryIntervalMinutes: -1}, wantErr: true},
		{name: "negative batch", settings: EventsOrphanSweepSettings{BatchRows: -1}, wantErr: true},
		{name: "negative pass cap", settings: EventsOrphanSweepSettings{MaxRowsPerPass: -1}, wantErr: true},
		{name: "negative duration", settings: EventsOrphanSweepSettings{MaxDurationMs: -1}, wantErr: true},
		{name: "pass cap below batch", settings: EventsOrphanSweepSettings{BatchRows: 100, MaxRowsPerPass: 10}, wantErr: true},
		{name: "pass cap equal to batch", settings: EventsOrphanSweepSettings{BatchRows: 100, MaxRowsPerPass: 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEventsOrphanSweep(tc.settings)
			if tc.wantErr && err == nil {
				t.Fatalf("validateEventsOrphanSweep(%+v) = nil, want error", tc.settings)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateEventsOrphanSweep(%+v) = %v, want nil", tc.settings, err)
			}
		})
	}
}

// TestOrphanSweepKitMatchesCanonicalDefaults pins the embedded kit YAML
// to the code floor so an edit to one without the other fails loudly
// instead of silently shipping two different published policies.
func TestOrphanSweepKitMatchesCanonicalDefaults(t *testing.T) {
	kit, err := LoadKitConfig()
	if err != nil {
		t.Fatalf("LoadKitConfig: %v", err)
	}
	got := kit.Events.OrphanSweep
	if got.Enabled == nil || !*got.Enabled {
		t.Fatalf("kit orphan_sweep.enabled = %v, want true", got.Enabled)
	}
	if got.IntervalMinutes != DefaultOrphanSweepIntervalMinutes {
		t.Fatalf("kit interval_minutes = %d, want %d", got.IntervalMinutes, DefaultOrphanSweepIntervalMinutes)
	}
	if got.RetryIntervalMinutes != DefaultOrphanSweepRetryIntervalMinutes {
		t.Fatalf("kit retry_interval_minutes = %d, want %d", got.RetryIntervalMinutes, DefaultOrphanSweepRetryIntervalMinutes)
	}
	if got.BatchRows != DefaultOrphanSweepBatchRows {
		t.Fatalf("kit batch_rows = %d, want %d", got.BatchRows, DefaultOrphanSweepBatchRows)
	}
	if got.MaxRowsPerPass != DefaultOrphanSweepMaxRowsPerPass {
		t.Fatalf("kit max_rows_per_pass = %d, want %d", got.MaxRowsPerPass, DefaultOrphanSweepMaxRowsPerPass)
	}
	if got.MaxDurationMs != DefaultOrphanSweepMaxDurationMs {
		t.Fatalf("kit max_duration_ms = %d, want %d", got.MaxDurationMs, DefaultOrphanSweepMaxDurationMs)
	}
}

// TestOrphanSweepReachesTheImmutableSnapshot proves AC 1's "expose them
// through the immutable config snapshot": the block must be readable
// from a built Snapshot both structurally and in the flattened
// effective-configuration view the settings screen renders.
func TestOrphanSweepReachesTheImmutableSnapshot(t *testing.T) {
	kit, err := LoadKitConfig()
	if err != nil {
		t.Fatalf("LoadKitConfig: %v", err)
	}
	snap := BuildSnapshot(Bundle{Config: kit})

	if got := snap.Events().ResolveOrphanSweep(); got.MaxRowsPerPass != DefaultOrphanSweepMaxRowsPerPass {
		t.Fatalf("snapshot ResolveOrphanSweep().MaxRowsPerPass = %d, want %d", got.MaxRowsPerPass, DefaultOrphanSweepMaxRowsPerPass)
	}
	want := map[string]string{
		"orphan_sweep.enabled":                "true",
		"orphan_sweep.interval_minutes":       "1440",
		"orphan_sweep.retry_interval_minutes": "60",
		"orphan_sweep.batch_rows":             "100",
		"orphan_sweep.max_rows_per_pass":      "500",
		"orphan_sweep.max_duration_ms":        "50",
	}
	for _, tuple := range snap.EffectiveTuples() {
		if tuple.Section != "events" {
			continue
		}
		if expected, ok := want[tuple.Key]; ok {
			if tuple.Value != expected {
				t.Fatalf("EffectiveTuples events.%s = %q, want %q", tuple.Key, tuple.Value, expected)
			}
			delete(want, tuple.Key)
		}
	}
	if len(want) > 0 {
		t.Fatalf("orphan_sweep keys missing from EffectiveTuples: %v", want)
	}
}
