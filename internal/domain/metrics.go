package domain

// AgentMetrics aggregates per-AI-model behaviour over a period. Drives the
// /metrics.summary tool: each row is one model, computed from the unified
// events log filtered to domain events (error.recorded, error.searched,
// solution.added, solution.liked, solution.failed, solution.viewed_top).
//
// LikeRate divides liked solutions by added solutions (0 when none added).
// SearchBeforeRecordRatio is computed only over errors recorded with a
// non-empty agent_session_id, since correlating searches to records
// requires session continuity. Models that never pass _agent_session_id
// will report 0.0 even if they search heavily — that is by design.
type AgentMetrics struct {
	AgentModel              string  `json:"agent_model"`
	ErrorsRecorded          int     `json:"errors_recorded"`
	ErrorsSearched          int     `json:"errors_searched"`
	SolutionsAdded          int     `json:"solutions_added"`
	SolutionsLiked          int     `json:"solutions_liked"`
	SolutionsFailed         int     `json:"solutions_failed"`
	SolutionsTopViewed      int     `json:"solutions_top_viewed"`
	LikeRate                float64 `json:"like_rate"`
	SearchBeforeRecordRatio float64 `json:"search_before_record_ratio"`
	SessionCorrelatedSample int     `json:"session_correlated_sample"`
}

type MetricsSummary struct {
	Period   string         `json:"period"`
	Since    string         `json:"since,omitempty"`
	ByModel  []AgentMetrics `json:"by_model"`
	Total    AgentMetrics   `json:"total"`
}
