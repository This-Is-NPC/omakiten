package contract

import (
	"context"

	"omakiten/internal/domain"
)

// Maintenance performs database-wide recovery work behind the operation facade.
type Maintenance interface {
	BackupDatabase(ctx context.Context, input DatabaseBackupInput) (DatabaseBackupResult, error)
	ReindexSearch(ctx context.Context, input SearchReindexInput) (SearchReindexResult, error)
	DeleteProject(ctx context.Context, input ProjectDeleteInput) (ProjectDeleteResult, error)
}

// DatabaseBackupInput selects the snapshot destination. An empty Out writes to
// the retained backup directory and prunes it; Out pins one file instead.
type DatabaseBackupInput struct {
	Out   string `json:"out,omitempty"`
	Force bool   `json:"force,omitempty"`
}

type DatabaseBackupResult struct {
	Path          string   `json:"path"`
	Pruned        bool     `json:"pruned"`
	Retention     *int     `json:"retention,omitempty"`
	PruneWarnings []string `json:"prune_warnings,omitempty"`
}

// SearchReindexInput without Confirm reports the repair plan and mutates nothing.
type SearchReindexInput struct {
	Confirm bool `json:"confirm"`
}

type SearchReindexResult struct {
	domain.SearchIndexReindexReport
	DatabasePath        string   `json:"database_path"`
	BackupPath          string   `json:"backup_path,omitempty"`
	PruneWarnings       []string `json:"prune_warnings,omitempty"`
	LeaseReleaseWarning string   `json:"lease_release_warning,omitempty"`
}
