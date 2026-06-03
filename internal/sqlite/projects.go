package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"omakiten/internal/domain"
)

func (s *Store) UpsertProject(ctx context.Context, name, slug, rootPath string) (domain.Project, error) {
	row := s.db.QueryRowContext(ctx, `
INSERT INTO projects(name, slug, root_path, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(root_path) DO UPDATE SET
  name = excluded.name,
  slug = excluded.slug,
  updated_at = CURRENT_TIMESTAMP
RETURNING id, name, slug, root_path
`, name, slug, rootPath)

	var project domain.Project
	if err := row.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath); err != nil {
		return domain.Project{}, err
	}
	return project, nil
}

// UpdateProjectDescription persists a new description onto a live
// (non-archived) project and returns the refreshed row. The
// projects.description column has existed since migration 002 but had
// no write path; this restores it. An unknown or archived id matches
// no row, surfacing ErrProjectNotFound via scanProject's sql.ErrNoRows
// branch.
func (s *Store) UpdateProjectDescription(ctx context.Context, id int64, description string) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, `
UPDATE projects SET description = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND archived_at IS NULL
RETURNING id, name, slug, root_path, description
`, description, id))
}

func (s *Store) FindProjectByID(ctx context.Context, id int64) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, "SELECT id, name, slug, root_path, description FROM projects WHERE id = ? AND archived_at IS NULL", id))
}

func (s *Store) FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, "SELECT id, name, slug, root_path, description FROM projects WHERE slug = ? AND archived_at IS NULL", slug))
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, slug, root_path, description FROM projects WHERE archived_at IS NULL ORDER BY LOWER(name), id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []domain.Project
	for rows.Next() {
		var project domain.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath, &project.Description); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) FindProjectsContainingPath(ctx context.Context, path string) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE archived_at IS NULL ORDER BY length(root_path) DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []domain.Project
	for rows.Next() {
		var project domain.Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath); err != nil {
			return nil, err
		}
		if pathWithinRoot(path, project.RootPath) {
			projects = append(projects, project)
		}
	}
	return projects, rows.Err()
}

func (s *Store) scanProject(row *sql.Row) (domain.Project, error) {
	var project domain.Project
	if err := row.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath, &project.Description); err != nil {
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
// mcp.tool_call / tui.tool_call) the project accumulated.
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
			(SELECT COUNT(*) FROM events        WHERE project_id = ?1 AND event_type IN ('operation', 'cli.tool_call', 'mcp.tool_call', 'tui.tool_call'))
	`
	var counters domain.ProjectDeleteCounters
	if err := s.db.QueryRowContext(ctx, query, projectID).Scan(
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

// DeleteProject hard-deletes a project row in a single transaction.
// The FK CASCADE chain installed by migration 025 takes care of
// tasks (→ task_tags, task_dependencies), plans (→ plan_waves),
// errors (→ solutions, error_tags), and project_tags. Event rows
// have no FK to projects so they would
// linger as orphans pointing at a gone project_id — we delete them
// explicitly inside the same transaction so the activity feed stays
// consistent with the project being gone.
func (s *Store) DeleteProject(ctx context.Context, projectID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE project_id = ?`, projectID); err != nil {
		_ = tx.Rollback()
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if rows == 0 {
		_ = tx.Rollback()
		return domain.NewError(domain.ErrProjectNotFound, "project not found", map[string]any{"project_id": projectID})
	}
	return tx.Commit()
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
