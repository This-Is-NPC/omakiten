package sqlite

import (
	"context"
	"errors"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// CreateClonedProject inserts a synced project that carries the remote uid
// and the shared fields of info, rooted at root. Nothing is queued for push.
func (s *Store) CreateClonedProject(ctx context.Context, info domain.SyncProjectInfo, root string) (domain.Project, error) {
	if err := domain.ValidateSyncProjectInfo(info); err != nil {
		return domain.Project{}, err
	}
	project, err := s.scanProject(s.query(ctx).QueryRowContext(ctx, `
INSERT INTO projects(uid, name, slug, description, root_path, sync_scope)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING `+projectColumns, info.UID, info.Name, info.Slug, info.Description, root, domain.SyncScopeSynced))
	var constraint *sqlutil.ConstraintError
	if !errors.As(sqlutil.MapSQLiteError(err), &constraint) || constraint.Violation != sqlutil.ViolationUnique {
		return project, err
	}
	if constraint.Field == "root_path" {
		return domain.Project{}, domain.NewError(domain.ErrProjectPathConflict, "root_path is already registered to a project", map[string]any{"root_path": root})
	}
	return domain.Project{}, domain.NewError(domain.ErrProjectAmbiguous, "a project with this slug or uid already exists",
		map[string]any{"slug": info.Slug, "uid": string(info.UID)})
}

var _ interface {
	SyncedProjects(context.Context) ([]domain.SyncProject, error)
	PendingChanges(context.Context, int64, int) ([]domain.SyncChange, error)
	MarkPushed(context.Context, int64, string) error
	ApplyChanges(context.Context, int64, []domain.SyncChange, int64) (int, error)
	CreateClonedProject(context.Context, domain.SyncProjectInfo, string) (domain.Project, error)
	SyncRemoteURL(context.Context) (string, error)
	SetSyncRemoteURL(context.Context, string) error
} = (*Store)(nil)
