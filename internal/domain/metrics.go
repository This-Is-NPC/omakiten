package domain

// EventMetricBucket is the typed tag that ties a /metrics.summary bucket to
// the YAML registry's `metric:` field. Adding a new bucket is now a
// two-step change: declare the const here, then point the kit YAML entry
// at the matching value. The SQL builder in internal/sqlite walks
// EventDefinitions for every def whose Metric is non-empty and projects
// its count into Buckets[EventMetricBucket(def.Metric)] — no flat-field
// listing to keep in sync.
type EventMetricBucket string

const (
	MetricBucketErrorRecorded     EventMetricBucket = "error_recorded"
	MetricBucketErrorsResearched  EventMetricBucket = "error_searched"
	MetricBucketSolutionAdded     EventMetricBucket = "solution_added"
	MetricBucketSolutionLiked     EventMetricBucket = "solution_liked"
	MetricBucketSolutionFailed    EventMetricBucket = "solution_failed"
	MetricBucketSolutionTopViewed EventMetricBucket = "solution_top_viewed"
)

// AgentMetrics aggregates per-AI-model behaviour over a period. Drives the
// /metrics.summary tool: each row is one model, computed from the unified
// events log filtered to domain events tagged with a `metric:` value in
// the YAML registry (error.recorded, errors.researched, solution.added,
// solution.liked, solution.failed, solution.viewed_top).
//
// LikeRate divides liked solutions by added solutions (0 when none added).
// SearchBeforeRecordRatio is computed only over errors recorded with a
// non-empty agent_session_id, since correlating searches to records
// requires session continuity. Models that never pass _agent_session_id
// will report 0.0 even if they search heavily — that is by design.
//
// Buckets is keyed by the EventMetricBucket const matching the YAML
// `metric:` field. Consumers must read counts via the typed key rather
// than dotted event_type strings so a kit-level rename of an event_type
// (Phase 4) does not break the metric tag contract.
type AgentMetrics struct {
	AgentModel              string                    `json:"agent_model"`
	Buckets                 map[EventMetricBucket]int `json:"buckets"`
	LikeRate                float64                   `json:"like_rate"`
	SearchBeforeRecordRatio float64                   `json:"search_before_record_ratio"`
	SessionCorrelatedSample int                       `json:"session_correlated_sample"`
}

type MetricsSummary struct {
	Period  string         `json:"period"`
	Since   string         `json:"since,omitempty"`
	ByModel []AgentMetrics `json:"by_model"`
	Total   AgentMetrics   `json:"total"`
}
