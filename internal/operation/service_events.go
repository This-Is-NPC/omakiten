package operation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// EmitEvent writes external.<name> for an outside caller: a CI job, a
// script, another app. The kit must declare the event under the external
// category; its fields are checked against the external caps, and a
// project takes at most domain.MaxExternalEventsPerMinute of them a
// minute. The hooks declared on the event run as for any other event.
func (s *Service) EmitEvent(ctx context.Context, input contract.EmitEventInput) (contract.EmitEventResponse, error) {
	if err := s.allow("event.emit"); err != nil {
		return contract.EmitEventResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.EmitEventResponse{}, err
	}
	if err := domain.ValidateExternalName(input.Name); err != nil {
		return contract.EmitEventResponse{}, err
	}
	eventType := domain.EventTypeExternalPrefix + input.Name
	if s.Snapshot().EventRegistry().CategoryOf(eventType) != domain.EventCategoryExternal {
		return contract.EmitEventResponse{}, domain.NewError(domain.ErrValidation,
			fmt.Sprintf("%s is not declared: add it under events.definitions with category %q", eventType, domain.EventCategoryExternal),
			map[string]any{"field": "name", "value": input.Name})
	}
	if err := domain.ValidateExternalPayload(input.Payload); err != nil {
		return contract.EmitEventResponse{}, err
	}
	recent, err := s.repo.ListEvents(ctx, domain.EventFilter{
		ProjectID:  project.ID,
		Categories: []domain.EventCategory{domain.EventCategoryExternal},
		Since:      s.nowFunc()().Add(-time.Minute),
		Limit:      domain.MaxExternalEventsPerMinute,
	})
	if err != nil {
		return contract.EmitEventResponse{}, err
	}
	if len(recent) >= domain.MaxExternalEventsPerMinute {
		return contract.EmitEventResponse{}, domain.NewError(domain.ErrRateLimited,
			fmt.Sprintf("project %s took %d external events in the last minute; try again later", project.Slug, domain.MaxExternalEventsPerMinute),
			map[string]any{"field": "name", "max_per_minute": domain.MaxExternalEventsPerMinute})
	}
	payload := input.Payload
	if payload == nil {
		payload = map[string]string{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return contract.EmitEventResponse{}, err
	}
	if err := s.repo.RecordEntityEvent(ctx, domain.EventEntityProject, project.ID, project.ID, eventType, string(body)); err != nil {
		return contract.EmitEventResponse{}, err
	}
	return contract.EmitEventResponse{
		Project:        projectSummary(project),
		EventType:      eventType,
		NextStepPrompt: "Event recorded; the hooks declared on it run now. Read it back with logs.list.",
	}, nil
}
