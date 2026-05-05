package domain

type ErrorRecord struct {
	ID          int64      `json:"id"`
	Description string     `json:"description"`
	Context     string     `json:"context,omitempty"`
	ProjectID   int64      `json:"project_id,omitempty"`
	ProjectSlug string     `json:"project_slug,omitempty"`
	CreatedAt   string     `json:"created_at,omitempty"`
	Tags        []Tag      `json:"tags,omitempty"`
	Solutions   []Solution `json:"solutions,omitempty"`
}

type Solution struct {
	ID          int64  `json:"id"`
	ErrorID     int64  `json:"error_id"`
	Description string `json:"description"`
	Steps       string `json:"steps,omitempty"`
	Success     *bool  `json:"success,omitempty"`
	TaskID      *int64 `json:"task_id,omitempty"`
	TriedAt     string `json:"tried_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}
