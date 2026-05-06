package domain

// Event types stored in the unified `events` table. The activity feed
// (entity_type='task') and the logs view (event_type='operation') both
// read from the same source — discriminator columns keep them apart.
const (
	EventTypeComment       = "comment"
	EventTypeTaskCreated   = "task.created"
	EventTypeTaskMoved     = "task.moved"
	EventTypeTaskCompleted = "task.completed"
	EventTypeOperation     = "operation"
)

const (
	EventEntityTask   = "task"
	EventEntitySystem = "system"
)

// Event is the row shape of the unified events log. Different event_types
// populate different subsets of the fields — comments use Body/AuthorType,
// system events (task.*) use Payload, operations use Source/Operation/
// Status/DurationMs. Treat absent fields as empty.
type Event struct {
	ID           int64  `json:"id"`
	EntityType   string `json:"entity_type"`
	EntityID     int64  `json:"entity_id,omitempty"`
	ProjectID    int64  `json:"project_id,omitempty"`
	ProjectSlug  string `json:"project_slug,omitempty"`
	EventType    string `json:"event_type"`
	Body         string `json:"body,omitempty"`
	Payload      string `json:"payload,omitempty"`
	AuthorType   string `json:"author_type,omitempty"`
	Source       string `json:"source,omitempty"`
	Entrypoint   string `json:"entrypoint,omitempty"`
	Operation    string `json:"operation,omitempty"`
	Status       string `json:"status,omitempty"`
	DurationMs   int    `json:"duration_ms,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Tags         []Tag  `json:"tags,omitempty"`
}
