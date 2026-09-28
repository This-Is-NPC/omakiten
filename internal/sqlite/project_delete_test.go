package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestDeleteProjectRowsUsesEventFirstOrder(t *testing.T) {
	t.Parallel()

	executor := &recordingProjectDeleteExecutor{projectRows: 1}
	if err := deleteProjectRows(context.Background(), executor, 42); err != nil {
		t.Fatalf("deleteProjectRows: %v", err)
	}

	want := []string{
		"DELETE FROM events WHERE project_id = ?",
		"DELETE FROM projects WHERE id = ?",
	}
	if len(executor.statements) != len(want) {
		t.Fatalf("statements = %v, want %v", executor.statements, want)
	}
	for i := range want {
		if executor.statements[i] != want[i] {
			t.Errorf("statement[%d] = %q, want %q", i, executor.statements[i], want[i])
		}
		if len(executor.args[i]) != 1 || executor.args[i][0] != int64(42) {
			t.Errorf("args[%d] = %v, want [42]", i, executor.args[i])
		}
	}
}

func TestDeleteProjectWrappersPreserveDeletionContract(t *testing.T) {
	t.Parallel()

	for name, deleteProject := range projectDeleteWrappers() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "omakiten.db")
			store := openStoreFixture(t, dbPath)
			target := insertProjectDeleteFixture(t, ctx, store, "target")
			survivor := insertProjectDeleteFixture(t, ctx, store, "survivor")

			if _, err := store.db.ExecContext(ctx, `
CREATE TRIGGER project_delete_requires_event_purge
BEFORE DELETE ON projects
WHEN EXISTS (SELECT 1 FROM events WHERE project_id = OLD.id)
BEGIN
  SELECT RAISE(ABORT, 'project events must be deleted first');
END`); err != nil {
				t.Fatalf("create ordering trigger: %v", err)
			}

			if err := deleteProject(t, ctx, store.Store, dbPath, target.ID); err != nil {
				t.Fatalf("delete project: %v", err)
			}
			assertProjectNotFound(t, ctx, store.Store, target.ID)
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM events WHERE project_id = ?`, target.ID, 0)
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM tasks WHERE project_id = ?`, target.ID, 0)

			if _, err := store.FindProjectByID(ctx, survivor.ID); err != nil {
				t.Fatalf("surviving project missing: %v", err)
			}
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM events WHERE project_id = ?`, survivor.ID, 1)
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM tasks WHERE project_id = ?`, survivor.ID, 1)
		})
	}
}

func TestDeleteProjectWrappersRollBackEventDeleteFailure(t *testing.T) {
	t.Parallel()

	for name, deleteProject := range projectDeleteWrappers() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "omakiten.db")
			store := openStoreFixture(t, dbPath)
			project := insertProjectDeleteFixture(t, ctx, store, "target")

			if _, err := store.db.ExecContext(ctx, `
CREATE TRIGGER fail_project_event_delete
BEFORE DELETE ON events
BEGIN
  SELECT RAISE(ABORT, 'forced event delete failure');
END`); err != nil {
				t.Fatalf("create failure trigger: %v", err)
			}

			err := deleteProject(t, ctx, store.Store, dbPath, project.ID)
			if err == nil || !strings.Contains(err.Error(), "forced event delete failure") {
				t.Fatalf("delete error = %v, want forced event delete failure", err)
			}
			if _, err := store.FindProjectByID(ctx, project.ID); err != nil {
				t.Fatalf("project missing after rollback: %v", err)
			}
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM events WHERE project_id = ?`, project.ID, 1)
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM tasks WHERE project_id = ?`, project.ID, 1)
		})
	}
}

func TestDeleteProjectWrappersReturnExistingNotFoundError(t *testing.T) {
	t.Parallel()

	for name, deleteProject := range projectDeleteWrappers() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "omakiten.db")
			store := openStoreFixture(t, dbPath)
			survivor := insertProjectDeleteFixture(t, ctx, store, "survivor")
			const unknownProjectID int64 = 4242

			err := deleteProject(t, ctx, store.Store, dbPath, unknownProjectID)
			var coded *domain.CodedError
			if !errors.As(err, &coded) {
				t.Fatalf("delete error = %T (%v), want *domain.CodedError", err, err)
			}
			if coded.Code != domain.ErrProjectNotFound || coded.Message != "project not found" {
				t.Fatalf("coded error = %#v, want project_not_found: project not found", coded)
			}
			if coded.Details["project_id"] != unknownProjectID {
				t.Fatalf("project_id detail = %v, want %d", coded.Details["project_id"], unknownProjectID)
			}

			if _, err := store.FindProjectByID(ctx, survivor.ID); err != nil {
				t.Fatalf("surviving project missing: %v", err)
			}
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM events WHERE project_id = ?`, survivor.ID, 1)
			assertProjectDeleteRowCount(t, ctx, store.db, `SELECT COUNT(*) FROM tasks WHERE project_id = ?`, survivor.ID, 1)
		})
	}
}

type projectDeleteWrapper func(*testing.T, context.Context, *Store, string, int64) error

func projectDeleteWrappers() map[string]projectDeleteWrapper {
	return map[string]projectDeleteWrapper{
		"transaction": func(_ *testing.T, ctx context.Context, store *Store, _ string, projectID int64) error {
			return store.DeleteProject(ctx, projectID)
		},
		"exact-generation backup": func(t *testing.T, ctx context.Context, store *Store, dbPath string, projectID int64) error {
			create, discard, _ := atomicDeleteBackupCallbacks(t, dbPath)
			_, err := store.DeleteProjectWithBackup(ctx, projectID, create, discard, func() error { return nil })
			return err
		},
	}
}

func insertProjectDeleteFixture(t *testing.T, ctx context.Context, store *storeFixture, slug string) domain.Project {
	t.Helper()
	project, err := store.UpsertProject(ctx, slug, slug, filepath.Join(t.TempDir(), slug))
	if err != nil {
		t.Fatalf("UpsertProject(%q): %v", slug, err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (?, 1, 'seed', '', 2, 'active')`, project.ID); err != nil {
		t.Fatalf("insert task for %q: %v", slug, err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO events(entity_type, entity_id, project_id, event_type, payload) VALUES ('project', ?, ?, 'test.event', '{}')`, project.ID, project.ID); err != nil {
		t.Fatalf("insert event for %q: %v", slug, err)
	}
	return project
}

func assertProjectNotFound(t *testing.T, ctx context.Context, store *Store, projectID int64) {
	t.Helper()
	_, err := store.FindProjectByID(ctx, projectID)
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("FindProjectByID(%d) error = %v, want project_not_found", projectID, err)
	}
}

func assertProjectDeleteRowCount(t *testing.T, ctx context.Context, db *sql.DB, query string, projectID int64, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, projectID).Scan(&got); err != nil {
		t.Fatalf("query row count: %v", err)
	}
	if got != want {
		t.Fatalf("row count = %d, want %d", got, want)
	}
}

type recordingProjectDeleteExecutor struct {
	statements  []string
	args        [][]any
	projectRows int64
}

func (e *recordingProjectDeleteExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.statements = append(e.statements, strings.Join(strings.Fields(query), " "))
	e.args = append(e.args, args)
	if strings.Contains(query, "DELETE FROM projects") {
		return driver.RowsAffected(e.projectRows), nil
	}
	return driver.RowsAffected(0), nil
}
