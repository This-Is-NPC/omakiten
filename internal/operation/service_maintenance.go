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

func (s *Service) requireMaintenance() error {
	if s.maintenance == nil {
		return domain.NewError(domain.ErrValidation, "database maintenance is unavailable", nil)
	}
	return nil
}
