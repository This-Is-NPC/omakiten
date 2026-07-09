package app

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

// fakeInsightsRepo records the args it was called with and returns a canned
// result/error, so the service-layer tests can assert the orchestration
// (default threshold, pass-through) without standing up a SQLite store —
// the real query correctness is covered by internal/sqlite/insights_test.go.
type fakeInsightsRepo struct {
	gotProjectID    int64
	gotStuckDays    int
	gotStuckBuckets []int64
	ret             domain.Insights
	err             error
	calls           int
}

func (f *fakeInsightsRepo) Insights(_ context.Context, projectID int64, stuckDays int, stuckBuckets []int64) (domain.Insights, error) {
	f.calls++
	f.gotProjectID = projectID
	f.gotStuckDays = stuckDays
	f.gotStuckBuckets = stuckBuckets
	return f.ret, f.err
}

func TestInsightsServiceTodayPassesThroughArgs(t *testing.T) {
	repo := &fakeInsightsRepo{ret: domain.Insights{StuckDays: 14}}
	svc := NewInsightsService(repo)

	got, err := svc.Today(context.Background(), domain.ProjectContext{ID: 7}, 7, 14, []int64{5, 6})
	if err != nil {
		t.Fatalf("Today error = %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("repo called %d times, want 1", repo.calls)
	}
	if repo.gotProjectID != 7 {
		t.Fatalf("repo projectID = %d, want 7", repo.gotProjectID)
	}
	if repo.gotStuckDays != 14 {
		t.Fatalf("repo stuckDays = %d, want 14", repo.gotStuckDays)
	}
	if len(repo.gotStuckBuckets) != 2 || repo.gotStuckBuckets[0] != 5 || repo.gotStuckBuckets[1] != 6 {
		t.Fatalf("repo stuckBuckets = %v, want [5 6] (pass-through)", repo.gotStuckBuckets)
	}
	if got.StuckDays != 14 {
		t.Fatalf("result StuckDays = %d, want 14 (pass-through)", got.StuckDays)
	}
}

func TestInsightsServiceTodayDefaultsStuckDays(t *testing.T) {
	repo := &fakeInsightsRepo{}
	svc := NewInsightsService(repo)

	for _, in := range []int{0, -3} {
		repo.calls = 0
		if _, err := svc.Today(context.Background(), domain.ProjectContext{}, 0, in, nil); err != nil {
			t.Fatalf("Today(stuckDays=%d) error = %v", in, err)
		}
		if repo.gotStuckDays != DefaultStuckDays {
			t.Fatalf("stuckDays=%d not defaulted: repo got %d, want %d", in, repo.gotStuckDays, DefaultStuckDays)
		}
	}
}

func TestInsightsServiceTodayPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	repo := &fakeInsightsRepo{err: sentinel}
	svc := NewInsightsService(repo)

	if _, err := svc.Today(context.Background(), domain.ProjectContext{}, 0, 7, nil); !errors.Is(err, sentinel) {
		t.Fatalf("Today error = %v, want %v", err, sentinel)
	}
}

// TestInsightsServiceTodayIntegration drives the service against the real
// SQLite store (via the app test fixture) so the wiring — service ->
// InsightsRepository (*snapstore.Store embeds *sqlite.Store) — is exercised
// end to end. We assert the empty-state contract: a fresh store reports
// HasData=false on every insight rather than a silent zero.
func TestInsightsServiceTodayIntegration(t *testing.T) {
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	svc := NewInsightsService(store)
	got, err := svc.Today(context.Background(), project.Context(), 0, 0, nil)
	if err != nil {
		t.Fatalf("Today error = %v", err)
	}
	if got.StuckDays != DefaultStuckDays {
		t.Fatalf("StuckDays = %d, want %d", got.StuckDays, DefaultStuckDays)
	}
	if got.Stuck.HasData || got.CycleTime.HasData || got.WIP.HasData ||
		got.Guards.HasData || got.ErrorLoop.HasData || got.PerModel.HasData {
		t.Fatalf("fresh store should report HasData=false everywhere: %+v", got)
	}
}
