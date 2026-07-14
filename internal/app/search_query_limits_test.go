package app

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type cappedSearchRepository struct {
	called bool
}

func (r *cappedSearchRepository) Search(context.Context, string, int64, []domain.SearchEntityType) ([]domain.SearchHit, error) {
	r.called = true
	return nil, nil
}

type cappedSearchActivityRepository struct {
	beginCalls int
	lastLog    domain.ActivityLog
}

func (r *cappedSearchActivityRepository) BeginActivityLog(_ context.Context, value any) (int64, error) {
	r.beginCalls++
	r.lastLog = value.(domain.ActivityLog)
	return 1, nil
}

func (*cappedSearchActivityRepository) FinishActivityLog(context.Context, int64, string, int, string) error {
	return nil
}

func TestSearchServiceNormalizesEntityTypesBeforeTelemetry(t *testing.T) {
	t.Parallel()

	repo := &cappedSearchRepository{}
	telemetry := &cappedSearchActivityRepository{}
	ctx := activity.WithRepository(context.Background(), telemetry)
	_, err := NewSearchService(repo, nil).Search(ctx, domain.ProjectContext{}, "term", []string{" error ", "error", "", "task"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	const want = `{"entity_types":["error","task"],"query":"term"}`
	if telemetry.lastLog.ArgumentsJSON != want {
		t.Fatalf("telemetry arguments = %s, want %s", telemetry.lastLog.ArgumentsJSON, want)
	}
}

func TestSearchServiceRejectsEntityTypesBeforeTelemetryAndRepository(t *testing.T) {
	t.Parallel()

	repo := &cappedSearchRepository{}
	telemetry := &cappedSearchActivityRepository{}
	ctx := activity.WithRepository(context.Background(), telemetry)
	if _, err := NewSearchService(repo, nil).Search(ctx, domain.ProjectContext{}, "term", []string{"retired"}); err == nil {
		t.Fatal("invalid entity type was accepted")
	}
	if repo.called || telemetry.beginCalls != 0 {
		t.Fatalf("invalid entity type reached repository=%v telemetry=%d", repo.called, telemetry.beginCalls)
	}
}

func TestNormalizeEntityTypesCapsOutputCapacity(t *testing.T) {
	t.Parallel()

	raw := make([]string, 1000)
	for index := range raw {
		raw[index] = "task"
	}
	types, err := normalizeEntityTypes(raw)
	if err != nil {
		t.Fatalf("normalizeEntityTypes: %v", err)
	}
	if cap(types) > len(domain.AllSearchEntityTypes()) {
		t.Fatalf("normalized capacity = %d, want <= %d", cap(types), len(domain.AllSearchEntityTypes()))
	}
}

func (*cappedSearchActivityRepository) ListActivityLogs(context.Context, domain.ActivityLogFilter) ([]domain.ActivityLog, error) {
	return nil, nil
}

func (*cappedSearchActivityRepository) ActivityLogStats(context.Context, domain.ActivityLogFilter) (domain.ActivityLogStats, error) {
	return domain.ActivityLogStats{}, nil
}

func TestSearchServiceRejectsAmplificationBeforeTelemetryAndRepository(t *testing.T) {
	t.Parallel()

	repo := &cappedSearchRepository{}
	telemetry := &cappedSearchActivityRepository{}
	ctx := activity.WithRepository(context.Background(), telemetry)
	query := strings.Repeat("term OR ", domain.SearchQueryMaxTokens/2+1) + "term"

	if _, err := NewSearchService(repo, nil).Search(ctx, domain.ProjectContext{}, query, nil); err == nil {
		t.Fatal("oversized lexical query was accepted")
	}
	if repo.called {
		t.Fatal("oversized query reached SearchRepository")
	}
	if telemetry.beginCalls != 0 {
		t.Fatalf("oversized query persisted telemetry %d times", telemetry.beginCalls)
	}
}
