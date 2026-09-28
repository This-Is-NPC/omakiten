package operation

import (
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

func commentSummary(comment domain.Comment) contract.CommentSummary {
	s := contract.CommentSummary{
		ID:         comment.ID,
		TaskID:     comment.TaskID,
		Scope:      comment.Scope,
		Body:       comment.Body,
		Title:      comment.Title,
		Kind:       comment.Kind,
		Pinned:     comment.Pinned,
		AuthorType: comment.AuthorType,
		CreatedAt:  comment.CreatedAt,
		UpdatedAt:  comment.UpdatedAt,
	}
	if len(comment.Tags) > 0 {
		s.Tags = tagSummaries(comment.Tags)
	}
	return s
}

func eventSummary(event domain.Event) contract.EventSummary {
	s := contract.EventSummary{
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

func eventSummaries(events []domain.Event) []contract.EventSummary {
	if len(events) == 0 {
		return nil
	}
	out := make([]contract.EventSummary, len(events))
	for i, ev := range events {
		out[i] = eventSummary(ev)
	}
	return out
}
