package domain

// DefaultStuckDays is the canonical fallback for the stuck-task staleness
// threshold (insight 1). It lives in domain — not app or sqlite — so both
// layers reference one value and can never disagree on the default; each
// still applies it defensively when handed stuckDays <= 0.
const DefaultStuckDays = 7

// Insights bundles the six "today-computable" intelligence-layer readings
// the InsightsService derives on demand (no cache) from the unified events
// log plus the errors/solutions tables. Each sub-report carries an explicit
// HasData flag so a renderer can distinguish "no history to compute from"
// (HasData=false) from a genuine zero reading (HasData=true, empty/zero
// payload) — a silent zero would otherwise misread an empty board as a
// healthy one.
//
// All six are read-only aggregations; nothing here mutates state. The
// per-insight doc on each field names its source columns, its acceptance
// criterion, and the one-line agent-actionable response.
type Insights struct {
	// StuckDays is the threshold (in days) that the stuck-task scan used,
	// echoed back so a renderer can label "stuck > N days".
	StuckDays int `json:"stuck_days"`

	Stuck     StuckInsight     `json:"stuck"`
	CycleTime CycleTimeInsight `json:"cycle_time"`
	WIP       WIPInsight       `json:"wip"`
	Guards    GuardInsight     `json:"guards"`
	ErrorLoop ErrorLoopInsight `json:"error_loop"`
	PerModel  PerModelInsight  `json:"per_model"`
}

// StuckInsight — INSIGHT 1: tasks parked too long.
//
// Source: events(entity_type='task', event_type='task.moved') — the MAX
// created_at per task is when it last changed bucket; tasks(bucket_id,
// state, title) supplies current position. A task counts when its current
// bucket is dev(2) or review(3) and the last move is older than StuckDays.
//
// Acceptance: returns one row per task in bucket 2/3 whose last task.moved
// is > StuckDays days old, ordered by days_stuck desc.
//
// Agent action: "Task #<id> has sat in <bucket> for <days_stuck>d — pick it
// up, unblock it, or move it back to backlog."
type StuckInsight struct {
	HasData bool        `json:"has_data"`
	Tasks   []StuckTask `json:"tasks"`
}

type StuckTask struct {
	TaskID    int64  `json:"task_id"`
	BucketID  int64  `json:"bucket_id"`
	DaysStuck int    `json:"days_stuck"`
	Title     string `json:"title"`
}

// CycleTimeInsight — INSIGHT 2: dwell + bottleneck per from-bucket.
//
// Source: events(entity_type='task', event_type='task.moved'); dwell in a
// bucket is the gap between the move that entered it (LAG over created_at
// partitioned by entity_id) and the move that left it, attributed to
// json_extract(payload,'$.from'). Averaged per from-bucket.
//
// Acceptance: one row per from-bucket that has at least one completed dwell
// interval, with avg_dwell_days and the sample count; the bucket with the
// largest avg is the Bottleneck.
//
// Agent action: "<bottleneck> is the slowest stage at <avg>d avg dwell —
// look at WIP limits or review capacity there first."
type CycleTimeInsight struct {
	HasData    bool          `json:"has_data"`
	Buckets    []BucketDwell `json:"buckets"`
	Bottleneck string        `json:"bottleneck,omitempty"`
}

type BucketDwell struct {
	FromBucket   string  `json:"from_bucket"`
	Samples      int     `json:"samples"`
	AvgDwellDays float64 `json:"avg_dwell_days"`
}

// WIPInsight — INSIGHT 3: work-in-progress per bucket.
//
// Source: tasks(bucket_id, state) counted where state='active'. No event
// history needed — this is the live board shape.
//
// Acceptance: one row per bucket_id that holds at least one active task,
// ordered by bucket_id.
//
// Agent action: "Bucket <id> holds <count> active tasks — if that exceeds
// your WIP limit, finish before starting."
type WIPInsight struct {
	HasData bool        `json:"has_data"`
	Buckets []BucketWIP `json:"buckets"`
}

type BucketWIP struct {
	BucketID int64 `json:"bucket_id"`
	Count    int   `json:"count"`
}

// GuardInsight — INSIGHT 4: guard-violation hotspots.
//
// Source: events(event_type='guard.violated'), grouped by
// json_extract(payload,'$.rule') and json_extract(payload,'$.tag'). Hits is
// the all-time count; Recent7d is the subset in the last 7 days (the trend
// signal).
//
// Acceptance: one row per (rule, tag) pair with at least one violation,
// ordered by hits desc.
//
// Agent action: "Rule <rule>/<tag> tripped <hits>x (<recent7d> this week) —
// satisfy that guard up front to stop re-tripping it."
type GuardInsight struct {
	HasData  bool           `json:"has_data"`
	Hotspots []GuardHotspot `json:"hotspots"`
}

type GuardHotspot struct {
	Rule     string `json:"rule"`
	Tag      string `json:"tag"`
	Hits     int    `json:"hits"`
	Recent7d int    `json:"recent_7d"`
}

// ErrorLoopInsight — INSIGHT 5: errors recorded vs resolved.
//
// Source: errors (total) joined to solutions(success=1) (resolved =
// distinct errors with at least one successful solution). Open = total -
// resolved.
//
// Acceptance: Total/Resolved/Open computed over the errors+solutions
// tables; Open never goes negative.
//
// Agent action: "<open> of <total> recorded errors are still unresolved —
// search top solutions before re-debugging a known failure."
type ErrorLoopInsight struct {
	HasData  bool `json:"has_data"`
	Total    int  `json:"total"`
	Resolved int  `json:"resolved"`
	Open     int  `json:"open"`
}

// MinModelSampleSize is the minimum number of stamped task events a model
// must contribute before its per-model contrast is treated as a confident
// reading. Below this threshold a row is flagged Partial (SampleSize < N) so
// both surfaces render "sample since <date>, N rows" rather than a confident
// average on a tiny n — a single-event model must NEVER look like a measured
// trend. Exposed as a documented const (rather than buried in the query) so a
// future config knob can override it without touching the SQL.
//
// Default 5: small enough that a model in genuine rotation crosses it within a
// handful of stamped transitions, large enough that an n=1/n=2 fluke can never
// masquerade as a real per-model signal.
const MinModelSampleSize = 5

// PerModelInsight — INSIGHT 6: per-agent-model contrast, partial-state gated
// (task 1353).
//
// Source: events filtered to agent_model<>” (via sqlutil.AgentAttributedFilter)
// — ONLY stamped rows; pre-stamp / blank-model events are excluded so they can
// never pollute a model bucket. AvgDwellDays is the same LAG-based dwell as
// insight 2 grouped by agent_model; GuardsPerTask is guard violations divided
// by the distinct tasks the model touched (a rate, comparable across models of
// different volume); GuardViolations is the raw count behind that rate.
//
// Each row carries SampleSize (the count of stamped task events feeding the
// model = the gate input) and FirstStampedAt (the model's earliest stamped
// event). A row with SampleSize < MinModelSampleSize sets Partial=true; a
// renderer MUST then show "sample since <FirstStampedAt>, <SampleSize> rows"
// instead of a confident average.
//
// Acceptance: one row per stamped agent_model that appears in events, never a
// silent/misleading zero for a thin model.
//
// Agent action: "Model <m> averages <avg>d dwell with <guards-per-task> guard
// hits/task — compare against peers; treat partial rows as not-yet-measured."
//
// DEFERRED (out of scope, task 1353): a model-score that fuses
// closes-fast-AND-no-rework needs a `rework` signal that is not yet defined in
// the event log. No score field is emitted here until that signal exists.
type PerModelInsight struct {
	HasData bool            `json:"has_data"`
	Models  []ModelContrast `json:"models"`
}

type ModelContrast struct {
	AgentModel      string  `json:"agent_model"`
	AvgDwellDays    float64 `json:"avg_dwell_days"`
	DwellSamples    int     `json:"dwell_samples"`
	GuardViolations int     `json:"guard_violations"`
	// GuardsPerTask is GuardViolations / distinct tasks the model touched
	// (0 when the model has touched no task), so a high-volume model is not
	// penalised purely for doing more work.
	GuardsPerTask float64 `json:"guards_per_task"`
	// SampleSize is the count of stamped task events attributed to this model
	// — the input to the partial-state gate (Partial = SampleSize < N).
	SampleSize int `json:"sample_size"`
	// FirstStampedAt is the model's earliest stamped event timestamp
	// (the "sample since" date a partial row surfaces). Empty only when the
	// model has no stamped event (which cannot happen for a roster row).
	FirstStampedAt string `json:"first_stamped_at"`
	// Partial is true when SampleSize < MinModelSampleSize: the row is below
	// the confidence gate and must render as partial-state, never a confident
	// average.
	Partial bool `json:"partial"`
}
