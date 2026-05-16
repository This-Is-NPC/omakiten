package sqlite

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

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
// projectID == 0 disables the project filter (cross-project view).
// limit clamps to a non-zero positive count; callers pass
// app.SearchServiceLimit.
//
// An invalid FTS5 MATCH expression surfaces as a coded validation_error
// so the agent layer can shape a friendly response without leaking SQL.
func (s *Store) Search(ctx context.Context, query string, projectID int64, entityTypes []domain.SearchEntityType, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("Search: limit must be > 0")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, domain.NewError(domain.ErrValidation, "query is required", nil)
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
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		// FTS5 surfaces query errors as either `fts5: syntax error
		// near "..."` (the FTS5 module's own marker) or `unterminated
		// string` (tokenizer-level). Match those tight markers only —
		// avoid catching unrelated `SQL logic error` cases (e.g. an
		// unexpected schema problem) and reporting them as user input
		// errors.
		msg := err.Error()
		if strings.Contains(msg, "fts5:") || strings.Contains(msg, "unterminated string") {
			return nil, domain.NewError(domain.ErrValidation, "invalid FTS5 query expression", map[string]any{
				"query":  query,
				"reason": msg,
			})
		}
		return nil, err
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

// includesTaskEntity reports whether the implicit `tasks.state='active'`
// filter must be applied. Empty filter ⇒ all entity types ⇒ task is in
// scope. Non-empty filter ⇒ check the slice.
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
