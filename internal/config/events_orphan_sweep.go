package config

import (
	"fmt"
	"time"
)

// Canonical orphan-sweep defaults. They mirror the
// `config.events.orphan_sweep` block shipped in
// defaults/config/modules/base-config.yaml; the kit-parity test in
// events_orphan_sweep_test.go keeps the two in lockstep so a YAML edit
// can never drift silently from the code floor.
//
// The floor exists because the sweep deletes rows: a bundle that omits
// the block (an old user file, a hand-built test fixture) must still
// resolve to the conservative published policy rather than to zero —
// zero would mean "no cadence, no batch, no cap", which is exactly the
// unbounded shape the policy is meant to prevent.
const (
	// DefaultOrphanSweepIntervalMinutes is the 24h cadence between two
	// successful opportunistic passes.
	DefaultOrphanSweepIntervalMinutes = 1440
	// DefaultOrphanSweepRetryIntervalMinutes is the 1h cadence used after
	// a pass that hit a cap or failed, so a backlog drains faster than
	// the steady-state interval without turning into a busy loop.
	DefaultOrphanSweepRetryIntervalMinutes = 60
	// DefaultOrphanSweepBatchRows is how many orphans one DELETE removes.
	DefaultOrphanSweepBatchRows = 100
	// DefaultOrphanSweepMaxRowsPerPass caps the rows a single pass may
	// delete before it yields, regardless of how many orphans remain.
	DefaultOrphanSweepMaxRowsPerPass = 500
	// DefaultOrphanSweepMaxDurationMs caps the wall-clock budget of a
	// single pass. The sweep runs inline on a caller's goroutine, so the
	// budget is deliberately small enough to disappear under a keystroke.
	DefaultOrphanSweepMaxDurationMs = 50
)

// EventsOrphanSweepSettings declares the bounded reconciliation policy
// for event rows whose positive project_id no longer resolves to a
// projects row. Every field is optional in YAML: an omitted leaf
// inherits the active kit's value (NormalizeEventsOrphanSweep) and then
// the canonical constant above (ResolveOrphanSweep), so a partial current
// config keeps working.
//
// This is reconciliation, NOT deletion. Canonical project deletion
// (Store.DeleteProject / DeleteProjectWithBackup) already removes every
// event row for the project inside the same transaction as the projects
// row. The sweep only picks up what a process that bypassed that
// sequence left behind — see docs/internal/data-model.md.
type EventsOrphanSweepSettings struct {
	// Enabled gates the whole sweep. nil inherits (default: on).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// IntervalMinutes is the cadence between two successful passes.
	IntervalMinutes int `yaml:"interval_minutes,omitempty" json:"interval_minutes,omitempty"`
	// RetryIntervalMinutes is the cadence after a capped or failed pass.
	RetryIntervalMinutes int `yaml:"retry_interval_minutes,omitempty" json:"retry_interval_minutes,omitempty"`
	// BatchRows is the row count of one DELETE statement.
	BatchRows int `yaml:"batch_rows,omitempty" json:"batch_rows,omitempty"`
	// MaxRowsPerPass caps the rows one pass may delete in total.
	MaxRowsPerPass int `yaml:"max_rows_per_pass,omitempty" json:"max_rows_per_pass,omitempty"`
	// MaxDurationMs caps the wall-clock budget of one pass.
	MaxDurationMs int `yaml:"max_duration_ms,omitempty" json:"max_duration_ms,omitempty"`
}

// ResolvedOrphanSweep is the materialised policy the SQLite maintenance
// path consumes. Every field is guaranteed positive when Enabled is
// true — the validator rejects negative declarations and the resolver
// substitutes the canonical floor for anything left at zero.
type ResolvedOrphanSweep struct {
	Enabled        bool
	Interval       time.Duration
	RetryInterval  time.Duration
	BatchRows      int
	MaxRowsPerPass int
	MaxDuration    time.Duration
}

// ResolveOrphanSweep materialises the effective sweep policy, applying
// the canonical floor to every leaf the bundle left unset.
func (e EventsSettings) ResolveOrphanSweep() ResolvedOrphanSweep {
	s := e.OrphanSweep
	enabled := true
	if s.Enabled != nil {
		enabled = *s.Enabled
	}
	return ResolvedOrphanSweep{
		Enabled:        enabled,
		Interval:       time.Duration(intOr(s.IntervalMinutes, DefaultOrphanSweepIntervalMinutes)) * time.Minute,
		RetryInterval:  time.Duration(intOr(s.RetryIntervalMinutes, DefaultOrphanSweepRetryIntervalMinutes)) * time.Minute,
		BatchRows:      intOr(s.BatchRows, DefaultOrphanSweepBatchRows),
		MaxRowsPerPass: intOr(s.MaxRowsPerPass, DefaultOrphanSweepMaxRowsPerPass),
		MaxDuration:    time.Duration(intOr(s.MaxDurationMs, DefaultOrphanSweepMaxDurationMs)) * time.Millisecond,
	}
}

// NormalizeEventsOrphanSweep fills every unset orphan-sweep leaf from the
// active kit so a user file that omits the block inherits the preset's
// published policy rather than the bare code floor. Mirrors
// NormalizeEventsRetention's test-fixture inheritance contract and runs in
// the same loader step.
func NormalizeEventsOrphanSweep(cfg *Settings, kit Settings) {
	k := kit.Events.OrphanSweep
	s := &cfg.Events.OrphanSweep
	if s.Enabled == nil && k.Enabled != nil {
		enabled := *k.Enabled
		s.Enabled = &enabled
	}
	s.IntervalMinutes = intOr(s.IntervalMinutes, k.IntervalMinutes)
	s.RetryIntervalMinutes = intOr(s.RetryIntervalMinutes, k.RetryIntervalMinutes)
	s.BatchRows = intOr(s.BatchRows, k.BatchRows)
	s.MaxRowsPerPass = intOr(s.MaxRowsPerPass, k.MaxRowsPerPass)
	s.MaxDurationMs = intOr(s.MaxDurationMs, k.MaxDurationMs)
}

// validateEventsOrphanSweep enforces the positive bounds of the sweep
// policy. Declared leaves may not be negative (a negative batch or cap
// has no defined semantic and would mask a typo), and the resolved
// policy must stay internally coherent: a per-pass cap below the batch
// size would make the batch limit unreachable and the pass shape a lie.
func validateEventsOrphanSweep(s EventsOrphanSweepSettings) error {
	for _, field := range []struct {
		name  string
		value int
	}{
		{"interval_minutes", s.IntervalMinutes},
		{"retry_interval_minutes", s.RetryIntervalMinutes},
		{"batch_rows", s.BatchRows},
		{"max_rows_per_pass", s.MaxRowsPerPass},
		{"max_duration_ms", s.MaxDurationMs},
	} {
		if field.value < 0 {
			return fmt.Errorf("config.events.orphan_sweep.%s: must be > 0 (omit the key to inherit the kit canonical; see defaults/omakiten.yaml)", field.name)
		}
	}
	resolved := EventsSettings{OrphanSweep: s}.ResolveOrphanSweep()
	if resolved.MaxRowsPerPass < resolved.BatchRows {
		return fmt.Errorf("config.events.orphan_sweep: max_rows_per_pass (%d) must be >= batch_rows (%d)", resolved.MaxRowsPerPass, resolved.BatchRows)
	}
	return nil
}

// intOr returns value when it is non-zero, otherwise fallback. Used by
// the orphan-sweep inheritance chain where 0 uniformly means "not
// declared at this layer" (a zero cadence / batch / cap is meaningless,
// so 0 never needs to survive as an explicit value).
func intOr(value, fallback int) int {
	if value != 0 {
		return value
	}
	return fallback
}
