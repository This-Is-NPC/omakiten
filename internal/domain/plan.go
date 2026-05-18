package domain

// PlanStatus tracks the high-level state of a plan. Plans start `active`
// when created, transition to `done` when the last child task closes, and
// `abandoned` when the user explicitly aborts the plan. The catalog mirrors
// the `status` CHECK constraint on the `plans` table (migration 023).
type PlanStatus string

const (
	PlanStatusActive    PlanStatus = "active"
	PlanStatusDone      PlanStatus = "done"
	PlanStatusAbandoned PlanStatus = "abandoned"
)

// Plan is a WBS-style implementation plan that groups tasks into ordered
// waves. A plan is scoped to a single project (cross-project plans are an
// explicit non-goal for v1); the (project_id, slug) pair is unique.
//
// GoalBody is human-authored markdown — used as the prose contract that
// agents read before claiming a task in this plan; the FTS5 search index
// gains a content type for this column in a follow-up migration.
type Plan struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"project_id"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	GoalBody    string     `json:"goal_body,omitempty"`
	Status      PlanStatus `json:"status"`
	CreatedAt   string     `json:"created_at,omitempty"`
	UpdatedAt   string     `json:"updated_at,omitempty"`
	CompletedAt string     `json:"completed_at,omitempty"`
}

// PlanWave is an ordered phase inside a plan. Tasks attached to the same
// wave run in parallel; tasks in wave N+1 are gated on every task in wave
// N reaching the workflow's final bucket — enforced by the wave_gate guard
// (registered in a later slice, not by this domain type).
type PlanWave struct {
	ID       int64  `json:"id"`
	PlanID   int64  `json:"plan_id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// PlanTaskRow is the projection ListPlanTasks returns: enough to render
// the network diagram and compute done / total counts, without dragging
// the full domain.Task (which the plan view never needs description,
// priority, or created_at for). WaveID may be 0 when the task carries a
// plan_id but its wave_id was cleared via a wave deletion (cannot happen
// today because wave deletion is not exposed; reserved for future use).
type PlanTaskRow struct {
	TaskID     int64     `json:"task_id"`
	WaveID     int64     `json:"wave_id"`
	Title      string    `json:"title"`
	BucketKey  string    `json:"bucket_key"`
	State      TaskState `json:"state"`
	AssignedTo string    `json:"assigned_to,omitempty"`
}
