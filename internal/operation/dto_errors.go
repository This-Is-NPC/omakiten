package operation

import (
	"fmt"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func errorSummary(record domain.ErrorRecord) contract.ErrorSummary {
	out := contract.ErrorSummary{
		ID:          record.ID,
		Description: record.Description,
		Context:     record.Context,
		ProjectID:   record.ProjectID,
		ProjectSlug: record.ProjectSlug,
		CreatedAt:   record.CreatedAt,
	}
	if len(record.Tags) > 0 {
		out.Tags = tagSummaries(record.Tags)
	}
	if len(record.Solutions) > 0 {
		out.Solutions = make([]contract.SolutionSummary, len(record.Solutions))
		for i, sol := range record.Solutions {
			out.Solutions[i] = solutionSummary(sol)
		}
	}
	return out
}

func solutionSummary(s domain.Solution) contract.SolutionSummary {
	out := contract.SolutionSummary{
		ID:          s.ID,
		ErrorID:     s.ErrorID,
		Description: s.Description,
		Steps:       s.Steps,
		Success:     s.Success,
		TaskID:      s.TaskID,
		TriedAt:     s.TriedAt,
		CreatedAt:   s.CreatedAt,
		Likes:       s.Likes,
		ProjectID:   s.ProjectID,
		ProjectSlug: s.ProjectSlug,
	}
	if s.Likes > 0 {
		out.LikesBadge = solutionLikesBadge(s.Likes)
	}
	return out
}

func solutionLikesBadge(likes int) string {
	if likes <= 0 {
		return ""
	}
	return fmt.Sprintf("[★ %d]", likes)
}
