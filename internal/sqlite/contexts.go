package sqlite

import (
	"context"
	"database/sql"

	"omakiten/internal/domain"
)

func (s *Store) AddContextEntry(ctx context.Context, projectID int64, body string, tokenEstimate int) (domain.ContextEntry, error) {
	row := s.db.QueryRowContext(ctx, `
INSERT INTO context_entries(project_id, body, token_estimate)
VALUES (?, ?, ?)
RETURNING id, project_id, body, token_estimate, created_at
`, projectID, body, tokenEstimate)
	return scanContextEntry(row)
}

func (s *Store) ListContextEntries(ctx context.Context, projectID int64) ([]domain.ContextEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, project_id, body, token_estimate, created_at FROM context_entries WHERE project_id = ? ORDER BY id DESC", projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []domain.ContextEntry
	for rows.Next() {
		var entry domain.ContextEntry
		if err := rows.Scan(&entry.ID, &entry.ProjectID, &entry.Body, &entry.TokenEstimate, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func scanContextEntry(row *sql.Row) (domain.ContextEntry, error) {
	var entry domain.ContextEntry
	if err := row.Scan(&entry.ID, &entry.ProjectID, &entry.Body, &entry.TokenEstimate, &entry.CreatedAt); err != nil {
		return domain.ContextEntry{}, err
	}
	return entry, nil
}
