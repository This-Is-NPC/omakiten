package tui

import (
	"sort"

	"omakiten/internal/domain"
)

// commentToEvent projects a comment into the shared event-card shape used by
// the project activity surface.
func commentToEvent(comment domain.Comment) domain.Event {
	scope := comment.Scope
	if scope == "" {
		scope = domain.CommentScopeTask
	}
	entityID := comment.TaskID
	switch scope {
	case domain.CommentScopeProject:
		entityID = comment.ProjectID
	case domain.CommentScopeUniversal:
		entityID = 0
	}
	return domain.Event{
		ID: comment.ID, EntityType: scope, EntityID: entityID, ProjectID: comment.ProjectID,
		EventType: domain.EventTypeComment, Body: comment.Body, AuthorType: comment.AuthorType,
		CreatedAt: comment.CreatedAt, Tags: comment.Tags}
}

func (m Model) commentsForProjectScope(filter domain.CommentFilter) ([]domain.Event, error) {
	if m.repos.Comments == nil {
		return nil, nil
	}
	svc := m.repos.operationService()
	if svc == nil {
		return nil, nil
	}
	merged := make([]domain.Comment, 0)
	if filter.Scope != domain.CommentScopeUniversal {
		projectFilter := filter
		projectFilter.ProjectID = m.project.ID
		if projectFilter.Scope == "" {
			projectFilter.Scope = domain.CommentScopeProject
		}
		comments, err := svc.QueryComments(m.ctx, m.project, projectFilter)
		if err != nil {
			return nil, err
		}
		merged = append(merged, comments...)
	}
	if filter.Scope != domain.CommentScopeProject {
		universalFilter := filter
		universalFilter.ProjectID = 0
		universalFilter.Scope = domain.CommentScopeUniversal
		comments, err := svc.QueryComments(m.ctx, m.project, universalFilter)
		if err != nil {
			return nil, err
		}
		merged = append(merged, comments...)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Pinned && !merged[j].Pinned })
	out := make([]domain.Event, len(merged))
	for i, comment := range merged {
		out[i] = commentToEvent(comment)
	}
	return out, nil
}

func eventToComment(event domain.Event) domain.Comment {
	comment := domain.Comment{
		ID: event.ID, ProjectID: event.ProjectID, Scope: event.EntityType, Body: event.Body,
		AuthorType: event.AuthorType, CreatedAt: event.CreatedAt, Tags: event.Tags}
	if event.EntityType == "" || event.EntityType == domain.EventEntityTask {
		comment.Scope = domain.CommentScopeTask
		comment.TaskID = event.EntityID
	}
	return comment
}
