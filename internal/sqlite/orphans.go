package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"omakiten/internal/domain"
)

// PreviewOrphanedTasks reports active tasks whose bucket no longer belongs to
// the active workflow (the bucket row exists but workflow_buckets.active = 0).
// Read-only: no rebind, no events. When there is no active workflow the report
// is empty and no error is returned — callers handle "nothing to do".
func (s *Store) PreviewOrphanedTasks(ctx context.Context, projectID int64) (domain.OrphanReport, error) {
	workflowKey, targets, defaultKey, err := s.activeWorkflowTargets(ctx)
	if err != nil {
		return domain.OrphanReport{}, err
	}
	if defaultKey == "" {
		return domain.OrphanReport{}, nil
	}
	orphans, err := s.queryOrphans(ctx, projectID)
	if err != nil {
		return domain.OrphanReport{}, err
	}
	return buildReport(workflowKey, orphans, targets, defaultKey), nil
}

// RebindOrphanedTasks applies the migration: every active task pointing at an
// inactive bucket is rebinded to the active workflow's matching key (when the
// key survives) or to the first active bucket (default). A task.migrated event
// is emitted per task inside the same transaction. Returns the report of what
// was applied. No-op when there are no orphans or no active workflow.
//
// Transition guards are bypassed — the trigger is a config change, not a
// workflow transition, and the source bucket no longer exists in the active
// workflow so transition rules cannot apply.
func (s *Store) RebindOrphanedTasks(ctx context.Context, projectID int64) (domain.OrphanReport, error) {
	report, err := s.PreviewOrphanedTasks(ctx, projectID)
	if err != nil {
		return domain.OrphanReport{}, err
	}
	if report.Total == 0 {
		return report, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.OrphanReport{}, err
	}
	defer func() { _ = tx.Rollback() }()

	targetIDs, err := loadActiveBucketIDsByKey(ctx, tx)
	if err != nil {
		return domain.OrphanReport{}, err
	}

	type emitted struct {
		event domain.Event
	}
	var events []emitted

	for _, group := range report.Groups {
		toID, ok := targetIDs[group.ToBucketKey]
		if !ok {
			return domain.OrphanReport{}, fmt.Errorf("rebind orphaned tasks: target bucket %q not active", group.ToBucketKey)
		}
		for _, task := range group.Tasks {
			if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET bucket_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE project_id = ? AND id = ?
`, toID, projectID, task.TaskID); err != nil {
				return domain.OrphanReport{}, err
			}

			payload := fmt.Sprintf(`{"from":%q,"to":%q,"reason":"workflow_swap"}`, task.FromBucketKey, task.ToBucketKey)
			var ev domain.Event
			if s.shouldLogEvent(domain.EventTypeTaskMigrated) {
				ev, err = insertTaskEvent(ctx, tx, projectID, task.TaskID, domain.EventTypeTaskMigrated, "", payload)
				if err != nil {
					return domain.OrphanReport{}, err
				}
			} else {
				ev = domain.Event{EntityType: domain.EventEntityTask, EntityID: task.TaskID, ProjectID: projectID, EventType: domain.EventTypeTaskMigrated, Payload: payload}
			}
			events = append(events, emitted{event: ev})
		}
	}

	if err := tx.Commit(); err != nil {
		return domain.OrphanReport{}, err
	}
	for _, e := range events {
		s.publishEvent(ctx, e.event)
	}
	return report, nil
}

// activeWorkflowTargets returns the active workflow's key, the set of active
// bucket keys, and the first/default bucket key (lowest position). When no
// active workflow exists, defaultKey is "" and callers should treat that as
// "no migration possible".
func (s *Store) activeWorkflowTargets(ctx context.Context) (workflowKey string, activeKeys map[string]struct{}, defaultKey string, err error) {
	row := s.db.QueryRowContext(ctx, `
SELECT workflows.key
FROM workflows
JOIN config_bundles ON config_bundles.id = workflows.bundle_id
JOIN settings ON settings.bundle_id = config_bundles.id
  AND settings.key = 'workflow.active'
  AND settings.value = workflows.key
  AND settings.active = 1
WHERE workflows.active = 1 AND config_bundles.active = 1
ORDER BY config_bundles.id DESC, workflows.id DESC
LIMIT 1
`)
	if err = row.Scan(&workflowKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, "", nil
		}
		return "", nil, "", err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT workflow_buckets.key
FROM workflow_buckets
JOIN workflows ON workflows.id = workflow_buckets.workflow_id
JOIN config_bundles ON config_bundles.id = workflows.bundle_id
JOIN settings ON settings.bundle_id = config_bundles.id
  AND settings.key = 'workflow.active'
  AND settings.value = workflows.key
  AND settings.active = 1
WHERE workflow_buckets.active = 1
  AND workflows.active = 1
  AND config_bundles.active = 1
ORDER BY workflow_buckets.position, workflow_buckets.id
`)
	if err != nil {
		return "", nil, "", err
	}
	defer func() { _ = rows.Close() }()

	activeKeys = map[string]struct{}{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return "", nil, "", err
		}
		if defaultKey == "" {
			defaultKey = key
		}
		activeKeys[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return "", nil, "", err
	}
	return workflowKey, activeKeys, defaultKey, nil
}

type orphanRow struct {
	taskID  int64
	title   string
	oldKey  string
}

func (s *Store) queryOrphans(ctx context.Context, projectID int64) ([]orphanRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.title, wb.key
FROM tasks t
JOIN workflow_buckets wb ON wb.id = t.bucket_id
WHERE t.project_id = ?
  AND t.state = 'active'
  AND wb.active = 0
ORDER BY t.id
`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []orphanRow
	for rows.Next() {
		var row orphanRow
		if err := rows.Scan(&row.taskID, &row.title, &row.oldKey); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func loadActiveBucketIDsByKey(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT workflow_buckets.key, workflow_buckets.id
FROM workflow_buckets
JOIN workflows ON workflows.id = workflow_buckets.workflow_id
JOIN config_bundles ON config_bundles.id = workflows.bundle_id
JOIN settings ON settings.bundle_id = config_bundles.id
  AND settings.key = 'workflow.active'
  AND settings.value = workflows.key
  AND settings.active = 1
WHERE workflow_buckets.active = 1
  AND workflows.active = 1
  AND config_bundles.active = 1
`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]int64{}
	for rows.Next() {
		var key string
		var id int64
		if err := rows.Scan(&key, &id); err != nil {
			return nil, err
		}
		out[key] = id
	}
	return out, rows.Err()
}

func buildReport(workflowKey string, orphans []orphanRow, targets map[string]struct{}, defaultKey string) domain.OrphanReport {
	if len(orphans) == 0 {
		return domain.OrphanReport{WorkflowKey: workflowKey}
	}

	byGroup := map[string]*domain.OrphanGroup{}
	for _, row := range orphans {
		target := defaultKey
		if _, ok := targets[row.oldKey]; ok {
			target = row.oldKey
		}
		key := row.oldKey + "→" + target
		group, ok := byGroup[key]
		if !ok {
			group = &domain.OrphanGroup{FromBucketKey: row.oldKey, ToBucketKey: target}
			byGroup[key] = group
		}
		group.Tasks = append(group.Tasks, domain.OrphanedTask{
			TaskID:        row.taskID,
			Title:         row.title,
			FromBucketKey: row.oldKey,
			ToBucketKey:   target,
		})
		group.Count++
	}

	groups := make([]domain.OrphanGroup, 0, len(byGroup))
	for _, g := range byGroup {
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].FromBucketKey != groups[j].FromBucketKey {
			return groups[i].FromBucketKey < groups[j].FromBucketKey
		}
		return groups[i].ToBucketKey < groups[j].ToBucketKey
	})

	total := 0
	for _, g := range groups {
		total += g.Count
	}
	return domain.OrphanReport{WorkflowKey: workflowKey, Groups: groups, Total: total}
}
