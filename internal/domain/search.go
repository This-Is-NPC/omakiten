package domain

// SearchEntityType labels a row inside the unified FTS5 `search_index`.
// The five values mirror the source tables the index syncs from; any
// other value is rejected at the service boundary so the SQL filter
// only sees a closed set.
type SearchEntityType string

const (
	SearchEntityTask     SearchEntityType = "task"
	SearchEntityComment  SearchEntityType = "comment"
	SearchEntityError    SearchEntityType = "error"
	SearchEntitySolution SearchEntityType = "solution"
	SearchEntityContext  SearchEntityType = "context"
	SearchEntityPlan     SearchEntityType = "plan"
)

// AllSearchEntityTypes is the canonical set of entity types the FTS5
// index covers. Callers passing an empty `entity_types` list inherit
// this set.
func AllSearchEntityTypes() []SearchEntityType {
	return []SearchEntityType{
		SearchEntityTask,
		SearchEntityComment,
		SearchEntityError,
		SearchEntitySolution,
		SearchEntityContext,
		SearchEntityPlan,
	}
}

// IsValidSearchEntityType reports whether the supplied value matches one
// of the six indexed entity types.
func IsValidSearchEntityType(value string) bool {
	switch SearchEntityType(value) {
	case SearchEntityTask, SearchEntityComment, SearchEntityError, SearchEntitySolution, SearchEntityContext, SearchEntityPlan:
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
