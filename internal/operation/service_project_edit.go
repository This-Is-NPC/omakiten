package operation

import (
	"context"
	"encoding/json"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// EditProject applies the set description and sync scope to the active
// project and, when a value actually changed, emits a project.updated
// audit event keyed by the project id so metrics.summary and the Logs
// inspector can attribute the edit to the calling agent. An edit that sets
// neither field is a validation error; an invalid sync scope is refused
// before anything is written. The refreshed project DTO is returned.
func (s *Service) EditProject(ctx context.Context, input contract.EditProjectInput) (contract.EditProjectResponse, error) {
	if err := s.allow("project.edit"); err != nil {
		return contract.EditProjectResponse{}, err
	}
	patch, err := projectPatch(input)
	if err != nil {
		return contract.EditProjectResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.EditProjectResponse{}, err
	}

	updated, err := s.repo.UpdateProject(ctx, project.ID, patch)
	if err != nil {
		return contract.EditProjectResponse{}, err
	}

	s.recordProjectUpdated(ctx, project, updated)

	return contract.EditProjectResponse{
		Project:        projectSummary(updated.Context()),
		Description:    updated.Description,
		SyncScope:      updated.SyncScope,
		NextStepPrompt: s.settings.NextStep.ProjectEdit,
	}, nil
}

func projectPatch(input contract.EditProjectInput) (domain.ProjectPatch, error) {
	patch := domain.ProjectPatch{Description: input.Description}
	if input.SyncScope != nil {
		scope, err := domain.ParseSyncScope(*input.SyncScope)
		if err != nil {
			return domain.ProjectPatch{}, err
		}
		patch.SyncScope = &scope
	}
	if patch.Description == nil && patch.SyncScope == nil {
		return domain.ProjectPatch{}, domain.NewError(domain.ErrValidation, "project edit needs description or sync_scope", nil)
	}
	return patch, nil
}

// recordProjectUpdated marshals the project.updated payload from the fields
// that changed and writes the audit row. The persist already committed, so an
// audit-emission failure is swallowed (the same post-commit best-effort
// contract the project.removed path follows) rather than failing the edit.
func (s *Service) recordProjectUpdated(ctx context.Context, before domain.ProjectContext, after domain.Project) {
	changes := map[string]any{}
	if before.Description != after.Description {
		changes["description"] = map[string]any{"from": before.Description, "to": after.Description}
	}
	if before.SyncScope != after.SyncScope {
		changes["sync_scope"] = map[string]any{"from": before.SyncScope, "to": after.SyncScope}
	}
	if len(changes) == 0 {
		return
	}
	payload, err := json.Marshal(changes)
	if err != nil {
		return
	}
	_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityProject, after.ID, after.ID, domain.EventTypeProjectUpdated, string(payload))
}
