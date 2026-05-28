// Package clock provides a deterministic fake clock for tests that
// need to control time without sleeping. Production callers accept a
// `now func() time.Time` field; tests pass Fake.Now to get
// deterministic timestamps and call Advance to move the clock forward.
//
// The package lives under internal/testfakes/ so the import-graph gate
// keeps production code from depending on it accidentally — the
// directory name is the contract. Production constructors continue
// defaulting their `now` parameter to `time.Now`, so tests are the
// only consumers of this Fake.
package clock

import (
	"sync"
	"time"
)

// Fake is a thread-safe deterministic clock. Now reads the current
// fake time; Advance moves the clock forward by the given duration.
// Construct via New so callers cannot land an uninitialised zero-time
// fake by accident.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// New returns a Fake initialised at the given start time. Tests
// typically seed it with a fixed reference timestamp (e.g.
// time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) so assertions can
// compare against the exact reading without any tolerance window.
func New(start time.Time) *Fake {
	return &Fake{now: start}
}

// Now returns the current fake time. Safe for concurrent use — the
// mutex guards the read so concurrent Advance + Now interleavings stay
// race-detector clean.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake clock forward by d. Safe for concurrent use.
// Passing a negative duration rewinds the clock (handy for testing
// "since" floors against a known anchor).
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
