package agent

import "omakiten/internal/domain"

// SearchInput is the MCP shape for the unified `search` tool. EntityTypes
// is an array of {task, comment, error, solution, context}; empty means
// "all five". Query is an FTS5 MATCH expression — the adapter forwards
// it to SQLite verbatim, so callers can use phrase, prefix, NEAR, AND/
// OR/NOT, and column filters as documented at
// https://sqlite.org/fts5.html#full_text_query_syntax.
type SearchInput struct {
	ProjectSelector
	Query       string   `json:"query"`
	EntityTypes []string `json:"entity_types,omitempty"`
}

// SearchHitDTO is one ranked row returned by `search`. Score is the
// FTS5 BM25 ranking normalised so higher = more relevant (the adapter
// inverts the raw bm25() value internally). Snippet contains the matching
// content slice with `<mark>…</mark>` wrapping each query-matching token.
// ProjectID stays in the payload even when the caller passed a project
// filter so cross-entity result lists remain self-describing.
type SearchHitDTO struct {
	EntityType string  `json:"entity_type"`
	ID         int64   `json:"id"`
	Score      float64 `json:"score"`
	Snippet    string  `json:"snippet"`
	ProjectID  int64   `json:"project_id"`
}

// SearchResponse wraps the hit list with the resolved project summary so
// MCP clients can confirm which project the cross-entity search filtered
// on (or whether it was cross-project).
type SearchResponse struct {
	Project ProjectSummary `json:"project"`
	Hits    []SearchHitDTO `json:"hits"`
}

func searchHitDTO(h domain.SearchHit) SearchHitDTO {
	return SearchHitDTO{
		EntityType: string(h.EntityType),
		ID:         h.ID,
		Score:      h.Score,
		Snippet:    h.Snippet,
		ProjectID:  h.ProjectID,
	}
}
