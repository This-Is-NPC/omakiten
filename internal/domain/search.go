package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SearchEntityType labels a row inside the unified FTS5 `search_index`.
// The values mirror the source tables the index syncs from; any other
// value is rejected at the service boundary so the SQL filter only sees
// a closed set.
type SearchEntityType string

const (
	SearchEntityTask     SearchEntityType = "task"
	SearchEntityComment  SearchEntityType = "comment"
	SearchEntityError    SearchEntityType = "error"
	SearchEntitySolution SearchEntityType = "solution"
	SearchEntityPlan     SearchEntityType = "plan"

	// SearchQueryMaxBytes and SearchQueryMaxTokens bound work before a query
	// reaches telemetry or SQLite's FTS5 parser. Bytes bound storage and copy
	// cost; lexical tokens bound parser/expression amplification.
	SearchQueryMaxBytes  = 4096
	SearchQueryMaxTokens = 256
)

// ValidateSearchQuery trims and bounds one FTS5 MATCH expression. All app and
// repository entry points use this validator so internal callers cannot bypass
// the same CPU/storage limits enforced for MCP requests.
func ValidateSearchQuery(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", NewError(ErrValidation, "query is required", nil)
	}
	if len(query) > SearchQueryMaxBytes || !utf8.ValidString(query) {
		return "", searchQueryLimitError()
	}
	if searchQueryLexicalTokens(query) > SearchQueryMaxTokens {
		return "", searchQueryLimitError()
	}
	return query, nil
}

func searchQueryLimitError() error {
	return NewError(ErrValidation, "search query exceeds limits", map[string]any{
		"max_bytes":  SearchQueryMaxBytes,
		"max_tokens": SearchQueryMaxTokens,
	})
}

func searchQueryLexicalTokens(query string) int {
	tokens := 0
	inWord := false
	for _, value := range query {
		if unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' {
			if !inWord {
				tokens++
				inWord = true
			}
			continue
		}
		inWord = false
		if !unicode.IsSpace(value) && value != '\'' && value != '"' && value != '`' {
			tokens++
		}
	}
	return tokens
}

// AllSearchEntityTypes is the canonical set of entity types the FTS5
// index covers. Callers passing an empty `entity_types` list inherit
// this set.
func AllSearchEntityTypes() []SearchEntityType {
	return []SearchEntityType{
		SearchEntityTask,
		SearchEntityComment,
		SearchEntityError,
		SearchEntitySolution,
		SearchEntityPlan,
	}
}

// IsValidSearchEntityType reports whether the supplied value matches one
// of the indexed entity types.
func IsValidSearchEntityType(value string) bool {
	switch SearchEntityType(value) {
	case SearchEntityTask, SearchEntityComment, SearchEntityError, SearchEntitySolution, SearchEntityPlan:
		return true
	}
	return false
}

// SearchHit is one row returned by app.SearchService.Search. Score is the
// FTS5 BM25 ranking normalised so larger is better (the adapter inverts
// the raw bm25() output internally — callers consume DESC order).
// Snippet wraps query-matching tokens with `<mark>…</mark>` for the
// `content` column.
type SearchHit struct {
	EntityType SearchEntityType `json:"entity_type"`
	ID         int64            `json:"id"`
	Score      float64          `json:"score"`
	Snippet    string           `json:"snippet"`
	ProjectID  int64            `json:"project_id"`
}
