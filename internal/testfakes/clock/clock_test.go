package clock

import (
	"sync"
	"testing"
	"time"
)

// TestFakeAdvanceMovesClock locks the core contract: New seeds the
// fake at the supplied instant, Now reports it verbatim, and Advance
// shifts the reading by exactly the duration passed in. Without this
// the entire fake-clock substitution pattern would silently drift.
func TestFakeAdvanceMovesClock(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := New(start)

	if got := f.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}

	f.Advance(2 * time.Hour)
	want := start.Add(2 * time.Hour)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(2h) Now() = %v, want %v", got, want)
	}

	// Negative durations rewind — explicit assertion so callers can
	// rely on it when modelling "since" floors backwards from a known
	// anchor.
	f.Advance(-30 * time.Minute)
	want = start.Add(2*time.Hour - 30*time.Minute)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(-30m) Now() = %v, want %v", got, want)
	}
}

// TestFakeNowIsConcurrencySafe stresses Now/Advance under -race so
// the documented "safe for concurrent use" claim is verified by the
// race detector rather than left as a comment-only promise. The loop
// counts are intentionally small — the goal is to surface a race, not
// benchmark throughput.
func TestFakeNowIsConcurrencySafe(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := New(start)

	const goroutines = 8
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				f.Advance(time.Millisecond)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = f.Now()
			}
		}()
	}
	wg.Wait()

	// Final reading must match start + (goroutines * iterations) ms.
	// Verifies Advance is commutative under concurrency — the mutex
	// makes every increment land, no lost updates.
	want := start.Add(time.Duration(goroutines*iterations) * time.Millisecond)
	if got := f.Now(); !got.Equal(want) {
		t.Fatalf("after concurrent Advance Now() = %v, want %v", got, want)
	}
}
