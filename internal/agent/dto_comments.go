package agent

import "omakiten/internal/domain"

type CommentSummary struct {
	ID         int64        `json:"id"`
	TaskID     int64        `json:"task_id"`
	Body       string       `json:"body"`
	AuthorType string       `json:"author_type"`
	CreatedAt  string       `json:"created_at,omitempty"`
	Tags       []TagSummary `json:"tags,omitempty"`
}

type AddCommentInput struct {
	ProjectSelector
	TaskID       int64    `json:"task_id"`
	Body         string   `json:"body"`
	AuthorType   string   `json:"author_type,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	TemplateSlug string   `json:"template_slug,omitempty"`
}

type ListCommentsInput struct {
	ProjectSelector
	TaskID int64 `json:"task_id"`
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

func commentSummary(comment domain.Comment) CommentSummary {
	s := CommentSummary{ID: comment.ID, TaskID: comment.TaskID, Body: comment.Body, AuthorType: comment.AuthorType, CreatedAt: comment.CreatedAt}
	if len(comment.Tags) > 0 {
		s.Tags = tagSummaries(comment.Tags)
	}
	return s
}

func eventSummary(event domain.Event) EventSummary {
	s := EventSummary{
		ID:         event.ID,
		EventType:  event.EventType,
		Body:       event.Body,
		Payload:    event.Payload,
		AuthorType: event.AuthorType,
		CreatedAt:  event.CreatedAt,
	}
	if s.Payload == "{}" {
		s.Payload = ""
	}
	if len(event.Tags) > 0 {
		s.Tags = tagSummaries(event.Tags)
	}
	return s
}

func eventSummaries(events []domain.Event) []EventSummary {
	if len(events) == 0 {
		return nil
	}
	out := make([]EventSummary, len(events))
	for i, ev := range events {
		out[i] = eventSummary(ev)
	}
	return out
}
