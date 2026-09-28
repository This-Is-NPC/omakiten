package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// planSummary projects a domain.Plan into the delivery contract, keeping
// the goal body intact so the show / create responses can echo it back.
// List responses zero the field before sending to keep payloads compact.
func planSummary(plan domain.Plan) contract.PlanSummary {
	return contract.PlanSummary{
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
