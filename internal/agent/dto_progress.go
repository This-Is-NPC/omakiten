package agent

import "omakiten/internal/domain"

type RecordProgressInput struct {
	ProjectSelector
	TaskID       int64            `json:"task_id,omitempty"`
	Title        *string          `json:"title,omitempty"`
	Description  *string          `json:"description,omitempty"`
	Priority     *domain.Priority `json:"priority,omitempty"`
	MoveToBucket string           `json:"move_to_bucket,omitempty"`
	Comment      string           `json:"comment,omitempty"`
	Context      string           `json:"context,omitempty"`
	AuthorType   string           `json:"author_type,omitempty"`
}

type RecordProgressResponse struct {
	Project      ProjectSummary  `json:"project"`
	Task         *TaskSummary    `json:"task,omitempty"`
	Comment      *CommentSummary `json:"comment,omitempty"`
	ContextEntry *ContextSnippet `json:"context_entry,omitempty"`
}
