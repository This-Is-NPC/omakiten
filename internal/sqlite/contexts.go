package sqlite

import (
	"context"
	"database/sql"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// kitContextSettings reads the canonical context block from the embedded
// kit YAML. Used as the seed in ContextSettings before the SELECT
// possibly overrides per-key. Falls back to {1, 0} if the embedded YAML
// is unparseable so the binary keeps booting and the validator's later
// pass surfaces the real failure.
func kitContextSettings() domain.ContextSettings {
	cfg, err := config.LoadKitConfig()
	if err != nil {
		return domain.ContextSettings{DefaultLevel: 1, MaxTokens: 0}
	}
	return domain.ContextSettings{
		DefaultLevel: cfg.Context.DefaultLevel,
		MaxTokens:    cfg.Context.MaxTokens,
	}
}

func (s *Store) ContextSettings(_ context.Context) (domain.ContextSettings, error) {
	// The SQL `settings` table was dropped in migration 020 and the
	// per-project Snapshot now owns the read. The kit canonical (read
	// from the embedded YAML) seeds the bootstrap window between
	// sqlite.Open and the first ImportBundle so callers never see a
	// zero-value response. TRANSITIONAL: ContextSettings remains on
	// the Store solely to satisfy the legacy ConfigRepository surface;
	// app.ContextService now reads s.snap.ContextSettings() directly.
	out := kitContextSettings()
	cfg := s.Snapshot().Settings()
	if cfg.Context.DefaultLevel != 0 {
		out.DefaultLevel = cfg.Context.DefaultLevel
	}
	if cfg.Context.MaxTokens != 0 {
		out.MaxTokens = cfg.Context.MaxTokens
	}
	return out, nil
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
