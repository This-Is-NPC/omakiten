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

func (s *Store) FindProjectByID(ctx context.Context, id int64) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE id = ? AND archived_at IS NULL", id))
}

func (s *Store) FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error) {
	return s.scanProject(s.db.QueryRowContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE slug = ? AND archived_at IS NULL", slug))
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, slug, root_path FROM projects WHERE archived_at IS NULL ORDER BY LOWER(name), id")
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
	if err := row.Scan(&project.ID, &project.Name, &project.Slug, &project.RootPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, domain.NewError(domain.ErrProjectNotFound, "project not found", nil)
		}
		return domain.Project{}, err
	}
	return project, nil
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
