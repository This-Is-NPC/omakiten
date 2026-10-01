package operation

import (
	"context"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// SetMaintenance injects the database-wide recovery port. Composition roots
// set it on their surface copy; a Service without it refuses maintenance.
func (s *Service) SetMaintenance(maintenance contract.Maintenance) {
	s.maintenance = maintenance
}

func (s *Service) BackupDatabase(ctx context.Context, input contract.DatabaseBackupInput) (contract.DatabaseBackupResult, error) {
	if err := s.allow("db.backup"); err != nil {
		return contract.DatabaseBackupResult{}, err
	}
	if err := s.requireMaintenance(); err != nil {
		return contract.DatabaseBackupResult{}, err
	}
	return s.maintenance.BackupDatabase(ctx, input)
}

func (s *Service) ReindexSearch(ctx context.Context, input contract.SearchReindexInput) (contract.SearchReindexResult, error) {
	if err := s.allow("db.reindex"); err != nil {
		return contract.SearchReindexResult{}, err
	}
	if err := s.requireMaintenance(); err != nil {
		return contract.SearchReindexResult{}, err
	}
	return s.maintenance.ReindexSearch(ctx, input)
}

func (s *Service) DeleteProject(ctx context.Context, input contract.ProjectDeleteInput) (contract.ProjectDeleteResult, error) {
	if err := s.allow("project.delete"); err != nil {
		return contract.ProjectDeleteResult{}, err
	}
	if input.ProjectID <= 0 {
		return contract.ProjectDeleteResult{}, domain.NewError(domain.ErrValidation, "project id must be positive", map[string]any{"project_id": input.ProjectID})
	}
	if err := s.requireMaintenance(); err != nil {
		return contract.ProjectDeleteResult{}, err
	}
	return s.maintenance.DeleteProject(ctx, input)
}

// RemoveProject deletes the selected project once confirmed, after a
// backup of the database; unconfirmed it only reports what would go.
func (s *Service) RemoveProject(ctx context.Context, input contract.RemoveProjectInput) (contract.RemoveProjectResponse, error) {
	if err := s.allow("project.delete"); err != nil {
		return contract.RemoveProjectResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RemoveProjectResponse{}, err
	}
	counters, err := s.repo.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		return contract.RemoveProjectResponse{}, err
	}
	out := contract.RemoveProjectResponse{Project: projectSummary(project), Counters: counters}
	if !input.Confirmed {
		out.Confirmation = contract.Confirmation{
			RequiresConfirmation: true,
			Reason:               "Deleting a project removes its tasks, comments, plans, tags, errors, and activity for good; a database backup is written first. Confirm with confirmed=true to proceed.",
			Options:              []contract.ConfirmationOption{{Action: "confirm_delete", Label: "Retry projects.delete with confirmed=true to hard-delete"}},
		}
		return out, nil
	}
	result, err := s.DeleteProject(ctx, contract.ProjectDeleteInput{ProjectID: project.ID, Counters: counters})
	if err != nil {
		return contract.RemoveProjectResponse{}, err
	}
	out.Counters = result.Counters
	out.BackupPath = result.BackupPath
	out.PruneWarnings = result.PruneWarnings
	return out, nil
}

func (s *Service) requireMaintenance() error {
	if s.maintenance == nil {
		return domain.NewError(domain.ErrValidation, "database maintenance is unavailable", nil)
	}
	return nil
}
