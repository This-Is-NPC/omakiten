package sqlite

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/domain"
)

func TestSearchTriggerDiscoveryIgnoresReadOnlyAndRowIdentifierReferences(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	execIntegritySQL(t, ctx, store, `
CREATE TRIGGER unrelated_search_reader AFTER INSERT ON tags BEGIN
  SELECT NEW.search_index;
  SELECT COUNT(*) FROM search_index;
END`)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if !report.Healthy || report.Triggers.UnexpectedCount != 0 {
		t.Fatalf("read-only unrelated trigger was claimed by search repair: %+v", report.Triggers)
	}
	if _, err := store.ReindexSearchConfirmed(ctx, false); err != nil {
		t.Fatalf("reconstructive no-op rejected unrelated trigger: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'unrelated_search_reader'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unrelated trigger after reindex = %d, %v", count, err)
	}
}

func TestSearchTriggerDiscoveryRequiresBackupForArbitraryQuotedDMLTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	execIntegritySQL(t, ctx, store, `
CREATE TRIGGER arbitrary_poison AFTER INSERT ON tags BEGIN
  DELETE FROM "search_index" WHERE entity_type = 'task';
END`)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.Healthy || report.Triggers.UnexpectedCount != 1 || !report.RequiresBackupBeforeRepair() {
		t.Fatalf("quoted DML target classification = %+v requires_backup=%v", report.Triggers, report.RequiresBackupBeforeRepair())
	}
	result, err := store.ReindexSearchConfirmed(ctx, false)
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation || !result.BackupRecommended {
		t.Fatalf("unconfirmed poisoning repair = %+v, %v", result, err)
	}
	var before int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'arbitrary_poison'`).Scan(&before); err != nil || before != 1 {
		t.Fatalf("poison trigger changed before confirmation = %d, %v", before, err)
	}
	confirmed, err := store.ReindexSearchConfirmed(ctx, true)
	if err != nil || !confirmed.After.Healthy {
		t.Fatalf("confirmed poisoning repair = %+v, %v", confirmed, err)
	}
	task, err := store.CreateTask(ctx, project.ID, "post repair health", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask after repair: %v", err)
	}
	hits, err := store.Search(ctx, "health", project.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil || len(hits) != 1 || hits[0].ID != task.ID {
		t.Fatalf("post-reindex source-write health = %+v, %v", hits, err)
	}
}

func TestStaleCanonicalTriggerRequiresConfirmation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
	execIntegritySQL(t, ctx, store, `CREATE TRIGGER search_index_tasks_ai AFTER INSERT ON tasks BEGIN SELECT 1; END`)
	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if len(report.Triggers.Stale) != 1 || !report.RequiresBackupBeforeRepair() {
		t.Fatalf("stale trigger did not require recovery backup: %+v", report.Triggers)
	}
	if _, err := store.ReindexSearchConfirmed(ctx, false); err == nil {
		t.Fatal("stale trigger was removed without confirmation")
	}
}

func TestTriggerSQLTargetsSearchIndexOnlyForDMLBodies(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		`CREATE TRIGGER x AFTER INSERT ON tags BEGIN SELECT NEW.search_index; END`:                       false,
		`CREATE TRIGGER x AFTER INSERT ON tags BEGIN SELECT * FROM search_index; END`:                    false,
		`CREATE TRIGGER x AFTER INSERT ON tags BEGIN INSERT INTO search_index VALUES ('x'); END`:         true,
		`CREATE TRIGGER x AFTER INSERT ON tags BEGIN REPLACE INTO [search_index_shadow] VALUES (1); END`: true,
		"CREATE TRIGGER x AFTER INSERT ON tags BEGIN UPDATE `search_index` SET content = 'x'; END":       true,
		`CREATE TRIGGER x AFTER INSERT ON tags BEGIN DELETE FROM "search_index_data"; END`:               true,
	}
	for sql, want := range tests {
		if got := triggerSQLTargetsSearchIndex(sql); got != want {
			t.Errorf("triggerSQLTargetsSearchIndex(%q) = %v, want %v", sql, got, want)
		}
	}
}
