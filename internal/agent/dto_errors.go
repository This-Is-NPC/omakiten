package agent

import (
	"fmt"

	"omakiten/internal/domain"
)

type RecordErrorInput struct {
	ProjectSelector
	Description string   `json:"description"`
	Context     string   `json:"context,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type AddSolutionInput struct {
	ProjectSelector
	ErrorID     int64  `json:"error_id"`
	Description string `json:"description"`
	Steps       string `json:"steps,omitempty"`
	TaskID      int64  `json:"task_id,omitempty"`
}

type ConfirmSolutionInput struct {
	ProjectSelector
	SolutionID int64 `json:"solution_id"`
	Success    bool  `json:"success"`
}

type ListTopSolutionsInput struct {
	ProjectSelector
	Limit int `json:"limit,omitempty"`
}

type ErrorSummary struct {
	ID          int64             `json:"id"`
	Description string            `json:"description"`
	Context     string            `json:"context,omitempty"`
	ProjectID   int64             `json:"project_id,omitempty"`
	ProjectSlug string            `json:"project_slug,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
	Tags        []TagSummary      `json:"tags,omitempty"`
	Solutions   []SolutionSummary `json:"solutions,omitempty"`
}

type SolutionSummary struct {
	ID          int64  `json:"id"`
	ErrorID     int64  `json:"error_id"`
	Description string `json:"description"`
	Steps       string `json:"steps,omitempty"`
	Success     *bool  `json:"success,omitempty"`
	TaskID      *int64 `json:"task_id,omitempty"`
	TriedAt     string `json:"tried_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Likes       int    `json:"likes"`
	LikesBadge  string `json:"likes_badge,omitempty"`
	ProjectID   int64  `json:"project_id,omitempty"`
	ProjectSlug string `json:"project_slug,omitempty"`
}

type ErrorRecordResponse struct {
	Project ProjectSummary `json:"project"`
	Error   ErrorSummary   `json:"error"`
}

type SolutionResponse struct {
	Project  ProjectSummary  `json:"project"`
	Solution SolutionSummary `json:"solution"`
}

type TopSolutionsResponse struct {
	Project   ProjectSummary    `json:"project"`
	Solutions []SolutionSummary `json:"solutions"`
}

func errorSummary(record domain.ErrorRecord) ErrorSummary {
	out := ErrorSummary{
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
		out.Solutions = make([]SolutionSummary, len(record.Solutions))
		for i, sol := range record.Solutions {
			out.Solutions[i] = solutionSummary(sol)
		}
	}
	return out
}

func solutionSummary(s domain.Solution) SolutionSummary {
	out := SolutionSummary{
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
