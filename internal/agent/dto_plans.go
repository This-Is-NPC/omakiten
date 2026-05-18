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
