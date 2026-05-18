package agent

import "omakiten/internal/domain"

// PlanSummary is the MCP wire shape for a plan row. GoalBody is omitted
// from list views (zero-value json:"omitempty") so dashboards stay
// compact; the show / get endpoints will fold it back in.
type PlanSummary struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	GoalBody    string `json:"goal_body,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type CreatePlanInput struct {
	ProjectSelector
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	GoalBody string `json:"goal_body,omitempty"`
}

type CreatePlanResponse struct {
	Project ProjectSummary `json:"project"`
	Plan    PlanSummary    `json:"plan"`
}

type ListPlansInput struct {
	ProjectSelector
}

type ListPlansResponse struct {
	Project ProjectSummary `json:"project"`
	Plans   []PlanSummary  `json:"plans"`
}

type ShowPlanInput struct {
	ProjectSelector
	Slug string `json:"slug"`
}

type ShowPlanResponse struct {
	Project      ProjectSummary  `json:"project"`
	Plan         PlanSummary     `json:"plan"`
	Waves        []PlanWaveView  `json:"waves"`
	DoneCount    int             `json:"done_count"`
	TotalCount   int             `json:"total_count"`
	Percent      int             `json:"percent"`
	ActiveWaveID int64           `json:"active_wave_id,omitempty"`
}

// PlanWaveView pairs a wave with its task list and per-wave counts.
// Tasks carry the bucket_key, state, and assignee shipped over the
// wire so the consumer can render them without a follow-up lookup.
type PlanWaveView struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	Position   int           `json:"position"`
	Tasks      []PlanTaskRow `json:"tasks,omitempty"`
	DoneCount  int           `json:"done_count"`
	TotalCount int           `json:"total_count"`
}

type PlanTaskRow struct {
	TaskID     int64  `json:"task_id"`
	Title      string `json:"title"`
	BucketKey  string `json:"bucket_key,omitempty"`
	State      string `json:"state,omitempty"`
	AssignedTo string `json:"assigned_to,omitempty"`
}

type AddPlanWaveInput struct {
	ProjectSelector
	PlanID   int64  `json:"plan_id"`
	Slug     string `json:"slug,omitempty"`
	Name     string `json:"name"`
	Position int    `json:"position,omitempty"`
}

type AddPlanWaveResponse struct {
	Project ProjectSummary `json:"project"`
	Wave    PlanWaveSummary `json:"wave"`
}

type PlanWaveSummary struct {
	ID       int64  `json:"id"`
	PlanID   int64  `json:"plan_id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

type AssignPlanTaskInput struct {
	ProjectSelector
	TaskID int64  `json:"task_id"`
	PlanID int64  `json:"plan_id,omitempty"`
	Slug   string `json:"slug,omitempty"`
	WaveID int64  `json:"wave_id"`
}

type AssignPlanTaskResponse struct {
	Project ProjectSummary `json:"project"`
	TaskID  int64          `json:"task_id"`
	PlanID  int64          `json:"plan_id"`
	WaveID  int64          `json:"wave_id"`
}

type ClaimNextPlanTaskInput struct {
	ProjectSelector
	PlanID int64  `json:"plan_id,omitempty"`
	Slug   string `json:"slug,omitempty"`
}

// ClaimNextPlanTaskResponse: Claimed is false when no task was available
// (every wave fully done OR active wave has no first-bucket tasks left).
// When Claimed=true the Task field carries the post-claim row (bucket
// already moved into the destination, assigned_to set to the agent).
type ClaimNextPlanTaskResponse struct {
	Project ProjectSummary `json:"project"`
	Claimed bool           `json:"claimed"`
	Task    *TaskSummary   `json:"task,omitempty"`
}

// planSummary projects a domain.Plan into the MCP wire shape, keeping
// the goal body intact so the show / create responses can echo it back.
// List responses zero the field before sending to keep payloads compact.
func planSummary(plan domain.Plan) PlanSummary {
	return PlanSummary{
		ID:          plan.ID,
		Slug:        plan.Slug,
		Name:        plan.Name,
		GoalBody:    plan.GoalBody,
		Status:      string(plan.Status),
		CreatedAt:   plan.CreatedAt,
		UpdatedAt:   plan.UpdatedAt,
		CompletedAt: plan.CompletedAt,
	}
}
