package sqlite

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestActivityLogCRUD(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	id, err := store.BeginActivityLog(ctx, domain.ActivityLog{
		Source:        domain.ActivitySourceMCP,
		Entrypoint:    "tasks.create_intent",
		Operation:     "app.TaskService.Add",
		ProjectID:     1,
		ProjectSlug:   "test",
		ArgumentsJSON: `{"title":"Test"}`,
		Status:        "running",
	})
	if err != nil {
		t.Fatalf("BeginActivityLog() error = %v", err)
	}
	if id <= 0 {
		t.Fatalf("BeginActivityLog() id = %d, want > 0", id)
	}

	if err := store.FinishActivityLog(ctx, id, "ok", 42, ""); err != nil {
		t.Fatalf("FinishActivityLog() error = %v", err)
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListActivityLogs() len = %d, want 1", len(logs))
	}
	log := logs[0]
	if log.Status != "ok" {
		t.Fatalf("log.Status = %q, want ok", log.Status)
	}
	if log.DurationMs != 42 {
		t.Fatalf("log.DurationMs = %d, want 42", log.DurationMs)
	}
	if log.Source != domain.ActivitySourceMCP {
		t.Fatalf("log.Source = %q, want mcp", log.Source)
	}
}

func TestActivityLogListFilterBySource(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	for _, source := range []domain.ActivitySource{domain.ActivitySourceCLI, domain.ActivitySourceMCP} {
		id, err := store.BeginActivityLog(ctx, domain.ActivityLog{Source: source, Operation: "test", Status: "running"})
		if err != nil {
			t.Fatalf("BeginActivityLog(%s) error = %v", source, err)
		}
		if err := store.FinishActivityLog(ctx, id, "ok", 1, ""); err != nil {
			t.Fatalf("FinishActivityLog() error = %v", err)
		}
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{Source: domain.ActivitySourceMCP, Limit: 10})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListActivityLogs() len = %d, want 1", len(logs))
	}
	if logs[0].Source != domain.ActivitySourceMCP {
		t.Fatalf("log.Source = %q, want mcp", logs[0].Source)
	}
}

func TestActivityLogPruneKeepsNewest(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	for i := 0; i < 5; i++ {
		id, err := store.BeginActivityLog(ctx, domain.ActivityLog{Source: domain.ActivitySourceCLI, Operation: "test", Status: "running"})
		if err != nil {
			t.Fatalf("BeginActivityLog() error = %v", err)
		}
		if err := store.FinishActivityLog(ctx, id, "ok", 1, ""); err != nil {
			t.Fatalf("FinishActivityLog() error = %v", err)
		}
	}

	if err := store.PruneActivityLogs(ctx, 3, 0); err != nil {
		t.Fatalf("PruneActivityLogs() error = %v", err)
	}

	logs, err := store.ListActivityLogs(ctx, domain.ActivityLogFilter{})
	if err != nil {
		t.Fatalf("ListActivityLogs() error = %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("ListActivityLogs() len = %d, want 3", len(logs))
	}
}

func TestActivityLogMigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open() #1 error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() #1 error = %v", err)
	}

	store2, err := Open(ctx, t.TempDir()+"/omakiten2.db")
	if err != nil {
		t.Fatalf("Open() #2 error = %v", err)
	}
	defer func() { _ = store2.Close() }()

	// Sanity: activity_logs table must exist
	var count int
	if err := store2.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name='activity_logs'").Scan(&count); err != nil {
		t.Fatalf("table check error = %v", err)
	}
	if count != 1 {
		t.Fatalf("activity_logs table missing after migration")
	}
}
