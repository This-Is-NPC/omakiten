package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"omakiten/internal/domain"
)

func (s *Store) UpsertProject(ctx context.Context, name, slug, rootPath string) (domain.Project, error) {
	return s.scanProject(s.query(ctx).QueryRowContext(ctx, `
INSERT INTO projects(uid, name, slug, root_path, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(root_path) DO UPDATE SET
  name = excluded.name,
  slug = excluded.slug,
  updated_at = CURRENT_TIMESTAMP
RETURNING `+projectColumns, domain.NewUID(), name, slug, rootPath))
}

// UpdateProject applies the non-nil patch fields to a live (non-archived)
// project and returns the refreshed row. An unknown or archived id matches
// no row, surfacing ErrProjectNotFound via scanProject's sql.ErrNoRows
// branch. Switching the scope from local to synced records every existing row
// of the project for the next push.
func (s *Store) UpdateProject(ctx context.Context, id int64, patch domain.ProjectPatch) (project domain.Project, err error) {
	err = s.WithinTransaction(ctx, func(ctx context.Context) error {
		var before domain.SyncScope
		if patch.SyncScope != nil {
			// A write returns the scope the update replaces and takes the write lock first.
			before, err = s.currentScope(ctx, id)
			if err != nil {
				return err
			}
		}
		project, err = s.scanProject(s.query(ctx).QueryRowContext(ctx, `
UPDATE projects SET
  description = COALESCE(?, description),
  sync_scope = COALESCE(?, sync_scope),
  updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND archived_at IS NULL
RETURNING `+projectColumns, patch.Description, patch.SyncScope, id))
		if err != nil {
			return err
		}
		if before == domain.SyncScopeLocal && project.SyncScope == domain.SyncScopeSynced {
			return s.enqueueProject(ctx, id)
		}
		return nil
	})
	return project, err
}

// currentScope returns the sync scope of a live project through a no-op write.
func (s *Store) currentScope(ctx context.Context, id int64) (domain.SyncScope, error) {
	var scope domain.SyncScope
	err := s.query(ctx).QueryRowContext(ctx,
		`UPDATE projects SET sync_scope = sync_scope WHERE id = ? AND archived_at IS NULL RETURNING sync_scope`, id).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.NewError(domain.ErrProjectNotFound, "project not found", nil)
	}
	return scope, err
}

func (s *Store) FindProjectByID(ctx context.Context, id int64) (domain.Project, error) {
	return s.scanProject(s.query(ctx).QueryRowContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE id = ? AND archived_at IS NULL", id))
}

func (s *Store) FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error) {
	return s.scanProject(s.query(ctx).QueryRowContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE slug = ? AND archived_at IS NULL", slug))
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.query(ctx).QueryContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE archived_at IS NULL ORDER BY LOWER(name), id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []domain.Project
	for rows.Next() {
		project, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) FindProjectsContainingPath(ctx context.Context, path string) ([]domain.Project, error) {
	rows, err := s.query(ctx).QueryContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE archived_at IS NULL ORDER BY length(root_path) DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []domain.Project
	for rows.Next() {
		project, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		if pathWithinRoot(path, project.RootPath) {
			projects = append(projects, project)
		}
	}
	return projects, rows.Err()
}

// projectColumns is the column list scanProjectRow reads.
const projectColumns = "id, name, slug, root_path, description, sync_scope"

func scanProjectRow(row rowScanner) (domain.Project, error) {
	var project domain.Project
	err := row.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath, &project.Description, &project.SyncScope)
	return project, err
}

func (s *Store) scanProject(row *sql.Row) (domain.Project, error) {
	project, err := scanProjectRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, domain.NewError(domain.ErrProjectNotFound, "project not found", nil)
		}
		return domain.Project{}, err
	}
	return project, nil
}

// ProjectDeleteCounts returns the per-table row count snapshot
// rendered before a destructive ProjectService.Delete. Tags counts
// the project_tags bridge (project-scoped tag attachments), not the
// global tags table. ActivityLogEntries sums every per-call activity
// log row (events.event_type in operation / cli.tool_call /
// cli.tool_call / tui.tool_call) the project accumulated.
//
// Counters are best-effort — concurrent writes between the read and
// the eventual DELETE are accepted; the contract is "what the user
// sees in the prompt is what was on disk at prompt time", not a hard
// guarantee that the delete operates on exactly those rows.
func (s *Store) ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error) {
	const query = `
		SELECT
			(SELECT COUNT(*) FROM tasks         WHERE project_id = ?1),
			(SELECT COUNT(*) FROM events        WHERE project_id = ?1 AND event_type = 'comment'),
			(SELECT COUNT(*) FROM plans         WHERE project_id = ?1),
			(SELECT COUNT(*) FROM project_tags  WHERE project_id = ?1),
			(SELECT COUNT(*) FROM events        WHERE project_id = ?1 AND event_type IN ('operation', 'cli.tool_call', 'cli.tool_call', 'tui.tool_call'))
	`
	var counters domain.ProjectDeleteCounters
	if err := s.query(ctx).QueryRowContext(ctx, query, projectID).Scan(
		&counters.Tasks,
		&counters.Comments,
		&counters.Plans,
		&counters.Tags,
		&counters.ActivityLogEntries,
	); err != nil {
		return domain.ProjectDeleteCounters{}, err
	}
	return counters, nil
}

type projectDeleteExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func deleteProjectRows(ctx context.Context, executor projectDeleteExecutor, projectID int64) error {
	if _, err := executor.ExecContext(ctx, `DELETE FROM events WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	res, err := executor.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.NewError(domain.ErrProjectNotFound, "project not found", map[string]any{"project_id": projectID})
	}
	return nil
}

// DeleteProject hard-deletes a project row in a single transaction.
// The current FK CASCADE chain takes care of
// tasks (→ task_tags, task_dependencies), plans (→ plan_waves),
// errors (→ solutions, error_tags), and project_tags. Event rows
// have no FK to projects so they would
// linger as orphans pointing at a gone project_id — we delete them
// explicitly inside the same transaction so the activity feed stays
// consistent with the project being gone.
func (s *Store) DeleteProject(ctx context.Context, projectID int64) error {
	tx, err := s.beginTransaction(ctx)
	if err != nil {
		return err
	}
	if err := deleteProjectRows(ctx, tx, projectID); err != nil {
		_ = s.rollbackTransaction(ctx, tx)
		return err
	}
	return s.commitTransaction(ctx, tx)
}

type projectDeleteBackupHooks struct {
	Generation   exactGenerationHooks
	BeforeCommit func(attempt int) error
	AfterConnect func(*sql.Conn)
	Rollback     func(context.Context, *sql.Conn) error
}

// DeleteProjectWithBackup retains an exact image of the generation it deletes.
// One pinned live connection reads data_version around the verified snapshot,
// acquires BEGIN IMMEDIATE, and reads the watermark again under the writer lock.
// Any external commit in either interval discards the candidate and retries.
func (s *Store) DeleteProjectWithBackup(
	ctx context.Context,
	projectID int64,
	createBackup func(context.Context, func(destinationPath string) error) (string, error),
	discardBackup func(string) error,
	validateLease func() error,
) (string, error) {
	return s.deleteProjectWithBackup(ctx, projectID, createBackup, discardBackup, validateLease, projectDeleteBackupHooks{})
}

func (s *Store) deleteProjectWithBackup(
	ctx context.Context,
	projectID int64,
	createBackup func(context.Context, func(destinationPath string) error) (string, error),
	discardBackup func(string) error,
	validateLease func() error,
	hooks projectDeleteBackupHooks,
) (string, error) {
	if createBackup == nil || discardBackup == nil || validateLease == nil {
		return "", errors.New("atomic project delete requires backup create, discard, and lease validation callbacks")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("pin project-delete connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if hooks.AfterConnect != nil {
		hooks.AfterConnect(conn)
	}
	transaction := transactionControl{
		invalidate: func() {
			invalidateSQLiteConn(conn)
		},
		rollbackLabel: "roll back project delete",
	}
	if hooks.Rollback != nil {
		transaction.rollback = func(rollbackCtx context.Context, _ sqliteTransaction) error {
			return hooks.Rollback(rollbackCtx, conn)
		}
	}
	backupPath, attempt, err := prepareExactGeneration(ctx, conn, projectDeleteExactGenerationPolicy, exactGenerationConfig{
		create:  createBackup,
		discard: discardBackup,
		snapshot: func(snapshotCtx context.Context, destinationPath string) error {
			return snapshotWithExecutor(snapshotCtx, conn, destinationPath, false, snapshotHooks{})
		},
		beforeSnapshot: validateLease,
		afterSnapshot:  validateLease,
		hooks:          hooks.Generation,
		transaction:    transaction,
	})
	if err != nil {
		return backupPath, err
	}

	// From this point onward a destructive statement has been attempted.
	// Preserve backupPath on every error even when rollback succeeds.
	if err := deleteProjectRows(ctx, conn, projectID); err != nil {
		return backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, transaction))
	}
	if hooks.BeforeCommit != nil {
		if err := hooks.BeforeCommit(attempt); err != nil {
			return backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, transaction))
		}
	}
	if err := validateLease(); err != nil {
		return backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, transaction))
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return backupPath, errors.Join(err, rollbackTransactionControlled(ctx, conn, transaction))
	}
	return backupPath, nil
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}
