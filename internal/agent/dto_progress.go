package agent

type RecordProgressInput struct {
	ProjectSelector
	TaskID      int64   `json:"task_id,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	// Priority is the configured priority label (e.g. "high"). nil leaves
	// the field unchanged. Resolved to a domain id via the bundle-scoped
	// registry inside RecordProgress so the wire boundary stays free of
	// domain handles.
	Priority     *string `json:"priority,omitempty"`
	MoveToBucket string  `json:"move_to_bucket,omitempty"`
	Comment      string  `json:"comment,omitempty"`
	AuthorType   string  `json:"author_type,omitempty"`
}

type RecordProgressResponse struct {
	Project ProjectSummary  `json:"project"`
	Task    *TaskSummary    `json:"task,omitempty"`
	Comment *CommentSummary `json:"comment,omitempty"`
}
