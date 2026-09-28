package sqlite

import (
	"context"
	"fmt"
	"io"
	"time"

	"omakiten/internal/config"
)

// orphanSweepDeleteBatchSQL removes one bounded batch of orphaned event
// rows and reports the ids it removed so the caller can advance the
// within-pass cursor.
//
// Shape notes, each of which is load-bearing:
//
//   - `e.project_id > 0` is the only in-scope predicate. NULL project_id
//     (global/system rows) and 0 (the "unscoped" sentinel some writers
//     use) are structurally not orphans and must survive untouched.
//   - The LEFT JOIN anti-join matches against the raw `projects` table
//     with no `archived_at` filter: an archived project still has a row,
//     so its events are live data, not orphans.
//   - `e.id > ?` is the within-pass cursor. It only ever moves forward
//     inside one pass and resets to 0 on the next pass, so a row that a
//     concurrent writer inserts BELOW the cursor is still reachable on
//     the following pass.
//   - ORDER BY e.id ASC + LIMIT gives the pass a deterministic, oldest-
//     first shape, which keeps the batch bounded and the cursor monotonic.
//   - The outer `project_id > 0` + NOT EXISTS clauses are the anti-join
//     recheck. The inner SELECT is evaluated as a subquery; the recheck
//     re-asserts the orphan predicate against the row actually being
//     deleted so a project row that reappeared cannot be swept away by a
//     stale candidate id.
//   - No age grace: an orphan is an orphan the moment its project row is
//     gone. A grace window would only delay reconciliation while leaving
//     the same rows visible to cross-project search and metrics.
//
// The statement runs outside an explicit transaction, so SQLite wraps it
// in one implicit transaction per batch — a crash mid-pass leaves whole
// batches committed and the rest untouched, never a half-deleted batch.
const orphanSweepDeleteBatchSQL = `
DELETE FROM events
WHERE id IN (
  SELECT e.id
  FROM events e
  LEFT JOIN projects p ON p.id = e.project_id
  WHERE e.project_id > 0 AND p.id IS NULL AND e.id > ?
  ORDER BY e.id ASC
  LIMIT ?
)
AND project_id > 0
AND NOT EXISTS (SELECT 1 FROM projects r WHERE r.id = events.project_id)
RETURNING id
`

// OrphanSweepReport is the internal outcome of one sweep pass. It is
// deliberately not surfaced on any user-facing surface: a successful
// sweep is silent maintenance, and callers that want the numbers read
// this value directly (tests, future diagnostics).
type OrphanSweepReport struct {
	// DeletedRows is how many event rows the pass removed.
	DeletedRows int
	// Batches is how many DELETE statements the pass issued.
	Batches int
	// Elapsed is the wall-clock time the pass consumed.
	Elapsed time.Duration
	// Limited reports that the pass stopped on a cap (rows or wall
	// clock) rather than because it drained every orphan. A limited
	// pass reschedules on the retry cadence.
	Limited bool
	// LastID is the highest event id the pass removed — the final value
	// of the within-pass cursor. Zero when nothing was deleted. It is
	// NOT carried into the next pass; see orphanSweepDeleteBatchSQL.
	LastID int64
}

// SetOrphanSweepPolicy installs the resolved reconciliation policy and
// arms the opportunistic schedule one interval out. Arming (rather than
// marking the sweep immediately due) is deliberate: the composition root
// runs a forced pass through ApplyConfig right after installing the
// policy, so an immediately-due schedule would only duplicate that work
// on the very next activity-log write.
func (s *Store) SetOrphanSweepPolicy(policy config.ResolvedOrphanSweep) {
	s.orphanSweepMu.Lock()
	defer s.orphanSweepMu.Unlock()
	s.orphanSweepPolicy = policy
	s.orphanSweepNextDue = s.orphanSweepClock().Add(policy.Interval)
}

// SetOrphanSweepWarnWriter overrides the sink that receives sweep
// failures. The default is io.Discard, and that default is a hard
// requirement rather than an oversight: the sweep runs inline on
// whichever goroutine wrote an activity-log row, which under `okt tui`
// is the bubbletea render loop with the alternate screen held on
// stdout. A stray write to stdout or stderr from there leaks under the
// render and corrupts the frame. Operators who want the diagnostics
// point this at a file or an in-memory buffer; nothing writes to a
// terminal stream by default.
func (s *Store) SetOrphanSweepWarnWriter(w io.Writer) {
	s.orphanSweepMu.Lock()
	defer s.orphanSweepMu.Unlock()
	if w == nil {
		w = io.Discard
	}
	s.orphanSweepWarn = w
}

// orphanSweepClock returns the time source. Tests replace the field to
// drive cadence and the wall-clock cap deterministically.
func (s *Store) orphanSweepClock() time.Time {
	if s.orphanSweepNow != nil {
		return s.orphanSweepNow()
	}
	return time.Now()
}

// SweepOrphanEvents runs one bounded reconciliation pass immediately,
// ignoring the cadence. Used by ApplyConfig so a freshly composed
// runtime reconciles once at startup, and by tests.
//
// This is the reconciliation half of project deletion, not a substitute
// for it: DeleteProject / DeleteProjectWithBackup already remove every
// event row of a project inside the deletion transaction. The sweep only
// collects what a process that bypassed that sequence left behind.
func (s *Store) SweepOrphanEvents(ctx context.Context) (OrphanSweepReport, error) {
	return s.runOrphanSweep(ctx)
}

// sweepOrphanEventsIfDue runs a pass only when the schedule says it is
// due. It never blocks on a sweep already in flight and never surfaces
// an error to its caller — it hangs off the activity-log write path,
// where maintenance must not break the operation that triggered it.
func (s *Store) sweepOrphanEventsIfDue(ctx context.Context) {
	if !s.orphanSweepDue() {
		return
	}
	if _, err := s.runOrphanSweep(ctx); err != nil {
		s.warnOrphanSweep(err)
	}
}

// orphanSweepDue reports whether the opportunistic path should run now.
func (s *Store) orphanSweepDue() bool {
	s.orphanSweepMu.Lock()
	defer s.orphanSweepMu.Unlock()
	if !orphanSweepRunnable(s.orphanSweepPolicy) {
		return false
	}
	return !s.orphanSweepClock().Before(s.orphanSweepNextDue)
}

// warnOrphanSweep writes a failure line to the configured sink. See
// SetOrphanSweepWarnWriter for why the default sink is io.Discard.
func (s *Store) warnOrphanSweep(err error) {
	s.orphanSweepMu.Lock()
	sink := s.orphanSweepWarn
	s.orphanSweepMu.Unlock()
	if sink == nil {
		return
	}
	fmt.Fprintf(sink, "warning: orphan-event sweep failed: %s\n", err.Error())
}

// runOrphanSweep executes one pass under the in-process sweep guard and
// reschedules the opportunistic path from the outcome.
//
// The guard is non-blocking on purpose. A second caller arriving while a
// pass is in flight returns an empty report rather than queueing behind
// it: the work it wanted done is already happening, and blocking would
// push SQLite contention onto a caller that only wanted to write an
// activity-log row. Cross-process concurrency needs no coordination —
// two processes sweeping at once simply race on the same DELETE, and the
// loser deletes zero rows because the anti-join recheck no longer
// matches.
func (s *Store) runOrphanSweep(ctx context.Context) (OrphanSweepReport, error) {
	policy := s.orphanSweepPolicySnapshot()
	if !orphanSweepRunnable(policy) {
		return OrphanSweepReport{}, nil
	}
	if !s.orphanSweepRunning.CompareAndSwap(false, true) {
		return OrphanSweepReport{}, nil
	}
	defer s.orphanSweepRunning.Store(false)

	report, err := s.orphanSweepPass(ctx, policy)
	s.rescheduleOrphanSweep(policy, report, err)
	return report, err
}

// orphanSweepRunnable reports whether a policy can drive a terminating
// pass. The config resolver and validator already guarantee positive
// bounds, so this is defence in depth for a policy handed straight to
// SetOrphanSweepPolicy: a zero batch size would make every DELETE remove
// zero rows without ever satisfying the drained exit, and the pass would
// spin on the caller's goroutine — which under `okt tui` is the render
// loop. Treat any non-positive bound as "not configured" and do nothing.
func orphanSweepRunnable(policy config.ResolvedOrphanSweep) bool {
	return policy.Enabled &&
		policy.BatchRows > 0 &&
		policy.MaxRowsPerPass > 0 &&
		policy.MaxDuration > 0 &&
		policy.Interval > 0 &&
		policy.RetryInterval > 0
}

func (s *Store) orphanSweepPolicySnapshot() config.ResolvedOrphanSweep {
	s.orphanSweepMu.Lock()
	defer s.orphanSweepMu.Unlock()
	return s.orphanSweepPolicy
}

// rescheduleOrphanSweep arms the next opportunistic pass: the steady
// interval after a pass that drained cleanly, the shorter retry cadence
// after a pass that hit a cap or failed. A capped pass leaves work
// behind, and an errored pass leaves an unknown amount behind — both
// want the next look sooner than the steady state, and neither wants a
// busy loop.
func (s *Store) rescheduleOrphanSweep(policy config.ResolvedOrphanSweep, report OrphanSweepReport, err error) {
	next := policy.Interval
	if err != nil || report.Limited {
		next = policy.RetryInterval
	}
	s.orphanSweepMu.Lock()
	defer s.orphanSweepMu.Unlock()
	s.orphanSweepNextDue = s.orphanSweepClock().Add(next)
}

// orphanSweepPass deletes orphaned events in batches until it drains
// them, exhausts the per-pass row cap, or exhausts the wall-clock
// budget. The cursor lives in this function's frame and dies with it —
// that is the whole of "cursor only within a pass, reset each pass".
func (s *Store) orphanSweepPass(ctx context.Context, policy config.ResolvedOrphanSweep) (OrphanSweepReport, error) {
	start := s.orphanSweepClock()
	report := OrphanSweepReport{}
	var cursor int64

	for report.DeletedRows < policy.MaxRowsPerPass {
		limit := policy.BatchRows
		if remaining := policy.MaxRowsPerPass - report.DeletedRows; remaining < limit {
			limit = remaining
		}
		deleted, highest, err := s.orphanSweepBatch(ctx, cursor, limit)
		if err != nil {
			report.Elapsed = s.orphanSweepClock().Sub(start)
			report.Limited = true
			return report, err
		}
		report.Batches++
		report.DeletedRows += deleted
		if highest > cursor {
			cursor = highest
			report.LastID = highest
		}
		if deleted < limit {
			// Short batch: the anti-join found fewer candidates than the
			// batch could hold, so every orphan visible to this pass is
			// gone. This is the only exit that proves the pass drained.
			report.Elapsed = s.orphanSweepClock().Sub(start)
			return report, nil
		}
		if elapsed := s.orphanSweepClock().Sub(start); elapsed >= policy.MaxDuration {
			report.Elapsed = elapsed
			report.Limited = true
			return report, nil
		}
	}

	report.Elapsed = s.orphanSweepClock().Sub(start)
	report.Limited = true
	return report, nil
}

// orphanSweepBatch issues one bounded DELETE and returns how many rows
// it removed plus the highest id among them.
func (s *Store) orphanSweepBatch(ctx context.Context, cursor int64, limit int) (int, int64, error) {
	rows, err := s.db.QueryContext(ctx, orphanSweepDeleteBatchSQL, cursor, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("sweep orphan events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	deleted := 0
	var highest int64
	// The result set MUST be drained: with DELETE ... RETURNING the
	// statement only finishes stepping as the rows are consumed, so an
	// early break would abandon part of the batch.
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return deleted, highest, fmt.Errorf("sweep orphan events: %w", err)
		}
		deleted++
		if id > highest {
			highest = id
		}
	}
	if err := rows.Err(); err != nil {
		return deleted, highest, fmt.Errorf("sweep orphan events: %w", err)
	}
	return deleted, highest, nil
}
