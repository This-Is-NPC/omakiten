package operation

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// RelocateProject repairs registration using an explicit project identity.
func (s *Service) RelocateProject(ctx context.Context, input contract.RelocateProjectInput) (contract.RelocateProjectResponse, error) {
	if err := s.allow("project.relocate"); err != nil {
		return contract.RelocateProjectResponse{}, err
	}
	if input.ProjectID <= 0 && strings.TrimSpace(input.Project) == "" {
		return contract.RelocateProjectResponse{}, domain.NewError(domain.ErrValidation, "project relocation requires an explicit project slug or id", nil)
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RelocateProjectResponse{}, err
	}
	updated, err := app.NewProjectService(s.repo, nil, nil).Relocate(ctx, project.ID, project.RootPath, input.RootPath)
	if err != nil {
		return contract.RelocateProjectResponse{}, err
	}
	return contract.RelocateProjectResponse{Project: projectSummary(updated.Context()), PreviousRootPath: project.RootPath}, nil
}
