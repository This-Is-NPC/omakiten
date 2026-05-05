package domain

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

type Task struct {
	ID          int64    `json:"id"`
	ProjectID   int64    `json:"project_id"`
	BucketID    int64    `json:"bucket_id,omitempty"`
	BucketKey   string   `json:"bucket_key,omitempty"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    Priority `json:"priority"`
}

type TaskFilter struct {
	BucketKey string
}

type TaskUpdate struct {
	Title       *string
	Description *string
	Priority    *Priority
	BucketKey   string
}

type Comment struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"project_id"`
	TaskID     int64  `json:"task_id"`
	Body       string `json:"body"`
	AuthorType string `json:"author_type"`
	CreatedAt  string `json:"created_at,omitempty"`
	Tags       []Tag  `json:"tags,omitempty"`
}

type TaskDependency struct {
	ProjectID       int64 `json:"project_id"`
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}
