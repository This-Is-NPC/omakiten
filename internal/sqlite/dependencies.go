package sqlite

import (
	"context"

	"omakiten/internal/domain"
)

func (s *Store) AddTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) (domain.TaskDependency, error) {
	if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
		return domain.TaskDependency{}, err
	}
	if err := s.ensureTaskExists(ctx, projectID, dependsOnTaskID); err != nil {
		return domain.TaskDependency{}, err
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO task_dependencies(project_id, task_id, depends_on_task_id)
VALUES (?, ?, ?)
ON CONFLICT(project_id, task_id, depends_on_task_id) DO NOTHING
`, projectID, taskID, dependsOnTaskID); err != nil {
		return domain.TaskDependency{}, err
	}
	return domain.TaskDependency{ProjectID: projectID, TaskID: taskID, DependsOnTaskID: dependsOnTaskID}, nil
}

func (s *Store) RemoveTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM task_dependencies WHERE project_id = ? AND task_id = ? AND depends_on_task_id = ?", projectID, taskID, dependsOnTaskID)
	return err
}

func (s *Store) ListTaskDependencies(ctx context.Context, projectID, taskID int64) ([]domain.TaskDependency, error) {
	query := "SELECT project_id, task_id, depends_on_task_id FROM task_dependencies WHERE project_id = ?"
	args := []any{projectID}
	if taskID > 0 {
		if err := s.ensureTaskExists(ctx, projectID, taskID); err != nil {
			return nil, err
		}
		query += " AND task_id = ?"
		args = append(args, taskID)
	}
	query += " ORDER BY task_id, depends_on_task_id"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var dependencies []domain.TaskDependency
	for rows.Next() {
		var dependency domain.TaskDependency
		if err := rows.Scan(&dependency.ProjectID, &dependency.TaskID, &dependency.DependsOnTaskID); err != nil {
			return nil, err
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, rows.Err()
}
