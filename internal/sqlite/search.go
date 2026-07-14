package sqlite

import (
	"context"
	"strings"

	"omakiten/internal/domain"
)

const searchResultLimit = 200

// Search runs an FTS5 MATCH against the unified `search_index` virtual
// table created by migration 022. Score is computed as `-bm25(...)` so
// callers can sort/expect "larger is better" without knowing FTS5's
// convention that raw bm25 returns negative values where smaller (more
// negative) is more relevant.
//
// Implicit filter: when entityTypes is empty OR includes "task", a
// LEFT JOIN against the live `tasks` table excludes archived rows.
// Other entity types have no archive concept and bypass the JOIN.
//
// projectID == 0 disables the project filter (cross-project view). Results are
// capped here so every app and internal repository caller shares one limit.
//
// An invalid FTS5 MATCH expression surfaces as a coded validation_error
// so the agent layer can shape a friendly response without leaking SQL.
func (s *Store) Search(ctx context.Context, query string, projectID int64, entityTypes []domain.SearchEntityType) ([]domain.SearchHit, error) {
	query, err := domain.ValidateSearchQuery(query)
	if err != nil {
		return nil, err
	}
	if len(entityTypes) == 0 {
		entityTypes = domain.AllSearchEntityTypes()
	}

	includeTask := includesTaskEntity(entityTypes)

	var b strings.Builder
	args := []any{}

	b.WriteString(`SELECT si.entity_type, si.entity_id, -bm25(search_index) AS score, ` +
		`snippet(search_index, 0, '<mark>', '</mark>', '…', 32) AS snippet, si.project_id ` +
		`FROM search_index si `)

	if includeTask {
		b.WriteString(`LEFT JOIN tasks t ON (si.entity_type = 'task' AND t.id = si.entity_id) `)
	}

	b.WriteString(`WHERE search_index MATCH ? `)
	args = append(args, query)

	if includeTask {
		// Drop archived task rows; non-task rows are untouched because the
		// disjunction short-circuits on entity_type.
		b.WriteString(`AND (si.entity_type != 'task' OR t.state = 'active') `)
	}

	if len(entityTypes) > 0 {
		placeholders := make([]string, len(entityTypes))
		for i, et := range entityTypes {
			placeholders[i] = "?"
			args = append(args, string(et))
		}
		b.WriteString(`AND si.entity_type IN (`)
		b.WriteString(strings.Join(placeholders, ","))
		b.WriteString(`) `)
	}

	if projectID > 0 {
		b.WriteString(`AND si.project_id = ? `)
		args = append(args, projectID)
	}

	b.WriteString(`ORDER BY score DESC LIMIT ?`)
	args = append(args, searchResultLimit)

	rows, err := s.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, classifyFTSQueryError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.SearchHit
	for rows.Next() {
		var hit domain.SearchHit
		var entityType string
		if err := rows.Scan(&entityType, &hit.ID, &hit.Score, &hit.Snippet, &hit.ProjectID); err != nil {
			return nil, err
		}
		hit.EntityType = domain.SearchEntityType(entityType)
		out = append(out, hit)
	}
	return out, rows.Err()
}

func classifyFTSQueryError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "fts5:") || strings.Contains(msg, "unterminated string") {
		return domain.NewError(domain.ErrValidation, "invalid FTS5 query expression", nil)
	}
	return err
}

// includesTaskEntity reports whether the implicit `tasks.state='active'`
// filter must be applied. Empty filter means all five entity types, so task
// is in scope. Non-empty filter requires checking the slice.
func includesTaskEntity(entityTypes []domain.SearchEntityType) bool {
	if len(entityTypes) == 0 {
		return true
	}
	for _, t := range entityTypes {
		if t == domain.SearchEntityTask {
			return true
		}
	}
	return false
}
