package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"omakiten/internal/domain"
)

// SaveWorkMetadata persists producer extensions with cascading entity ownership.
func (s *Store) SaveWorkMetadata(ctx context.Context, kind string, id int64, data []byte) error {
	column := "task_id"
	if kind == "plan" {
		column = "plan_id"
	}
	_, err := s.query(ctx).ExecContext(ctx, "INSERT INTO document_metadata("+column+", metadata) VALUES (?, ?) ON CONFLICT("+column+") DO UPDATE SET metadata=excluded.metadata", id, string(data))
	return err
}

func (s *Store) readWorkMetadata(ctx context.Context, kind string, id int64) ([]byte, error) {
	column := "task_id"
	if kind == "plan" {
		column = "plan_id"
	}
	var data string
	err := s.query(ctx).QueryRowContext(ctx, "SELECT metadata FROM document_metadata WHERE "+column+" = ?", id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return []byte(data), err
}

// ReadWorkRecord hydrates complete tasks and relationships in the caller's snapshot.
func (s *Store) ReadWorkRecord(ctx context.Context, projectID int64, slug string, taskID int64, buckets domain.BucketResolver) (domain.WorkRecord, error) {
	var record domain.WorkRecord
	var err error
	query := "WITH RECURSIVE members(id) AS (SELECT id FROM tasks WHERE project_id = ? AND plan_id = ? UNION SELECT tasks.id FROM tasks JOIN members ON tasks.parent_id = members.id) SELECT id, COALESCE(wave_id, 0), COALESCE(plan_id, 0), COALESCE(assigned_to, '') FROM tasks WHERE id IN (SELECT id FROM members) ORDER BY depth, id"
	var ownerID int64
	if slug != "" {
		plan, e := s.GetPlanBySlug(ctx, projectID, slug)
		if e != nil {
			return record, e
		}
		record.Plan, ownerID = &plan, plan.ID
		record.Waves, err = s.ListPlanWaves(ctx, projectID, plan.ID)
		if err != nil {
			return record, err
		}
		record.Metadata, err = s.readWorkMetadata(ctx, "plan", plan.ID)
	} else {
		if _, e := s.GetTaskByID(ctx, projectID, taskID, buckets); e != nil {
			return record, e
		}
		ownerID = taskID
		query = "WITH RECURSIVE members(id) AS (SELECT ? UNION ALL SELECT tasks.id FROM tasks JOIN members ON tasks.parent_id=members.id WHERE tasks.project_id=?) SELECT id, 0, COALESCE(plan_id, 0), COALESCE(assigned_to, '') FROM tasks WHERE id IN (SELECT id FROM members) ORDER BY depth, id"
	}
	if err != nil {
		return record, err
	}
	args := []any{projectID, ownerID}
	if slug == "" {
		args = []any{ownerID, projectID}
	}
	record.Tasks, err = s.readWorkTaskRows(ctx, query, args)
	if err != nil {
		return record, err
	}
	for i := range record.Tasks {
		if err := s.hydrateWorkTask(ctx, projectID, &record.Tasks[i], buckets); err != nil {
			return record, err
		}
	}
	record.Dependencies, err = s.ListTaskDependencies(ctx, projectID, 0)
	return record, err
}

func (s *Store) hydrateWorkTask(ctx context.Context, projectID int64, row *domain.WorkTaskRecord, buckets domain.BucketResolver) error {
	var err error
	row.Task, err = s.GetTaskByID(ctx, projectID, row.Task.ID, buckets)
	if err != nil {
		return err
	}
	row.Metadata, err = s.readWorkMetadata(ctx, "task", row.Task.ID)
	if err != nil {
		return err
	}
	tags, err := s.ListTaskTags(ctx, projectID, row.Task.ID)
	if err != nil {
		return err
	}
	for _, tag := range tags {
		row.Tags = append(row.Tags, tag.Name)
	}
	return nil
}

func (s *Store) readWorkTaskRows(ctx context.Context, query string, args []any) ([]domain.WorkTaskRecord, error) {
	var tasks []domain.WorkTaskRecord
	rows, err := s.query(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var row domain.WorkTaskRecord
		if err := rows.Scan(&row.Task.ID, &row.WaveID, &row.PlanID, &row.Assignee); err != nil {
			_ = rows.Close()
			return nil, err
		}
		tasks = append(tasks, row)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	return tasks, nil
}
