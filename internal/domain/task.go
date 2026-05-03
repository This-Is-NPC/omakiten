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
