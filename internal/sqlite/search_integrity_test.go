package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"omakiten/internal/domain"
)

func TestCheckSearchIndexHealthy(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	report, err := store.CheckSearchIndex(context.Background())
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if !report.Healthy {
		t.Fatalf("empty migrated store is unhealthy: %+v", report)
	}
	if report.Triggers.ExpectedCount != 16 || report.Triggers.ActualCount != 16 {
		t.Fatalf("trigger counts = %+v, want 16/16", report.Triggers)
	}
}

func TestCheckSearchIndexRetriesWhenLogicalAndFTSGenerationsDiffer(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "snapshot.db")
	checker := openStoreFixture(t, path)
	checker.applyBundle(sqliteTestBundle(t))
	writer := openStoreFixture(t, path)
	writer.applyBundle(sqliteTestBundle(t))
	project := mustUpsertProject(t, checker, "P", "p", "/work/p")

	canonicalReady := make(chan struct{})
	resume := make(chan struct{})
	checkDone := make(chan struct {
		report domain.SearchIndexIntegrityReport
		err    error
	}, 1)
	var pauseOnce sync.Once
	go func() {
		report, err := checker.checkSearchIndexSnapshotWithHooks(ctx, searchCheckHooks{AfterCanonical: func(int) {
			pauseOnce.Do(func() {
				close(canonicalReady)
				<-resume
			})
		}})
		checkDone <- struct {
			report domain.SearchIndexIntegrityReport
			err    error
		}{report: report, err: err}
	}()
	<-canonicalReady

	if _, err := writer.CreateTask(ctx, project.ID, "committed during check", "body", domain.Priority(2), "backlog", nil, writer.snap()); err != nil {
		close(resume)
		t.Fatalf("CreateTask during check: %v", err)
	}
	close(resume)
	result := <-checkDone
	if result.err != nil {
		t.Fatalf("in-flight CheckSearchIndex: %v", result.err)
	}
	if !result.report.Healthy || result.report.SourceTotal != 1 || result.report.IndexTotal != 1 {
		t.Fatalf("generation-stable report = %+v, want committed retry generation", result.report)
	}

	after, err := checker.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("subsequent CheckSearchIndex: %v", err)
	}
	if !after.Healthy || after.SourceTotal != 1 || after.IndexTotal != 1 {
		t.Fatalf("subsequent report = healthy:%v source:%d index:%d", after.Healthy, after.SourceTotal, after.IndexTotal)
	}
}

func TestCheckSearchIndexRetriesExternalCommitBetweenLogicalAndFTS(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "between-phases.db")
	checker := openStoreFixture(t, path)
	checker.applyBundle(sqliteTestBundle(t))
	writer := openStoreFixture(t, path)
	writer.applyBundle(sqliteTestBundle(t))
	project := mustUpsertProject(t, checker, "P", "p", "/work/p")
	beforeFTSCalls := 0

	report, err := checker.checkSearchIndexSnapshotWithHooks(ctx, searchCheckHooks{BeforeFTS: func(attempt int) {
		beforeFTSCalls++
		if attempt != 1 {
			return
		}
		if _, err := writer.CreateTask(ctx, project.ID, "between phases marker", "body", domain.Priority(2), "backlog", nil, writer.snap()); err != nil {
			t.Fatalf("external commit between logical check and FTS integrity: %v", err)
		}
	}})
	if err != nil {
		t.Fatalf("generation-stable CheckSearchIndex: %v", err)
	}
	if beforeFTSCalls != 2 || !report.Healthy || report.SourceTotal != 1 || report.IndexTotal != 1 {
		t.Fatalf("between-phase retry = calls:%d report:%+v", beforeFTSCalls, report)
	}
}

func TestCheckSearchIndexAbortsAfterBoundedGenerationChanges(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "unstable-check.db")
	checker := openStoreFixture(t, path)
	checker.applyBundle(sqliteTestBundle(t))
	writer := openStoreFixture(t, path)
	writer.applyBundle(sqliteTestBundle(t))
	attempts := 0

	report, err := checker.checkSearchIndexSnapshotWithHooks(ctx, searchCheckHooks{BeforeFTS: func(attempt int) {
		attempts++
		execIntegritySQL(t, ctx, writer, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES (?, 'retired_private_type', ?, 0)`, fmt.Sprintf("private unstable marker %d", attempt), attempt)
	}})
	if err == nil {
		t.Fatalf("unstable generation check succeeded: %+v", report)
	}
	if attempts != searchCheckGenerationAttempts {
		t.Fatalf("generation attempts = %d, want %d", attempts, searchCheckGenerationAttempts)
	}
	if report.Healthy || report.SourceTotal != 0 || report.IndexTotal != 0 || len(report.Types) != 0 {
		t.Fatalf("unstable check returned a mixed report: %+v", report)
	}
	if !strings.Contains(err.Error(), "changed during every integrity check attempt") || strings.Contains(err.Error(), "private unstable marker") {
		t.Fatalf("unsafe instability error: %v", err)
	}
}

func TestCheckSearchIndexReportsMissingCanonicalRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "missing marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID); err != nil {
		t.Fatalf("delete indexed task: %v", err)
	}

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.Healthy {
		t.Fatal("report.Healthy = true, want false")
	}
	tasks := searchIntegrityType(report, domain.SearchEntityTask)
	if tasks.Missing.Count != 1 || len(tasks.Missing.Details) != 1 {
		t.Fatalf("task missing = %+v, want one detail", tasks.Missing)
	}
	if tasks.Missing.Details[0].EntityID != task.ID {
		t.Fatalf("missing entity_id = %d, want %d", tasks.Missing.Details[0].EntityID, task.ID)
	}
}

func TestCheckSearchIndexRejectsMalformedMetadataStorageClasses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "typed marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `
INSERT INTO search_index(content, entity_type, entity_id, project_id)
VALUES ('must not appear', 'task', CAST(? AS TEXT), CAST(? AS TEXT))`, task.ID, project.ID)
	execIntegritySQL(t, ctx, store, `
INSERT INTO search_index(content, entity_type, entity_id, project_id)
VALUES ('also private', 'task', ?, 'not-a-number')`, task.ID)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.Healthy {
		t.Fatal("malformed text metadata reported healthy")
	}
	malformed := searchIntegrityType(report, domain.SearchEntityTask).Malformed
	if malformed.Count != 2 {
		t.Fatalf("malformed metadata not reported safely: %+v", searchIntegrityType(report, domain.SearchEntityTask))
	}
	for _, detail := range malformed.Details {
		if detail.EntityIDStorage != "text" && detail.ProjectIDStorage != "text" {
			t.Fatalf("malformed storage classes missing: %+v", detail)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "must not appear") || strings.Contains(fmt.Sprintf("%+v", report), "not-a-number") {
		t.Fatalf("report exposed indexed content: %+v", report)
	}
}

func TestCheckSearchIndexReportsNULLMetadataAndReindexRepairs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private null content', NULL, NULL, NULL)`)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex returned an operational error for NULL metadata: %v", err)
	}
	if report.Healthy || report.IndexTotal != 1 {
		t.Fatalf("NULL metadata report = %+v", report)
	}
	var nullType domain.SearchIndexTypeReport
	for _, typeReport := range report.Types {
		if typeReport.EntityType == "<null>" {
			nullType = typeReport
		}
	}
	if nullType.Malformed.Count != 1 || nullType.Malformed.Details[0].EntityTypeStorage != "null" || nullType.Malformed.Details[0].EntityIDStorage != "null" || nullType.Malformed.Details[0].ProjectIDStorage != "null" {
		t.Fatalf("NULL metadata placeholders missing: %+v", nullType)
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "private null content") {
		t.Fatalf("report exposed indexed content: %+v", report)
	}
	result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	if err != nil {
		t.Fatalf("ReindexSearch(NULL metadata): %v", err)
	}
	if !result.After.Healthy || result.After.IndexTotal != 0 {
		t.Fatalf("reindex after NULL metadata = %+v", result.After)
	}
}

func TestCheckSearchIndexReportsLogicalDrift(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		corrupt func(*testing.T, context.Context, *storeFixture, int64, int64)
		assert  func(*testing.T, domain.SearchIndexIntegrityReport, int64)
	}{
		"orphaned": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('orphan', 'task', ?, ?)`, taskID+1000, projectID)
			},
			assert: func(t *testing.T, report domain.SearchIndexIntegrityReport, _ int64) {
				if got := searchIntegrityType(report, domain.SearchEntityTask).Orphaned.Count; got != 1 {
					t.Fatalf("orphaned count = %d, want 1", got)
				}
			},
		},
		"unsupported retired note": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('retired', 'secret_type_marker', ?, ?)`, taskID, projectID)
			},
			assert: func(t *testing.T, report domain.SearchIndexIntegrityReport, _ int64) {
				var found bool
				for _, typeReport := range report.Types {
					if typeReport.EntityType == searchIndexUnsupportedType && typeReport.Unsupported.Count == 1 {
						found = true
					}
				}
				if !found || !report.RequiresBackupBeforeRepair() {
					t.Fatalf("unsupported row not safely classified: %+v", report.Types)
				}
				encoded, err := json.Marshal(report)
				if err != nil {
					t.Fatalf("Marshal report: %v", err)
				}
				if strings.Contains(string(encoded), "secret_type_marker") {
					t.Fatalf("report disclosed unsupported entity type: %s", encoded)
				}
			},
		},
		"duplicate": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) SELECT content, entity_type, entity_id, project_id FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, taskID)
			},
			assert: func(t *testing.T, report domain.SearchIndexIntegrityReport, _ int64) {
				duplicates := searchIntegrityType(report, domain.SearchEntityTask).Duplicates
				if duplicates.Count != 1 || duplicates.Details[0].IndexCount != 2 {
					t.Fatalf("duplicates = %+v, want one key with two rows", duplicates)
				}
			},
		},
		"content mismatch": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				execIntegritySQL(t, ctx, store, `UPDATE search_index SET content = 'wrong content' WHERE entity_type = 'task' AND entity_id = ?`, taskID)
			},
			assert: func(t *testing.T, report domain.SearchIndexIntegrityReport, _ int64) {
				if got := searchIntegrityType(report, domain.SearchEntityTask).ContentMismatched.Count; got != 1 {
					t.Fatalf("content mismatch count = %d, want 1", got)
				}
			},
		},
		"null content mismatch": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				execIntegritySQL(t, ctx, store, `UPDATE search_index SET content = NULL WHERE entity_type = 'task' AND entity_id = ?`, taskID)
			},
			assert: func(t *testing.T, report domain.SearchIndexIntegrityReport, _ int64) {
				if got := searchIntegrityType(report, domain.SearchEntityTask).ContentMismatched.Count; got != 1 {
					t.Fatalf("NULL content mismatch count = %d, want 1", got)
				}
			},
		},
		"project mismatch": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				other := mustUpsertProject(t, store, "Other", fmt.Sprintf("other-%d", taskID), fmt.Sprintf("/work/other-%d", taskID))
				execIntegritySQL(t, ctx, store, `UPDATE search_index SET project_id = ? WHERE entity_type = 'task' AND entity_id = ?`, other.ID, taskID)
			},
			assert: func(t *testing.T, report domain.SearchIndexIntegrityReport, _ int64) {
				if got := searchIntegrityType(report, domain.SearchEntityTask).ProjectMismatched.Count; got != 1 {
					t.Fatalf("project mismatch count = %d, want 1", got)
				}
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := openTestStore(t)
			project := mustUpsertProject(t, store, "P", "p", "/work/p")
			task, err := store.CreateTask(ctx, project.ID, "canonical marker", "body", domain.Priority(2), "backlog", nil, store.snap())
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			test.corrupt(t, ctx, store, project.ID, task.ID)
			report, err := store.CheckSearchIndex(ctx)
			if err != nil {
				t.Fatalf("CheckSearchIndex: %v", err)
			}
			if report.Healthy {
				t.Fatal("report.Healthy = true, want false")
			}
			test.assert(t, report, task.ID)
		})
	}
}

func TestCheckSearchIndexReportsMixedDuplicateDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	other := mustUpsertProject(t, store, "Other", "other", "/work/other")
	task, err := store.CreateTask(ctx, project.ID, "canonical duplicate", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `
INSERT INTO search_index(content, entity_type, entity_id, project_id)
VALUES ('wrong duplicate content', 'task', ?, ?)`, task.ID, other.ID)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	tasks := searchIntegrityType(report, domain.SearchEntityTask)
	if tasks.Duplicates.Count != 1 || tasks.ContentMismatched.Count != 1 || tasks.ProjectMismatched.Count != 1 {
		t.Fatalf("mixed duplicate drift = duplicates:%d content:%d project:%d; want 1/1/1", tasks.Duplicates.Count, tasks.ContentMismatched.Count, tasks.ProjectMismatched.Count)
	}
	if got := tasks.ProjectMismatched.Details[0].IndexedProjectID; got != other.ID {
		t.Fatalf("mismatched indexed project = %d, want %d", got, other.ID)
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "wrong duplicate content") {
		t.Fatalf("report exposed duplicate content: %+v", report)
	}
}

func TestCheckSearchIndexMixedIssueClassesGolden(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	other := mustUpsertProject(t, store, "Other", "other", "/work/other")
	tasks := make([]domain.Task, 4)
	for index := range tasks {
		var err error
		tasks[index], err = store.CreateTask(ctx, project.ID, fmt.Sprintf("mixed issue %d", index+1), "body", domain.Priority(2), "backlog", nil, store.snap())
		if err != nil {
			t.Fatalf("CreateTask(%d): %v", index+1, err)
		}
	}
	execIntegritySQL(t, ctx, store, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, tasks[0].ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) SELECT content, entity_type, entity_id, project_id FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, tasks[1].ID)
	execIntegritySQL(t, ctx, store, `UPDATE search_index SET content = 'private mismatch' WHERE entity_type = 'task' AND entity_id = ?`, tasks[2].ID)
	execIntegritySQL(t, ctx, store, `UPDATE search_index SET project_id = ? WHERE entity_type = 'task' AND entity_id = ?`, other.ID, tasks[3].ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private orphan', 'task', 8000, ?)`, project.ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private unsupported', 'secret_type', 7000, ?)`, project.ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private malformed', 'task', CAST(9000 AS TEXT), ?)`, project.ID)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "search_integrity_mixed.golden"))
	if err != nil {
		t.Fatalf("ReadFile golden: %v", err)
	}
	if strings.TrimSpace(string(encoded)) != strings.TrimSpace(string(want)) {
		t.Fatalf("mixed integrity report changed\nwant:\n%s\ngot:\n%s", want, encoded)
	}
}

func TestCheckSearchIndexDetectsEqualTotalOffsettingDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "canonical marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('orphan', 'task', ?, ?)`, task.ID+1000, project.ID)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.SourceTotal != report.IndexTotal {
		t.Fatalf("fixture totals differ: source=%d index=%d", report.SourceTotal, report.IndexTotal)
	}
	tasks := searchIntegrityType(report, domain.SearchEntityTask)
	if tasks.Missing.Count != 1 || tasks.Orphaned.Count != 1 || report.Healthy {
		t.Fatalf("offsetting drift not detected: %+v", tasks)
	}
}

func TestCheckSearchIndexReportsTriggerDrift(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		corrupt func(*testing.T, context.Context, *storeFixture)
		assert  func(domain.SearchIndexTriggerReport) bool
	}{
		"missing": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture) {
				execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
			},
			assert: func(report domain.SearchIndexTriggerReport) bool {
				return containsString(report.Missing, "search_index_tasks_ai")
			},
		},
		"unexpected": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture) {
				execIntegritySQL(t, ctx, store, `CREATE TRIGGER search_index_unexpected AFTER INSERT ON tags BEGIN SELECT 1; END`)
			},
			assert: func(report domain.SearchIndexTriggerReport) bool {
				return report.UnexpectedCount == 1
			},
		},
		"stale definition": {
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture) {
				execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
				stale := strings.Replace(canonicalSearchTriggers["search_index_tasks_ai"], "'task'", "'TASK'", 1)
				execIntegritySQL(t, ctx, store, stale)
			},
			assert: func(report domain.SearchIndexTriggerReport) bool {
				return containsString(report.Stale, "search_index_tasks_ai")
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := openTestStore(t)
			test.corrupt(t, ctx, store)
			report, err := store.CheckSearchIndex(ctx)
			if err != nil {
				t.Fatalf("CheckSearchIndex: %v", err)
			}
			if report.Healthy || !test.assert(report.Triggers) {
				t.Fatalf("trigger drift not detected: %+v", report.Triggers)
			}
		})
	}
}

func TestSearchTriggerDiscoveryUsesLiteralPrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	execIntegritySQL(t, ctx, store, `CREATE TRIGGER searchXindexYunrelated AFTER INSERT ON tags BEGIN SELECT 'search_index'; END`)
	execIntegritySQL(t, ctx, store, `CREATE TRIGGER search_index_secret_marker AFTER INSERT ON tags BEGIN SELECT 1; END`)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.Triggers.UnexpectedCount != 1 {
		t.Fatalf("unexpected trigger count = %d, want 1: %+v", report.Triggers.UnexpectedCount, report.Triggers)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal report: %v", err)
	}
	if strings.Contains(string(encoded), "searchXindexYunrelated") || strings.Contains(string(encoded), "secret_marker") {
		t.Fatalf("report disclosed arbitrary trigger name: %s", encoded)
	}
	if _, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true); err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	for name, want := range map[string]int{"searchXindexYunrelated": 1, "search_index_secret_marker": 0} {
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&count); err != nil {
			t.Fatalf("count trigger %s: %v", name, err)
		}
		if count != want {
			t.Fatalf("trigger %s count = %d, want %d", name, count, want)
		}
	}
}

func TestSearchTriggerCatalogFindsAndRemovesArbitraryNamePoisoning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	execIntegritySQL(t, ctx, store, `
CREATE TRIGGER private_poison_marker AFTER INSERT ON tasks BEGIN
  DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = NEW.id;
END`)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.Healthy || report.Triggers.UnexpectedCount != 1 {
		t.Fatalf("arbitrary-name persistence trigger not detected: %+v", report.Triggers)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal report: %v", err)
	}
	if strings.Contains(string(encoded), "private_poison_marker") || strings.Contains(string(encoded), "DELETE FROM search_index") {
		t.Fatalf("report disclosed arbitrary trigger catalog data: %s", encoded)
	}

	result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	if err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	if !result.After.Healthy {
		t.Fatalf("post-reindex report unhealthy: %+v", result.After)
	}
	var poisonTriggers int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'private_poison_marker'`).Scan(&poisonTriggers); err != nil {
		t.Fatalf("count poisoning trigger: %v", err)
	}
	if poisonTriggers != 0 {
		t.Fatalf("poisoning trigger survived reindex: count=%d", poisonTriggers)
	}

	task, err := store.CreateTask(ctx, project.ID, "source write health marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask after reindex: %v", err)
	}
	hits, err := store.Search(ctx, "health", project.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil || len(hits) != 1 || hits[0].ID != task.ID {
		t.Fatalf("canonical source-write indexing after reindex = %+v, %v", hits, err)
	}
}

func TestOpenSearchMaintenanceRejectsReplacementRace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "race.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	replacementErr := error(nil)
	opened, err := openSearchMaintenance(ctx, path, func() {
		if renameErr := os.Rename(path, path+".opened"); renameErr != nil {
			replacementErr = renameErr
			return
		}
		replacementErr = os.WriteFile(path, []byte("replacement"), 0o600)
	})
	if replacementErr != nil {
		t.Fatalf("replace database during open: %v", replacementErr)
	}
	if opened != nil {
		_ = opened.Close()
		t.Fatal("maintenance open accepted replaced database path")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("replacement race error = %v, want validation_error", err)
	}
}

func TestOpenSearchMaintenanceRejectsSymlinkedParentBeforeOpen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	realPath := filepath.Join(realDir, "omakiten.db")
	store, err := Open(ctx, realPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	linkedDir := filepath.Join(root, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	afterOpenCalled := false
	opened, err := openSearchMaintenance(ctx, filepath.Join(linkedDir, "omakiten.db"), func() {
		afterOpenCalled = true
	})
	if opened != nil {
		_ = opened.Close()
		t.Fatal("maintenance open accepted a symlinked parent")
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("symlinked-parent error = %v, want validation_error", err)
	}
	if afterOpenCalled {
		t.Fatal("symlinked parent was rejected only after SQLite open")
	}
}

func TestMaintenanceSnapshotAndReindexRejectPathReplacementAfterOpen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "maintenance.db")
	original := openStoreFixture(t, path)
	original.applyBundle(sqliteTestBundle(t))
	project := mustUpsertProject(t, original, "Original", "original", "/work/original")
	task, err := original.CreateTask(ctx, project.ID, "original evidence", "body", domain.Priority(2), "backlog", nil, original.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, original, `UPDATE search_index SET content = 'original corrupt evidence' WHERE entity_type = 'task' AND entity_id = ?`, task.ID)
	if err := original.Close(); err != nil {
		t.Fatalf("close original: %v", err)
	}

	maintenance, err := OpenSearchMaintenance(ctx, path)
	if err != nil {
		t.Fatalf("OpenSearchMaintenance: %v", err)
	}
	defer func() { _ = maintenance.Close() }()
	originalMoved := path + ".original"
	if err := os.Rename(path, originalMoved); err != nil {
		t.Fatalf("rename original: %v", err)
	}
	replacement, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("create replacement: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("close replacement: %v", err)
	}

	snapshotPath := filepath.Join(dir, "recovery.db")
	if err := maintenance.Snapshot(ctx, snapshotPath); err == nil {
		t.Fatal("maintenance snapshot accepted replaced pathname")
	}
	if _, err := os.Stat(snapshotPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement snapshot unexpectedly published: %v", err)
	}
	if _, err := maintenance.ReindexSearchConfirmed(ctx, true); err == nil {
		t.Fatal("maintenance reindex accepted replaced pathname")
	}

	originalDB, err := sql.Open("sqlite", originalMoved)
	if err != nil {
		t.Fatalf("open moved original: %v", err)
	}
	defer func() { _ = originalDB.Close() }()
	var originalCorruption int
	if err := originalDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE content = 'original corrupt evidence'`).Scan(&originalCorruption); err != nil || originalCorruption != 1 {
		t.Fatalf("original corruption after refusal = %d, %v", originalCorruption, err)
	}
	replacementDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open replacement: %v", err)
	}
	defer func() { _ = replacementDB.Close() }()
	var replacementTasks int
	if err := replacementDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&replacementTasks); err != nil || replacementTasks != 0 {
		t.Fatalf("replacement tasks after refusal = %d, %v", replacementTasks, err)
	}
}

func TestCheckSearchIndexPropagatesBusyIntegrityCheck(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy-check.db")
	store, err := OpenWithOptions(ctx, path, Options{BusyTimeoutMs: 25})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	locker, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = locker.Close() }()
	if _, err := locker.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("BEGIN IMMEDIATE: %v", err)
	}
	defer func() { _, _ = locker.ExecContext(context.Background(), `ROLLBACK`) }()

	report, err := store.CheckSearchIndex(ctx)
	if err == nil {
		t.Fatalf("CheckSearchIndex error = nil; report = %+v", report)
	}
	if !report.FTS5.OK {
		t.Fatalf("operational lock was misreported as corruption: %+v", report.FTS5)
	}
	var coded *domain.CodedError
	if errors.As(err, &coded) && coded.Code == domain.ErrSearchIndexInvalid {
		t.Fatalf("operational lock was mapped to search_index_invalid: %v", err)
	}
}

func TestCheckSearchIndexReportsFTS5InternalIntegrityFailureSafely(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "secret marker", "do not expose", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `UPDATE search_index_content SET c0 = 'shadow corruption' WHERE id = (SELECT rowid FROM search_index WHERE entity_type = 'task' AND entity_id = ?)`, task.ID)

	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if report.FTS5.OK || report.Healthy {
		t.Fatalf("FTS5 corruption not detected: %+v", report.FTS5)
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "secret marker") || strings.Contains(fmt.Sprintf("%+v", report), "do not expose") {
		t.Fatalf("report exposed indexed source text: %+v", report)
	}
}

func TestReindexSearchRepairsAllLogicalAndTriggerDriftAndIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "repair marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "repair marker comment", "human", nil); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	errorRecord, err := store.RecordError(ctx, project.ID, "repair marker error", "context", nil)
	if err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	if _, err := store.AddSolution(ctx, errorRecord.ID, "repair marker solution", "steps", nil); err != nil {
		t.Fatalf("AddSolution: %v", err)
	}
	if _, err := store.CreatePlan(ctx, project.ID, "repair-plan", "repair marker plan", "goal"); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	execIntegritySQL(t, ctx, store, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('orphan', 'task', ?, ?)`, task.ID+1000, project.ID)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('retired', 'note', ?, ?)`, task.ID, project.ID)
	execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
	execIntegritySQL(t, ctx, store, `CREATE TRIGGER search_index_unexpected AFTER INSERT ON tags BEGIN SELECT 1; END`)

	result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	if err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	if result.Before.Healthy || !result.After.Healthy {
		t.Fatalf("before/after health = %v/%v", result.Before.Healthy, result.After.Healthy)
	}
	if !result.BackupRecommended {
		t.Fatalf("orphan repair backup guidance = %+v", result)
	}
	hits, err := store.Search(ctx, "repair", project.ID, nil)
	if err != nil {
		t.Fatalf("Search after repair = %+v, %v", hits, err)
	}
	seen := map[domain.SearchEntityType]bool{}
	for _, hit := range hits {
		seen[hit.EntityType] = true
	}
	for _, entityType := range domain.AllSearchEntityTypes() {
		if !seen[entityType] {
			t.Fatalf("Search after repair missing physical type %s: %+v", entityType, hits)
		}
	}

	second, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	if err != nil {
		t.Fatalf("second ReindexSearch: %v", err)
	}
	if !second.Before.Healthy || !second.After.Healthy || second.BackupRecommended {
		t.Fatalf("idempotent result = %+v", second)
	}
}

func TestReindexSearchRepairsRemainingDriftCategories(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*testing.T, context.Context, *storeFixture, int64, int64){
		"duplicate": func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
			execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) SELECT content, entity_type, entity_id, project_id FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, taskID)
		},
		"content mismatch": func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
			execIntegritySQL(t, ctx, store, `UPDATE search_index SET content = 'wrong' WHERE entity_type = 'task' AND entity_id = ?`, taskID)
		},
		"project mismatch": func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
			other := mustUpsertProject(t, store, "Other", "other", "/work/other")
			execIntegritySQL(t, ctx, store, `UPDATE search_index SET project_id = ? WHERE entity_type = 'task' AND entity_id = ?`, other.ID, taskID)
		},
		"stale trigger": func(t *testing.T, ctx context.Context, store *storeFixture, _, _ int64) {
			execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
			execIntegritySQL(t, ctx, store, `CREATE TRIGGER search_index_tasks_ai AFTER INSERT ON tasks BEGIN SELECT 1; END`)
		},
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := openTestStore(t)
			project := mustUpsertProject(t, store, "P", "p", "/work/p")
			task, err := store.CreateTask(ctx, project.ID, "repair category", "body", domain.Priority(2), "backlog", nil, store.snap())
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			corrupt(t, ctx, store, project.ID, task.ID)
			before, err := store.CheckSearchIndex(ctx)
			if err != nil || before.Healthy {
				t.Fatalf("fixture health = %v, error = %v", before.Healthy, err)
			}
			result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
			if err != nil || !result.After.Healthy {
				t.Fatalf("ReindexSearch = %+v, %v", result.After, err)
			}
		})
	}
}

func TestReindexSearchFailedMandatoryPostCheckRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "rollback marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID)
	checks := 0
	corruptBeforePostCheck := func(checkCtx context.Context, db searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		checks++
		if checks == 2 {
			if _, err := db.ExecContext(checkCtx, `UPDATE search_index_content SET c0 = 'post-check corruption'`); err != nil {
				return domain.SearchIndexIntegrityReport{}, err
			}
		}
		return checkSearchIndex(checkCtx, db)
	}

	result, err := store.reindexSearchWithPolicy(ctx, corruptBeforePostCheck, true)
	if err == nil {
		t.Fatalf("ReindexSearch error = nil; result = %+v", result)
	}
	if result.After.FTS5.OK {
		t.Fatalf("mandatory post-check did not observe sabotage: %+v", result.After)
	}
	report, checkErr := store.CheckSearchIndex(ctx)
	if checkErr != nil {
		t.Fatalf("CheckSearchIndex after rollback: %v", checkErr)
	}
	if searchIntegrityType(report, domain.SearchEntityTask).Missing.Count != 1 || !report.FTS5.OK {
		t.Fatalf("rollback did not preserve pre-repair state: %+v", report)
	}
}

func TestReindexSearchPostCheckSQLErrorRollsBackTriggersAndContent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "sql rollback marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
	checks := 0
	failingPostCheck := func(checkCtx context.Context, db searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		checks++
		if checks == 2 {
			_, err := db.ExecContext(checkCtx, `INSERT INTO missing_search_integrity_table VALUES (1)`)
			return domain.SearchIndexIntegrityReport{}, err
		}
		return checkSearchIndex(checkCtx, db)
	}

	if _, err := store.reindexSearchWithPolicy(ctx, failingPostCheck, true); err == nil {
		t.Fatal("ReindexSearch SQL sabotage error = nil")
	}
	report, checkErr := store.CheckSearchIndex(ctx)
	if checkErr != nil {
		t.Fatalf("CheckSearchIndex after rollback: %v", checkErr)
	}
	if !containsString(report.Triggers.Missing, "search_index_tasks_ai") {
		t.Fatalf("SQL-error rollback did not restore pre-repair trigger drift: %+v", report.Triggers)
	}
	hits, searchErr := store.Search(ctx, "rollback", project.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if searchErr != nil || len(hits) != 1 || hits[0].ID != task.ID {
		t.Fatalf("SQL-error rollback changed index content: %+v, %v", hits, searchErr)
	}
}

func TestReindexSearchJoinsRollbackFailureWithPrimaryError(t *testing.T) {
	t.Parallel()

	primaryErr := errors.New("injected primary failure")
	rollbackErr := errors.New("injected raw rollback failure")
	db := rollbackFailingSearchDB{rollbackErr: rollbackErr}
	check := func(context.Context, searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		return domain.SearchIndexIntegrityReport{}, primaryErr
	}

	_, err := reindexSearchOnConn(context.Background(), db, check, true, transactionControl{rollbackLabel: "rollback search transaction"}, nil)
	if !errors.Is(err, primaryErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("reindex error = %v, want joined primary and rollback failures", err)
	}
}

func TestCheckSearchIndexJoinsRollbackFailureWithPrimaryError(t *testing.T) {
	t.Parallel()

	primaryErr := errors.New("injected logical check failure")
	rollbackErr := errors.New("injected raw rollback failure")
	db := rollbackFailingSearchDB{operationErr: primaryErr, rollbackErr: rollbackErr}

	_, err := checkSearchIndexLogicalSnapshot(context.Background(), db, nil, transactionControl{rollbackLabel: "rollback search transaction"})
	if !errors.Is(err, primaryErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("check error = %v, want joined primary and rollback failures", err)
	}
}

func TestReindexSearchRollbackFailureInvalidatesPhysicalConnection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "connection rollback marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	conn, release, connection, err := store.acquireSearchConnection(ctx)
	if err != nil {
		t.Fatalf("acquireSearchConnection: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		release()
		t.Fatalf("BEGIN IMMEDIATE: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE search_index SET content = 'uncommitted failed connection write' WHERE entity_type = 'task' AND entity_id = ?`, task.ID); err != nil {
		release()
		t.Fatalf("stage uncommitted update: %v", err)
	}
	primaryErr := errors.New("injected check failure")
	rollbackErr := errors.New("injected rollback transport failure")
	invalidated := false
	control := transactionControl{
		rollback: func(context.Context, sqliteTransaction) error { return rollbackErr },
		invalidate: func() {
			invalidated = true
			connection.invalidate()
		},
		rollbackLabel: "rollback search transaction",
	}
	check := func(context.Context, searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		return domain.SearchIndexIntegrityReport{}, primaryErr
	}

	_, err = reindexSearchOnConn(ctx, conn, check, true, control, nil)
	release()
	if !errors.Is(err, primaryErr) || !errors.Is(err, rollbackErr) || !invalidated {
		t.Fatalf("failed rollback result = invalidated:%v error:%v", invalidated, err)
	}
	if pingErr := conn.PingContext(ctx); !errors.Is(pingErr, sql.ErrConnDone) {
		t.Fatalf("failed physical connection remained reusable: %v", pingErr)
	}
	var content string
	if err := store.db.QueryRowContext(ctx, `SELECT content FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID).Scan(&content); err != nil {
		t.Fatalf("read evidence after failed rollback: %v", err)
	}
	if content == "uncommitted failed connection write" {
		t.Fatalf("unproven rollback leaked uncommitted evidence: %q", content)
	}
	report, err := store.CheckSearchIndex(ctx)
	if err != nil || !report.Healthy {
		t.Fatalf("fresh connection after invalidation = healthy:%v error:%v", report.Healthy, err)
	}
}

func TestReindexSearchRepairsFTS5InternalIntegrityFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "internal repair marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `UPDATE search_index_content SET c0 = 'shadow corruption' WHERE id = (SELECT rowid FROM search_index WHERE entity_type = 'task' AND entity_id = ?)`, task.ID)

	result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	if err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	if result.Before.FTS5.OK || !result.After.Healthy || !result.After.FTS5.OK {
		t.Fatalf("FTS5 before/after = %+v/%+v", result.Before.FTS5, result.After.FTS5)
	}
}

func TestReindexSearchPreservesSearchSemantics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	projectA := mustUpsertProject(t, store, "A", "a", "/work/a")
	projectB := mustUpsertProject(t, store, "B", "b", "/work/b")
	best, err := store.CreateTask(ctx, projectA.ID, "needle needle needle", "focused", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask best: %v", err)
	}
	if _, err := store.CreateTask(ctx, projectA.ID, "needle", "less relevant filler filler filler", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask lower: %v", err)
	}
	archived, err := store.CreateTask(ctx, projectA.ID, "needle archived", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask archived: %v", err)
	}
	if _, _, err := store.SetTaskState(ctx, projectA.ID, archived.ID, domain.TaskStateArchived, "", store.snap()); err != nil {
		t.Fatalf("SetTaskState: %v", err)
	}
	if _, err := store.CreateTask(ctx, projectB.ID, "needle other project", "body", domain.Priority(2), "backlog", nil, store.snap()); err != nil {
		t.Fatalf("CreateTask project B: %v", err)
	}

	assertSearch := func(stage string) {
		t.Helper()
		hits, err := store.Search(ctx, "needle", projectA.ID, []domain.SearchEntityType{domain.SearchEntityTask})
		if err != nil {
			t.Fatalf("Search %s: %v", stage, err)
		}
		if len(hits) != 2 || hits[0].ID != best.ID {
			t.Fatalf("Search %s ranking/filter = %+v", stage, hits)
		}
		if !strings.Contains(hits[0].Snippet, "<mark>needle</mark>") {
			t.Fatalf("Search %s snippet = %q", stage, hits[0].Snippet)
		}
		for _, hit := range hits {
			if hit.ProjectID != projectA.ID || hit.ID == archived.ID {
				t.Fatalf("Search %s leaked project/archive row: %+v", stage, hit)
			}
		}
	}
	assertSearch("before")
	if _, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true); err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	assertSearch("after")
}

func TestReindexSearchCanceledContextPreservesIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	task, err := store.CreateTask(ctx, project.ID, "cancel marker", "body", domain.Priority(2), "backlog", nil, store.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, store, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID)
	canceled, cancel := context.WithCancel(ctx)
	checks := 0
	checkThenCancel := func(checkCtx context.Context, db searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		checks++
		report, checkErr := checkSearchIndex(checkCtx, db)
		if checks == 2 {
			cancel()
		}
		return report, checkErr
	}
	if _, err := store.reindexSearchWithPolicy(canceled, checkThenCancel, true); err == nil {
		t.Fatal("reindexSearch(cancel after post-check) error = nil")
	}
	if checks != 2 {
		t.Fatalf("check calls = %d, want pre + post", checks)
	}
	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if searchIntegrityType(report, domain.SearchEntityTask).Missing.Count != 1 {
		t.Fatalf("canceled repair changed prior corruption: %+v", searchIntegrityType(report, domain.SearchEntityTask))
	}
}

func TestReindexSearchConfirmedRechecksDestructiveRowsInTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private', 'task', CAST(99 AS TEXT), 0)`)
	result, err := store.ReindexSearchConfirmed(ctx, false)
	if err == nil {
		t.Fatalf("ReindexSearchConfirmed(false) error = nil; result=%+v", result)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation || searchIntegrityType(result.Before, domain.SearchEntityTask).Malformed.Count != 1 {
		t.Fatalf("transactional confirmation error = %v, result=%+v", err, result)
	}
	var remaining int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE typeof(entity_id) = 'text'`).Scan(&remaining); err != nil {
		t.Fatalf("count malformed: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("malformed rows after refusal = %d, want 1", remaining)
	}
}

func TestReindexSearchConfirmedProtectsAllDiscardedIndexEvidence(t *testing.T) {
	tests := []struct {
		name      string
		corrupt   func(*testing.T, context.Context, *storeFixture, int64, int64)
		preserved func(*testing.T, context.Context, *storeFixture, int64, int64)
	}{
		{
			name: "orphaned row",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('unique orphan evidence', 'task', ?, ?)`, taskID+1000, projectID)
			},
			preserved: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				var count int
				if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE entity_type = 'task' AND entity_id = ? AND content = 'unique orphan evidence'`, taskID+1000).Scan(&count); err != nil || count != 1 {
					t.Fatalf("orphan evidence after refusal = %d, %v", count, err)
				}
			},
		},
		{
			name: "unsupported row",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('unique unsupported evidence', 'retired_private_type', ?, ?)`, taskID, projectID)
			},
			preserved: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				var count int
				if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE entity_type = 'retired_private_type' AND entity_id = ? AND content = 'unique unsupported evidence'`, taskID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("unsupported evidence after refusal = %d, %v", count, err)
				}
			},
		},
		{
			name: "malformed row",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('unique malformed evidence', 'task', CAST(? AS TEXT), ?)`, taskID+1000, projectID)
			},
			preserved: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				var count int
				if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE typeof(entity_id) = 'text' AND entity_id = CAST(? AS TEXT) AND content = 'unique malformed evidence'`, taskID+1000).Scan(&count); err != nil || count != 1 {
					t.Fatalf("malformed evidence after refusal = %d, %v", count, err)
				}
			},
		},
		{
			name: "duplicate unique text",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				execIntegritySQL(t, ctx, store, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('unique duplicate evidence', 'task', ?, ?)`, taskID, projectID)
			},
			preserved: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				var count int
				if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE entity_type = 'task' AND entity_id = ? AND content = 'unique duplicate evidence'`, taskID).Scan(&count); err != nil || count != 1 {
					t.Fatalf("unique duplicate evidence after refusal = %d, %v", count, err)
				}
			},
		},
		{
			name: "content mismatch",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				execIntegritySQL(t, ctx, store, `UPDATE search_index SET content = 'unique mismatched evidence' WHERE entity_type = 'task' AND entity_id = ?`, taskID)
			},
			preserved: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				var content string
				if err := store.db.QueryRowContext(ctx, `SELECT content FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, taskID).Scan(&content); err != nil || content != "unique mismatched evidence" {
					t.Fatalf("mismatched evidence after refusal = %q, %v", content, err)
				}
			},
		},
		{
			name: "project mismatch",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, _, taskID int64) {
				other := mustUpsertProject(t, store, "Other", "other", "/work/other")
				execIntegritySQL(t, ctx, store, `UPDATE search_index SET project_id = ? WHERE entity_type = 'task' AND entity_id = ?`, other.ID, taskID)
			},
			preserved: func(t *testing.T, ctx context.Context, store *storeFixture, projectID, taskID int64) {
				var indexedProjectID int64
				if err := store.db.QueryRowContext(ctx, `SELECT project_id FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, taskID).Scan(&indexedProjectID); err != nil || indexedProjectID == projectID {
					t.Fatalf("mismatched project evidence after refusal = %d, %v", indexedProjectID, err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := openTestStore(t)
			project := mustUpsertProject(t, store, "P", "p", "/work/p")
			task, err := store.CreateTask(ctx, project.ID, "canonical evidence", "body", domain.Priority(2), "backlog", nil, store.snap())
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			test.corrupt(t, ctx, store, project.ID, task.ID)
			before, err := store.CheckSearchIndex(ctx)
			if err != nil || !before.RequiresBackupBeforeRepair() {
				t.Fatalf("destructive preflight = requires:%v error:%v report:%+v", before.RequiresBackupBeforeRepair(), err, before)
			}

			result, err := store.ReindexSearchConfirmed(ctx, false)
			var coded *domain.CodedError
			if !errors.As(err, &coded) || coded.Code != domain.ErrValidation || result.BackupRecommended != true {
				t.Fatalf("unconfirmed repair = result:%+v error:%v", result, err)
			}
			test.preserved(t, ctx, store, project.ID, task.ID)

			confirmed, err := store.ReindexSearchConfirmed(ctx, true)
			if err != nil || !confirmed.After.Healthy || !confirmed.BackupRecommended {
				t.Fatalf("confirmed repair = result:%+v error:%v", confirmed, err)
			}
		})
	}
}

func TestReindexSearchConfirmedAllowsReconstructiveOnlyRepair(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(*testing.T, context.Context, *storeFixture, int64)
	}{
		{
			name: "missing canonical row",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, taskID int64) {
				execIntegritySQL(t, ctx, store, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, taskID)
			},
		},
		{
			name: "missing canonical trigger",
			corrupt: func(t *testing.T, ctx context.Context, store *storeFixture, _ int64) {
				execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store := openTestStore(t)
			project := mustUpsertProject(t, store, "P", "p", "/work/p")
			task, err := store.CreateTask(ctx, project.ID, "reconstructive repair", "body", domain.Priority(2), "backlog", nil, store.snap())
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			test.corrupt(t, ctx, store, task.ID)
			before, err := store.CheckSearchIndex(ctx)
			if err != nil || before.Healthy || before.RequiresBackupBeforeRepair() {
				t.Fatalf("reconstructive preflight = healthy:%v requires_backup:%v error:%v", before.Healthy, before.RequiresBackupBeforeRepair(), err)
			}
			result, err := store.ReindexSearchConfirmed(ctx, false)
			if err != nil || !result.After.Healthy || result.BackupRecommended {
				t.Fatalf("unconfirmed reconstructive repair = result:%+v error:%v", result, err)
			}
		})
	}
}

func TestReindexSearchConfirmedAllowsCanonicalInternalRebuild(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	checks := 0
	internalOnlyCheck := func(checkCtx context.Context, db searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		checks++
		if checks == 1 {
			return domain.SearchIndexIntegrityReport{
				Healthy: false,
				FTS5:    domain.SearchIndexFTSIntegrity{OK: false, Error: "FTS5 integrity-check failed"},
			}, nil
		}
		return checkSearchIndex(checkCtx, db)
	}

	result, err := store.reindexSearchWithPolicy(ctx, internalOnlyCheck, false)
	if err != nil || !result.After.Healthy || result.BackupRecommended || checks != 2 {
		t.Fatalf("unconfirmed canonical internal rebuild = result:%+v checks:%d error:%v", result, checks, err)
	}
}

func TestReindexSearchConfirmedWithBackupRetriesUntilGenerationIsExact(t *testing.T) {
	tests := []struct {
		name  string
		hooks func(*sql.DB) reindexBackupHooks
	}{
		{
			name: "external commit during backup",
			hooks: func(writer *sql.DB) reindexBackupHooks {
				return reindexBackupHooks{Generation: exactGenerationHooks{AfterBackup: func(attempt int) {
					if attempt == 1 {
						done := make(chan error, 1)
						go func() {
							_, err := writer.Exec(`INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('late evidence', 'retired_private_type', 202, 0)`)
							done <- err
						}()
						if err := <-done; err != nil {
							t.Fatalf("external commit during backup: %v", err)
						}
					}
				}}}
			},
		},
		{
			name: "external commit between backup and begin",
			hooks: func(writer *sql.DB) reindexBackupHooks {
				return reindexBackupHooks{Generation: exactGenerationHooks{BeforeBegin: func(attempt int) {
					if attempt == 1 {
						if _, err := writer.Exec(`INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('late evidence', 'retired_private_type', 202, 0)`); err != nil {
							t.Fatalf("external commit before BEGIN IMMEDIATE: %v", err)
						}
					}
				}}}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "maintenance.db")
			setup := openStoreFixture(t, path)
			execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('initial evidence', 'retired_private_type', 101, 0)`)
			if err := setup.Close(); err != nil {
				t.Fatalf("close setup: %v", err)
			}
			maintenance, err := OpenSearchMaintenance(ctx, path)
			if err != nil {
				t.Fatalf("OpenSearchMaintenance: %v", err)
			}
			defer func() { _ = maintenance.Close() }()
			writer, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatalf("open external writer: %v", err)
			}
			defer func() { _ = writer.Close() }()

			var attemptedPaths []string
			createBackup := func(attemptCtx context.Context, write func(string) error) (string, error) {
				backupPath := filepath.Join(dir, fmt.Sprintf("attempt-%d.db", len(attemptedPaths)+1))
				attemptedPaths = append(attemptedPaths, backupPath)
				return backupPath, write(backupPath)
			}
			result, backupPath, err := maintenance.reindexSearchConfirmedWithBackup(ctx, createBackup, os.Remove, func() error { return nil }, test.hooks(writer))
			if err != nil {
				t.Fatalf("reindexSearchConfirmedWithBackup: %v", err)
			}
			if len(attemptedPaths) != 2 || backupPath != attemptedPaths[1] || !result.After.Healthy {
				t.Fatalf("generation retry = attempts:%v retained:%q result:%+v", attemptedPaths, backupPath, result)
			}
			if _, err := os.Stat(attemptedPaths[0]); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale generation backup survived: %v", err)
			}
			backup, err := sql.Open("sqlite", backupPath)
			if err != nil {
				t.Fatalf("open retained backup: %v", err)
			}
			defer func() { _ = backup.Close() }()
			var retainedEvidence int
			if err := backup.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE content IN ('initial evidence', 'late evidence')`).Scan(&retainedEvidence); err != nil || retainedEvidence != 2 {
				t.Fatalf("retained generation evidence = %d, %v; want 2", retainedEvidence, err)
			}
		})
	}
}

func TestReindexSearchConfirmedWithBackupRejectsPathReplacementAfterBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "maintenance.db")
	setup := openStoreFixture(t, path)
	execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('preserved private evidence', 'retired_private_type', 101, 0)`)
	if err := setup.Close(); err != nil {
		t.Fatalf("close setup: %v", err)
	}

	maintenance, err := OpenSearchMaintenance(ctx, path)
	if err != nil {
		t.Fatalf("OpenSearchMaintenance: %v", err)
	}
	defer func() { _ = maintenance.Close() }()
	backupPath := filepath.Join(dir, "generated.db")
	createBackup := func(_ context.Context, write func(string) error) (string, error) {
		return backupPath, write(backupPath)
	}
	movedPath := path + ".original"
	var replacementErr error
	hooks := reindexBackupHooks{Generation: exactGenerationHooks{AfterBackup: func(int) {
		if err := os.Rename(path, movedPath); err != nil {
			replacementErr = err
			return
		}
		replacement, err := Open(ctx, path)
		if err != nil {
			replacementErr = err
			return
		}
		replacementErr = replacement.Close()
	}}}

	result, retainedPath, err := maintenance.reindexSearchConfirmedWithBackup(ctx, createBackup, os.Remove, func() error { return nil }, hooks)
	if replacementErr != nil {
		t.Fatalf("replace maintenance pathname: %v", replacementErr)
	}
	if err == nil {
		t.Fatalf("replacement after backup succeeded: result=%+v retained=%q", result, retainedPath)
	}
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
		t.Fatalf("replacement error = %v, want validation_error", err)
	}
	if retainedPath != "" {
		t.Fatalf("removed stale backup returned as retained path: %q", retainedPath)
	}
	if _, statErr := os.Stat(backupPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale generated backup survived replacement abort: %v", statErr)
	}

	originalDB, err := sql.Open("sqlite", movedPath)
	if err != nil {
		t.Fatalf("open moved original: %v", err)
	}
	defer func() { _ = originalDB.Close() }()
	var evidence int
	if err := originalDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE content = 'preserved private evidence'`).Scan(&evidence); err != nil || evidence != 1 {
		t.Fatalf("original evidence after replacement abort = %d, %v", evidence, err)
	}
	replacementDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open replacement: %v", err)
	}
	defer func() { _ = replacementDB.Close() }()
	var replacementRows int
	if err := replacementDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index`).Scan(&replacementRows); err != nil || replacementRows != 0 {
		t.Fatalf("replacement index rows = %d, %v", replacementRows, err)
	}
}

func TestReindexSearchConfirmedWithBackupRevalidatesPathDuringTransaction(t *testing.T) {
	tests := map[string]func(*reindexBackupHooks, func()){
		"under begin immediate": func(hooks *reindexBackupHooks, replace func()) {
			hooks.Generation.AfterBegin = func(int) { replace() }
		},
		"immediately before commit": func(hooks *reindexBackupHooks, replace func()) {
			hooks.BeforeCommit = func(int) { replace() }
		},
	}
	for name, installHook := range tests {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "maintenance.db")
			setup := openStoreFixture(t, path)
			execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('transaction identity evidence', 'retired_private_type', 303, 0)`)
			if err := setup.Close(); err != nil {
				t.Fatalf("close setup: %v", err)
			}
			maintenance, err := OpenSearchMaintenance(ctx, path)
			if err != nil {
				t.Fatalf("OpenSearchMaintenance: %v", err)
			}
			backupPath := filepath.Join(dir, "generated.db")
			createBackup := func(_ context.Context, write func(string) error) (string, error) {
				return backupPath, write(backupPath)
			}
			movedPath := path + ".original"
			var replacementErr error
			replace := func() {
				if replacementErr != nil {
					return
				}
				if err := os.Rename(path, movedPath); err != nil {
					replacementErr = err
					return
				}
				replacementErr = os.WriteFile(path, []byte("replacement pathname"), 0o600)
			}
			hooks := reindexBackupHooks{}
			installHook(&hooks, replace)

			result, retainedPath, err := maintenance.reindexSearchConfirmedWithBackup(ctx, createBackup, os.Remove, func() error { return nil }, hooks)
			if replacementErr != nil {
				t.Fatalf("replace maintenance pathname: %v", replacementErr)
			}
			if err == nil {
				t.Fatalf("transaction pathname replacement succeeded: result=%+v retained=%q", result, retainedPath)
			}
			var coded *domain.CodedError
			if !errors.As(err, &coded) || coded.Code != domain.ErrValidation {
				t.Fatalf("transaction replacement error = %v, want validation_error", err)
			}
			if retainedPath != "" {
				t.Fatalf("removed stale backup returned as retained path: %q", retainedPath)
			}
			if _, statErr := os.Stat(backupPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("stale generated backup survived transaction abort: %v", statErr)
			}
			if err := maintenance.Close(); err != nil {
				t.Fatalf("close maintenance: %v", err)
			}
			originalDB, err := sql.Open("sqlite", movedPath)
			if err != nil {
				t.Fatalf("open moved original: %v", err)
			}
			defer func() { _ = originalDB.Close() }()
			var evidence int
			if err := originalDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE content = 'transaction identity evidence'`).Scan(&evidence); err != nil || evidence != 1 {
				t.Fatalf("transaction rollback evidence = %d, %v", evidence, err)
			}
		})
	}
}

func TestReindexSearchConfirmedWithBackupRetainsCandidateAfterBeginFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "maintenance.db")
	setup := openStoreFixture(t, path)
	execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('begin failure evidence', 'retired_private_type', 401, 0)`)
	if err := setup.Close(); err != nil {
		t.Fatalf("close setup: %v", err)
	}
	maintenance, err := OpenSearchMaintenance(ctx, path)
	if err != nil {
		t.Fatalf("OpenSearchMaintenance: %v", err)
	}
	defer func() { _ = maintenance.Close() }()
	maintenance.busyTimeoutMs = 1
	lockerDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open competing writer: %v", err)
	}
	defer func() { _ = lockerDB.Close() }()
	locker, err := lockerDB.Conn(ctx)
	if err != nil {
		t.Fatalf("pin competing writer: %v", err)
	}
	defer func() { _ = locker.Close() }()
	var attemptedPaths []string
	create := func(_ context.Context, write func(string) error) (string, error) {
		backupPath := filepath.Join(dir, fmt.Sprintf("begin-failure-%d.db", len(attemptedPaths)+1))
		attemptedPaths = append(attemptedPaths, backupPath)
		return backupPath, write(backupPath)
	}
	hooks := reindexBackupHooks{Generation: exactGenerationHooks{BeforeBegin: func(attempt int) {
		if attempt == 1 {
			if _, err := locker.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
				t.Errorf("acquire competing writer lock: %v", err)
			}
		}
	}}}

	_, backupPath, err := maintenance.reindexSearchConfirmedWithBackup(ctx, create, os.Remove, func() error { return nil }, hooks)
	if _, rollbackErr := locker.ExecContext(ctx, `ROLLBACK`); rollbackErr != nil {
		t.Fatalf("release competing writer lock: %v", rollbackErr)
	}
	if err == nil {
		t.Fatal("reindex under competing writer lock succeeded")
	}
	if len(attemptedPaths) != 1 || backupPath != attemptedPaths[0] {
		t.Fatalf("begin failure = attempts:%v retained:%q, want first candidate retained without retry", attemptedPaths, backupPath)
	}
	if _, statErr := os.Stat(backupPath); statErr != nil {
		t.Fatalf("retained begin-failure candidate: %v", statErr)
	}
}

func TestReindexSearchBeginFailureRejectsReplacedRecoveryCandidate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "maintenance.db")
	setup := openStoreFixture(t, path)
	execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('begin replacement evidence', 'retired_private_type', 411, 0)`)
	if err := setup.Close(); err != nil {
		t.Fatalf("close setup: %v", err)
	}
	maintenance, err := OpenSearchMaintenance(ctx, path)
	if err != nil {
		t.Fatalf("OpenSearchMaintenance: %v", err)
	}
	defer func() { _ = maintenance.Close() }()
	maintenance.busyTimeoutMs = 1
	lockerDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open competing writer: %v", err)
	}
	defer func() { _ = lockerDB.Close() }()
	locker, err := lockerDB.Conn(ctx)
	if err != nil {
		t.Fatalf("pin competing writer: %v", err)
	}
	defer func() { _ = locker.Close() }()
	backupPath := filepath.Join(dir, "replaced-recovery.db")
	var backupIdentity os.FileInfo
	create := func(_ context.Context, write func(string) error) (string, error) {
		if err := write(backupPath); err != nil {
			return backupPath, err
		}
		backupIdentity, err = os.Lstat(backupPath)
		return backupPath, err
	}
	validate := func() error {
		current, err := os.Lstat(backupPath)
		if err != nil || !os.SameFile(backupIdentity, current) {
			return errors.New("recovery candidate changed")
		}
		return nil
	}
	hooks := reindexBackupHooks{Generation: exactGenerationHooks{BeforeBegin: func(int) {
		if err := os.Remove(backupPath); err != nil {
			t.Errorf("remove recovery candidate: %v", err)
			return
		}
		if err := os.WriteFile(backupPath, []byte("replacement"), 0o600); err != nil {
			t.Errorf("replace recovery candidate: %v", err)
			return
		}
		if _, err := locker.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
			t.Errorf("acquire competing writer lock: %v", err)
		}
	}}}

	_, retainedPath, err := maintenance.reindexSearchConfirmedWithBackup(ctx, create, os.Remove, validate, hooks)
	_, _ = locker.ExecContext(ctx, `ROLLBACK`)
	if err == nil || retainedPath != "" {
		t.Fatalf("replaced BEGIN-failure candidate = retained:%q error:%v", retainedPath, err)
	}
	if _, statErr := os.Stat(backupPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("replaced recovery candidate survived rejection: %v", statErr)
	}
}

func TestReindexSearchConfirmedWithBackupRetainsCandidateAfterVersionReadFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*reindexBackupHooks, func()){
		"after snapshot": func(hooks *reindexBackupHooks, closeConn func()) {
			hooks.Generation.AfterBackup = func(int) { closeConn() }
		},
		"under writer lock": func(hooks *reindexBackupHooks, closeConn func()) {
			hooks.Generation.AfterBegin = func(int) { closeConn() }
		},
	}
	for name, install := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, "maintenance.db")
			setup := openStoreFixture(t, path)
			execIntegritySQL(t, ctx, setup, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('read failure evidence', 'retired_private_type', 402, 0)`)
			if err := setup.Close(); err != nil {
				t.Fatalf("close setup: %v", err)
			}
			maintenance, err := OpenSearchMaintenance(ctx, path)
			if err != nil {
				t.Fatalf("OpenSearchMaintenance: %v", err)
			}
			defer func() { _ = maintenance.Close() }()
			backupPath := filepath.Join(dir, "read-failure.db")
			create := func(_ context.Context, write func(string) error) (string, error) {
				return backupPath, write(backupPath)
			}
			var closeErr error
			hooks := reindexBackupHooks{}
			install(&hooks, func() {
				if closeErr == nil {
					closeErr = maintenance.maintenanceConn.Close()
				}
			})

			_, retainedPath, err := maintenance.reindexSearchConfirmedWithBackup(ctx, create, os.Remove, func() error { return nil }, hooks)
			if closeErr != nil {
				t.Fatalf("close search maintenance connection: %v", closeErr)
			}
			if err == nil {
				t.Fatal("reindex survived injected data_version read failure")
			}
			if retainedPath != backupPath {
				t.Fatalf("data_version read failure retained %q, want %q", retainedPath, backupPath)
			}
			if _, statErr := os.Stat(backupPath); statErr != nil {
				t.Fatalf("retained read-failure candidate: %v", statErr)
			}
		})
	}
}

func TestReindexSearchLockTimeoutPreservesIndex(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "locked.db")
	store, err := OpenWithOptions(ctx, path, Options{BusyTimeoutMs: 25})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture := newStoreFixture(t, store)
	fixture.applyBundle(sqliteTestBundle(t))
	project := mustUpsertProject(t, fixture, "P", "p", "/work/p")
	task, err := fixture.CreateTask(ctx, project.ID, "locked marker", "body", domain.Priority(2), "backlog", nil, fixture.snap())
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	execIntegritySQL(t, ctx, fixture, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, task.ID)

	locker, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = locker.Close() }()
	if _, err := locker.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatalf("BEGIN IMMEDIATE locker: %v", err)
	}
	defer func() { _, _ = locker.ExecContext(context.Background(), `ROLLBACK`) }()
	if _, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true); err == nil {
		t.Fatal("ReindexSearch under write lock error = nil")
	}
	if _, err := locker.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	report, err := store.CheckSearchIndex(ctx)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if searchIntegrityType(report, domain.SearchEntityTask).Missing.Count != 1 {
		t.Fatalf("lock-timeout repair changed prior corruption: %+v", searchIntegrityType(report, domain.SearchEntityTask))
	}
}

func TestReindexSearchConcurrentReaderNeverSeesEmptyIndex(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent.db")
	writer := openStoreFixture(t, path)
	writer.applyBundle(sqliteTestBundle(t))
	readerStore, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open reader: %v", err)
	}
	t.Cleanup(func() { _ = readerStore.Close() })
	project := mustUpsertProject(t, writer, "P", "p", "/work/p")
	first, err := writer.CreateTask(ctx, project.ID, "concurrent marker one", "body", domain.Priority(2), "backlog", nil, writer.snap())
	if err != nil {
		t.Fatalf("CreateTask(first): %v", err)
	}
	second, err := writer.CreateTask(ctx, project.ID, "concurrent marker two", "body", domain.Priority(2), "backlog", nil, writer.snap())
	if err != nil {
		t.Fatalf("CreateTask(second): %v", err)
	}
	execIntegritySQL(t, ctx, writer, `DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = ?`, second.ID)

	postCheckReached := make(chan struct{})
	releaseCommit := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCommit) }) }
	defer release()
	checks := 0
	blockingCheck := func(checkCtx context.Context, db searchIndexDB) (domain.SearchIndexIntegrityReport, error) {
		checks++
		report, checkErr := checkSearchIndex(checkCtx, db)
		if checks == 2 && checkErr == nil {
			close(postCheckReached)
			<-releaseCommit
		}
		return report, checkErr
	}
	reindexDone := make(chan error, 1)
	go func() {
		_, reindexErr := writer.reindexSearchWithPolicy(ctx, blockingCheck, true)
		reindexDone <- reindexErr
	}()
	<-postCheckReached

	hits, err := readerStore.Search(ctx, "concurrent", project.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil {
		t.Fatalf("reader Search before commit: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != first.ID {
		t.Fatalf("reader saw uncommitted/empty index: %+v", hits)
	}
	release()
	if err := <-reindexDone; err != nil {
		t.Fatalf("reindexSearch: %v", err)
	}
	after, err := readerStore.Search(ctx, "concurrent", project.ID, []domain.SearchEntityType{domain.SearchEntityTask})
	if err != nil || len(after) != 2 {
		t.Fatalf("reader Search after commit = %+v, %v", after, err)
	}
}

func TestSearchIndexReportBoundsLargeCorruption(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	execIntegritySQL(t, ctx, store, `DROP TRIGGER search_index_tasks_ai`)
	execIntegritySQL(t, ctx, store, `
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state)
VALUES (?, 1, 'bounded missing', '', 2, 'active')`, project.ID)
	execIntegritySQL(t, ctx, store, `
WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM seq WHERE n < 49999)
INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT '', 'secret_unsupported_marker', 100000 + n, ? FROM seq`, project.ID)

	started := time.Now()
	report, err := store.CheckSearchIndex(ctx)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if elapsed > searchIndexLargeCorruptionLimit {
		t.Fatalf("50k integrity check took %s, want <= %s", elapsed, searchIndexLargeCorruptionLimit)
	}
	t.Logf("50k integrity check: %s", elapsed)
	missing := searchIntegrityType(report, domain.SearchEntityTask).Missing
	unsupported := searchIntegrityType(report, domain.SearchEntityType(searchIndexUnsupportedType)).Unsupported
	if missing.Count != 1 || len(missing.Details) != 1 || missing.Truncated {
		t.Fatalf("missing sample = count:%d details:%d truncated:%v", missing.Count, len(missing.Details), missing.Truncated)
	}
	if unsupported.Count != 49999 || len(unsupported.Details) != searchIndexDetailLimit || !unsupported.Truncated {
		t.Fatalf("unsupported sample = count:%d details:%d truncated:%v", unsupported.Count, len(unsupported.Details), unsupported.Truncated)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal report: %v", err)
	}
	if len(encoded) > 100000 {
		t.Fatalf("bounded report JSON = %d bytes, want <= 100000", len(encoded))
	}
	if strings.Contains(string(encoded), "secret_unsupported_marker") {
		t.Fatal("bounded report disclosed unsupported metadata or content")
	}
	result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	if err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	if !result.After.Healthy || result.After.SourceTotal != 1 || result.After.IndexTotal != 1 {
		t.Fatalf("reindex after bounded corruption = %+v", result.After)
	}
}

func TestCheckSearchIndexTenThousandRowsUsesIndexedSnapshot(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	execIntegritySQL(t, ctx, store, `
WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM seq WHERE n < 10000)
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state)
SELECT ?, 1, 'healthy scale ' || n, 'body', 2, 'active' FROM seq`, project.ID)

	started := time.Now()
	report, err := store.CheckSearchIndex(ctx)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("CheckSearchIndex: %v", err)
	}
	if !report.Healthy || report.SourceTotal != 10000 || report.IndexTotal != 10000 {
		t.Fatalf("10k report = healthy:%v source:%d index:%d", report.Healthy, report.SourceTotal, report.IndexTotal)
	}
	if elapsed > searchIndexPerformanceLimit {
		t.Fatalf("10k integrity check took %s, want <= %s", elapsed, searchIndexPerformanceLimit)
	}
	t.Logf("10k integrity check: %s", elapsed)

	conn, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("db.Conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := prepareSearchCheckTablesWithHook(ctx, conn, nil); err != nil {
		t.Fatalf("prepareSearchCheckTables: %v", err)
	}
	defer func() { _ = cleanupSearchCheckTables(context.Background(), conn) }()
	planRows, err := conn.QueryContext(ctx, `EXPLAIN QUERY PLAN
SELECT c.entity_id
FROM search_check_canonical c
LEFT JOIN search_check_index i
  ON i.metadata_valid = 1 AND i.supported = 1
 AND i.entity_type = c.entity_type AND i.entity_id = c.entity_id
WHERE i.index_rowid IS NULL`)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = planRows.Close() }()
	var plan []string
	for planRows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := planRows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, " | ")
	if !strings.Contains(joined, "SEARCH i USING") || !strings.Contains(joined, "entity_id=?") || strings.Contains(joined, "VIRTUAL TABLE") {
		t.Fatalf("comparison query plan is not indexed: %s", joined)
	}
	t.Logf("comparison query plan: %s", joined)
}

func TestReindexSearchTenThousandRowsBoundsWriterLockInterval(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	project := mustUpsertProject(t, store, "P", "p", "/work/p")
	execIntegritySQL(t, ctx, store, `
WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n + 1 FROM seq WHERE n < 10000)
INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state)
SELECT ?, 1, 'writer lock scale ' || n, 'body', 2, 'active' FROM seq`, project.ID)

	started := time.Now()
	result, err := store.reindexSearchWithPolicy(ctx, checkSearchIndex, true)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("ReindexSearch: %v", err)
	}
	if !result.After.Healthy || result.After.SourceTotal != 10000 || result.After.IndexTotal != 10000 {
		t.Fatalf("10k reindex result = healthy:%v source:%d index:%d", result.After.Healthy, result.After.SourceTotal, result.After.IndexTotal)
	}
	if elapsed > searchIndexReindexLockLimit {
		t.Fatalf("10k reindex writer transaction took %s, want <= %s", elapsed, searchIndexReindexLockLimit)
	}
	t.Logf("10k reindex writer transaction: %s", elapsed)
}

func execIntegritySQL(t *testing.T, ctx context.Context, store *storeFixture, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("corruption fixture %q: %v", query, err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func searchIntegrityType(report domain.SearchIndexIntegrityReport, entityType domain.SearchEntityType) domain.SearchIndexTypeReport {
	for _, typeReport := range report.Types {
		if typeReport.EntityType == string(entityType) {
			return typeReport
		}
	}
	return domain.SearchIndexTypeReport{EntityType: string(entityType)}
}

type rollbackFailingSearchDB struct {
	operationErr error
	rollbackErr  error
}

func (db rollbackFailingSearchDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if query == "ROLLBACK" {
		return nil, db.rollbackErr
	}
	if query != "BEGIN" && db.operationErr != nil {
		return nil, db.operationErr
	}
	return nil, nil
}

func (rollbackFailingSearchDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("unexpected QueryContext call")
}

func (rollbackFailingSearchDB) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("unexpected QueryRowContext call")
}
