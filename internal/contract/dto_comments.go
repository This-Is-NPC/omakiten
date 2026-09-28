package contract

type CommentSummary struct {
	ID         int64        `json:"id"`
	TaskID     int64        `json:"task_id"`
	Scope      string       `json:"scope,omitempty"`
	Body       string       `json:"body"`
	Title      string       `json:"title,omitempty"`
	Kind       string       `json:"kind,omitempty"`
	Pinned     bool         `json:"pinned,omitempty"`
	AuthorType string       `json:"author_type"`
	CreatedAt  string       `json:"created_at,omitempty"`
	UpdatedAt  string       `json:"updated_at,omitempty"`
	Tags       []TagSummary `json:"tags,omitempty"`
}

type AddCommentInput struct {
	ProjectSelector
	TaskID       int64    `json:"task_id,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	Body         string   `json:"body"`
	Title        string   `json:"title,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Pinned       bool     `json:"pinned,omitempty"`
	AuthorType   string   `json:"author_type,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	TemplateSlug string   `json:"template_slug,omitempty"`
}

type EditCommentInput struct {
	ProjectSelector
	CommentID int64 `json:"comment_id"`
	// Body/Title/Kind/Pinned are tri-state: an omitted JSON field decodes to nil
	// and leaves the stored column untouched; an explicit value overwrites it.
	// This keeps a metadata-only edit (pin/title/kind) from wiping the body, and
	// a body-only edit from wiping a comment's title, kind, or pin. A non-nil
	// body must be non-empty.
	Body   *string  `json:"body,omitempty"`
	Title  *string  `json:"title,omitempty"`
	Kind   *string  `json:"kind,omitempty"`
	Pinned *bool    `json:"pinned,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

type DeleteCommentInput struct {
	ProjectSelector
	CommentID int64 `json:"comment_id"`
	Confirmed bool  `json:"confirmed,omitempty"`
}

type DeleteCommentResponse struct {
	Project      ProjectSummary `json:"project"`
	Confirmation Confirmation   `json:"confirmation,omitempty"`
	Snapshot     *EventSummary  `json:"snapshot,omitempty"`
}

type ListCommentsInput struct {
	ProjectSelector
	TaskID    int64  `json:"task_id,omitempty"`
	CommentID int64  `json:"comment_id,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Pinned    bool   `json:"pinned,omitempty"`
	Query     string `json:"query,omitempty"`
	Since     string `json:"since,omitempty"`
}

type CommentsResponse struct {
	Project  ProjectSummary   `json:"project"`
	Comments []CommentSummary `json:"comments"`
}

type CommentResponse struct {
	Project ProjectSummary `json:"project"`
	Comment CommentSummary `json:"comment"`
}

// EventSummary is the agent-facing shape of a unified activity-feed entry.
// Comments use AuthorType + Body + Tags; system events use EventType + Payload.
type EventSummary struct {
	ID         int64        `json:"id"`
	EventType  string       `json:"event_type"`
	Body       string       `json:"body,omitempty"`
	Payload    string       `json:"payload,omitempty"`
	AuthorType string       `json:"author_type,omitempty"`
	CreatedAt  string       `json:"created_at"`
	Tags       []TagSummary `json:"tags,omitempty"`
}

type ListTaskActivityInput struct {
	ProjectSelector
	TaskID int64  `json:"task_id"`
	Order  string `json:"order,omitempty"`
}

type ListTaskActivityResponse struct {
	Project ProjectSummary `json:"project"`
	Events  []EventSummary `json:"events"`
	Order   string         `json:"order"`
}
