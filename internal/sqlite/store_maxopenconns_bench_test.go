package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
)

// db is an unexported field on *Store — the benchmark lives in
// package sqlite so it reaches the field directly without exposing
// a public DB() accessor production code would have no reason to
// call.

// BenchmarkStoreMaxOpenConnsContention exists to track db.Stats().Wait*
// metrics under realistic TUI+MCP+hook concurrency so the W7 #225
// decision to lower MaxOpenConns 4 → 2 can be re-evaluated against
// real data instead of intuition. It is intentionally not asserted
// against a specific WaitCount threshold — the benchmark reports the
// measurement; an operator runs `go test -bench=. -benchmem -benchtime=5s`
// against this package to surface the queue depth before locking the
// value in or restoring it. b.ReportMetric exposes the metric so
// benchstat output keeps the value alongside ns/op.
//
// The concurrency mix mirrors a TUI session: K goroutines run a
// read-heavy List loop, two goroutines run a Create/Move loop
// (writers contending for the single-writer connection), one goroutine
// resolves a project via UpsertProject (hook-pattern). All four
// surfaces share the *Store the production composition root would
// hand them, so the SetMaxOpenConns floor is the only knob that
// affects WaitCount.
func BenchmarkStoreMaxOpenConnsContention(b *testing.B) {
	ctx := context.Background()
	fixture := openStoreFixture(b, filepath.Join(b.TempDir(), "bench.db"))
	bundle, _ := testfixtures.LoadBundle(b, "kanban_three_buckets.yaml")
	fixture.applyBundle(bundle)
	project, err := fixture.UpsertProject(ctx, "Bench", "bench", b.TempDir())
	if err != nil {
		b.Fatalf("UpsertProject: %v", err)
	}

	// Seed a handful of tasks so the read loop has rows to scan; keeps
	// the read path comparable to a project that actually has work.
	for i := 0; i < 16; i++ {
		if _, err := fixture.CreateTask(ctx, project.ID, "seed", "", domain.Priority(2), "todo", nil, fixture.snap()); err != nil {
			b.Fatalf("seed CreateTask: %v", err)
		}
	}

	const readers = 4
	const writers = 2
	statsBefore := fixture.db.Stats()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		var wg sync.WaitGroup
		for r := 0; r < readers; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = fixture.ListTasks(ctx, project.ID, domain.TaskFilter{}, fixture.snap())
			}()
		}
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = fixture.CreateTask(ctx, project.ID, "concurrent", "", domain.Priority(2), "todo", nil, fixture.snap())
			}()
		}
		wg.Wait()
	}

	b.StopTimer()
	statsAfter := fixture.db.Stats()
	b.ReportMetric(float64(statsAfter.WaitCount-statsBefore.WaitCount), "wait-count")
	b.ReportMetric(float64(statsAfter.WaitDuration-statsBefore.WaitDuration)/1e6, "wait-ms")
	b.ReportMetric(float64(statsAfter.MaxOpenConnections), "max-open-conns")
}
