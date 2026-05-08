package domain

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
)

// TaskState toggles a task between the active workflow and the archived
// escape-hatch lane. Archive bypasses bucket-policy and transition guards but
// still respects the declared operations.archive.guards.
type TaskState string

const (
	TaskStateActive   TaskState = "active"
	TaskStateArchived TaskState = "archived"
)

type Task struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"project_id"`
	BucketID    int64     `json:"bucket_id,omitempty"`
	BucketKey   string    `json:"bucket_key,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Priority    Priority  `json:"priority"`
	State       TaskState `json:"state,omitempty"`
	CreatedAt   string    `json:"created_at,omitempty"`
}

// TaskSort drives the ORDER BY clause applied by ListTasks. Field is one of
// "id", "title", "priority", "created_at"; Order is "asc" or "desc". An empty
// Field falls back to id ascending — the legacy default.
type TaskSort struct {
	Field string
	Order string
}

type TaskFilter struct {
	BucketKey  string
	BucketKeys []string
	Priorities []Priority
	Sort       TaskSort
	// IncludeArchived flips the default active-only filter so callers can opt
	// into seeing archived tasks. Defaults to false: every list view (board,
	// table, graph, logs, MCP) hides archived rows unless the toggle is on.
	IncludeArchived bool
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

// TaskBlocker is a denormalized view of a single task this task depends on,
// used by the blockers_in workflow guard to report which blockers still sit
// in disallowed buckets. BucketKey is "" when the blocker has no active
// bucket (legacy data only).
type TaskBlocker struct {
	TaskID    int64
	Title     string
	BucketKey string
}
