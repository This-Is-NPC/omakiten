package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// UpdateProjectRootPath changes only the root and records its delta atomically.
func (s *Store) UpdateProjectRootPath(ctx context.Context, id int64, previousRoot, root string) (domain.Project, error) {
	if previousRoot == root {
		project, err := s.FindProjectByID(ctx, id)
		if err == nil && project.RootPath != previousRoot {
			return domain.Project{}, projectPathConflict(id)
		}
		return project, err
	}
	return txMutateAndEmit(ctx, s, TxMutation[domain.Project]{
		Scope: EventScopeEntity, EntityType: domain.EventEntityProject,
		EventType: domain.EventTypeProjectUpdated, ProjectID: id,
		EntityID: func(project domain.Project) int64 { return project.ID },
		Mutate: func(ctx context.Context, tx *sql.Tx) (domain.Project, error) {
			return s.updateProjectRootPath(ctx, tx, id, previousRoot, root)
		},
		Payload: func(domain.Project) (string, error) {
			body, err := json.Marshal(map[string]any{"root_path": map[string]string{"from": previousRoot, "to": root}})
			return string(body), err
		},
	})
}

func (s *Store) updateProjectRootPath(ctx context.Context, tx *sql.Tx, id int64, previousRoot, root string) (domain.Project, error) {
	project, err := s.scanProject(tx.QueryRowContext(ctx, `
UPDATE projects SET root_path = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND root_path = ? AND archived_at IS NULL
RETURNING id, name, slug, root_path, description`, root, id, previousRoot))
	var constraint *sqlutil.ConstraintError
	if errors.As(sqlutil.MapSQLiteError(err), &constraint) && constraint.Violation == sqlutil.ViolationUnique {
		return domain.Project{}, domain.NewError(domain.ErrProjectPathConflict, "root_path is already registered to a project", map[string]any{"root_path": root})
	}
	var coded *domain.CodedError
	if errors.As(err, &coded) && coded.Code == domain.ErrProjectNotFound {
		var count int
		if lookupErr := tx.QueryRowContext(ctx, "SELECT count(*) FROM projects WHERE id = ? AND archived_at IS NULL", id).Scan(&count); lookupErr != nil {
			return domain.Project{}, lookupErr
		}
		if count > 0 {
			return domain.Project{}, projectPathConflict(id)
		}
	}
	return project, err
}

func projectPathConflict(id int64) error {
	return domain.NewError(domain.ErrProjectPathConflict, "project root changed concurrently; read the project and retry", map[string]any{"project_id": id})
}
