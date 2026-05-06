package sqlite

import (
	"context"
	"database/sql"
	"strconv"

	"omakiten/internal/domain"
)

func (s *Store) ContextSettings(ctx context.Context) (domain.ContextSettings, error) {
	settings := domain.ContextSettings{DefaultLevel: 2, MaxTokens: 12000}
	rows, err := s.db.QueryContext(ctx, `
SELECT settings.key, settings.value
FROM settings
JOIN config_bundles ON config_bundles.id = settings.bundle_id
WHERE settings.active = 1
  AND config_bundles.active = 1
  AND settings.key IN ('context.default_level', 'context.max_tokens')
ORDER BY config_bundles.id DESC
`)
	if err != nil {
		return domain.ContextSettings{}, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return domain.ContextSettings{}, err
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return domain.ContextSettings{}, domain.NewError(domain.ErrConfigInvalid, "context setting must be numeric", map[string]any{"key": key, "value": value})
		}
		switch key {
		case "context.default_level":
			settings.DefaultLevel = parsed
		case "context.max_tokens":
			settings.MaxTokens = parsed
		}
	}
	return settings, rows.Err()
}

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
