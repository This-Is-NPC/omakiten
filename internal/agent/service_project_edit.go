package agent

import (
	"context"
	"encoding/json"

	"omakiten/internal/domain"
)

// EditProject restores the write path for a project's description. It
// resolves the active project (mirroring Overview / ResumeProject),
// persists the new description through the store's
// UpdateProjectDescription method, and — when the value actually
// changed — emits a project.updated audit event keyed by the project
// id so metrics.summary and the Logs inspector can attribute the edit
// to the calling agent. The refreshed project DTO is returned.
func (s *Service) EditProject(ctx context.Context, input EditProjectInput) (EditProjectResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return EditProjectResponse{}, err
	}

	before := project.Description

	updated, err := s.repo.UpdateProjectDescription(ctx, project.ID, input.Description)
	if err != nil {
		return EditProjectResponse{}, err
	}

	if before != updated.Description {
		s.recordProjectUpdated(ctx, updated, before)
	}

	return EditProjectResponse{
		Project:        projectSummary(updated.Context()),
		Description:    updated.Description,
		NextStepPrompt: "Project description updated. Ask for the overview, resume the project, or continue a task.",
	}, nil
}

// recordProjectUpdated marshals the project.updated payload and writes
// the audit row. The persist already committed, so an audit-emission
// failure is swallowed (the same post-commit best-effort contract the
// project.removed path follows) rather than failing the edit.
func (s *Service) recordProjectUpdated(ctx context.Context, project domain.Project, fromDescription string) {
	payload, err := json.Marshal(map[string]any{
		"description": map[string]any{
			"from": fromDescription,
			"to":   project.Description,
		},
	})
	if err != nil {
		return
	}
	_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityProject, project.ID, project.ID, domain.EventTypeProjectUpdated, string(payload))
}
