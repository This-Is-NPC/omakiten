package activity

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

type fakeRepo struct {
	beginCalled   bool
	finishCalled  bool
	lastStatus    string
	lastErrorMsg  string
	lastLog       domain.ActivityLog
	beginReturnID int64
	beginErr      error
}

func (f *fakeRepo) BeginActivityLog(_ context.Context, log any) (int64, error) {
	f.beginCalled = true
	f.lastLog = log.(domain.ActivityLog)
	return f.beginReturnID, f.beginErr
}

func (f *fakeRepo) FinishActivityLog(_ context.Context, _ int64, status string, _ int, errMsg string) error {
	f.finishCalled = true
	f.lastStatus = status
	f.lastErrorMsg = errMsg
	return nil
}

func (f *fakeRepo) ListActivityLogs(_ context.Context, _ domain.ActivityLogFilter) ([]domain.ActivityLog, error) {
	return nil, nil
}

func TestTrackLogsSuccess(t *testing.T) {
	repo := &fakeRepo{beginReturnID: 7}
	ctx := WithRepository(context.Background(), repo)
	ctx = WithAgent(ctx, "mcp", "tasks.create", "claude-opus-4-7", "sess-abc")

	project := domain.ProjectContext{ID: 1, Slug: "test"}
	finish := Track(ctx, "app.TaskService.Add", project, map[string]string{"title": "Hello"})
	finish("ok", "")

	if !repo.beginCalled {
		t.Fatal("expected BeginActivityLog to be called")
	}
	if !repo.finishCalled {
		t.Fatal("expected FinishActivityLog to be called")
	}
	if repo.lastStatus != "ok" {
		t.Fatalf("status = %q, want ok", repo.lastStatus)
	}
	if repo.lastLog.Operation != "app.TaskService.Add" {
		t.Fatalf("operation = %q, want app.TaskService.Add", repo.lastLog.Operation)
	}
	if repo.lastLog.Source != domain.ActivitySourceMCP {
		t.Fatalf("source = %q, want mcp", repo.lastLog.Source)
	}
	if repo.lastLog.ProjectSlug != "test" {
		t.Fatalf("project_slug = %q, want test", repo.lastLog.ProjectSlug)
	}
	if repo.lastLog.AgentModel != "claude-opus-4-7" {
		t.Fatalf("agent_model = %q, want claude-opus-4-7", repo.lastLog.AgentModel)
	}
	if repo.lastLog.AgentSessionID != "sess-abc" {
		t.Fatalf("agent_session_id = %q, want sess-abc", repo.lastLog.AgentSessionID)
	}
}

func TestTrackLogsError(t *testing.T) {
	repo := &fakeRepo{beginReturnID: 1}
	ctx := WithRepository(context.Background(), repo)
	ctx = WithAgent(ctx, "cli", "okt add", "", "")

	finish := Track(ctx, "app.TaskService.Add", domain.ProjectContext{}, nil)
	finish("error", "validation failed")

	if repo.lastStatus != "error" {
		t.Fatalf("status = %q, want error", repo.lastStatus)
	}
	if repo.lastErrorMsg != "validation failed" {
		t.Fatalf("error_message = %q, want validation failed", repo.lastErrorMsg)
	}
}

func TrackNoOpWhenRepoMissing(t *testing.T) {
	ctx := WithAgent(context.Background(), "cli", "okt add", "", "")
	finish := Track(ctx, "app.TaskService.Add", domain.ProjectContext{}, nil)
	finish("ok", "")
	// Should not panic
}

func TestTrackTruncatesLargeJSON(t *testing.T) {
	repo := &fakeRepo{beginReturnID: 1}
	ctx := WithRepository(context.Background(), repo)
	ctx = WithAgent(ctx, "mcp", "tasks.create", "claude-opus-4-7", "sess-abc")

	large := make(map[string]string)
	for i := 0; i < 1000; i++ {
		large[string(rune(i))] = string(make([]byte, 100))
	}
	finish := Track(ctx, "app.TaskService.Add", domain.ProjectContext{}, large)
	finish("ok", "")

	if len(repo.lastLog.ArgumentsJSON) > 2048 {
		t.Fatalf("arguments_json len = %d, want <= 2048", len(repo.lastLog.ArgumentsJSON))
	}
}

func TestTrackHandlesUnserializable(t *testing.T) {
	repo := &fakeRepo{beginReturnID: 1}
	ctx := WithRepository(context.Background(), repo)
	ctx = WithAgent(ctx, "mcp", "tasks.create", "claude-opus-4-7", "sess-abc")

	finish := Track(ctx, "app.TaskService.Add", domain.ProjectContext{}, make(chan int))
	finish("ok", "")

	if repo.lastLog.ArgumentsJSON != "<unserializable>" {
		t.Fatalf("arguments_json = %q, want <unserializable>", repo.lastLog.ArgumentsJSON)
	}
}

func TestTrackBeginFailureIsNoOp(t *testing.T) {
	repo := &fakeRepo{beginReturnID: 0, beginErr: errors.New("db down")}
	ctx := WithRepository(context.Background(), repo)
	ctx = WithAgent(ctx, "mcp", "tasks.create", "claude-opus-4-7", "sess-abc")

	finish := Track(ctx, "app.TaskService.Add", domain.ProjectContext{}, nil)
	finish("ok", "")

	if repo.finishCalled {
		t.Fatal("expected FinishActivityLog NOT to be called after Begin failure")
	}
}

// TestTrackHonorsWithoutTracking covers the contract the TUI's
// per-second realtime tick relies on: a context marked with
// `WithoutTracking` is treated as no-op even when a repository is
// attached, so refresh-driven app-service calls do not pollute the
// activity log or the per-agent metrics.
func TestTrackHonorsWithoutTracking(t *testing.T) {
	repo := &fakeRepo{beginReturnID: 1}
	ctx := WithRepository(context.Background(), repo)
	ctx = WithAgent(ctx, "tui", "tui", "human", "")
	ctx = WithoutTracking(ctx)

	finish := Track(ctx, "app.MetricsService.Summary", domain.ProjectContext{ID: 1, Slug: "test"}, nil)
	finish("ok", "")

	if repo.beginCalled {
		t.Fatal("BeginActivityLog must not run for a no-track context")
	}
	if repo.finishCalled {
		t.Fatal("FinishActivityLog must not run for a no-track context")
	}
}
