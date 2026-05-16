package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"omakiten/internal/domain"
)

// isNilResolver returns true when the BucketResolver interface is nil
// OR when it wraps a typed-nil pointer (the common shape callers get
// when they assign a nil Snapshot pointer to a BucketResolver slot).
// Reflection here is fine — the orphan path runs once per workflow
// swap, not in a hot loop.
func isNilResolver(r domain.BucketResolver) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return true
	}
	return false
}

// PreviewOrphanedTasks reports active tasks whose bucket no longer
// belongs to the active workflow. The SQL `workflow_buckets` table was
// dropped in migration 020; the implementation now diffs the current
// in-memory resolver against the previous one (the caller hands the
// previous-snapshot view through `previous` whenever the cache holds a
// pre-rotation pointer) — task.bucket_id values that exist in the
// previous resolver but not in the current one are orphans. When the
// bucket key survives the swap (renamed id, same key), the task migrates
// to the new bucket id transparently elsewhere (`bucket_key` preserved)
// and is not surfaced here.
//
// Empty report when:
//   - no current workflow loaded (resolver nil or workflow has no buckets)
//   - no previous resolver (caller has only ever seen one bundle)
//   - every task's bucket_id resolves in the current resolver
func (s *Store) PreviewOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error) {
	if current == nil {
		return domain.OrphanReport{}, nil
	}
	wf := current.Workflow()
	if wf.Key == "" || len(wf.Buckets) == 0 {
		return domain.OrphanReport{}, nil
	}

	activeKeysByKey := map[string]struct{}{}
	for _, b := range wf.Buckets {
		activeKeysByKey[b.Key] = struct{}{}
	}
	defaultKey := wf.Buckets[0].Key

	taskRows, err := s.queryActiveTasksWithBucket(ctx, projectID)
	if err != nil {
		return domain.OrphanReport{}, err
	}

	report := domain.OrphanReport{WorkflowKey: wf.Key}
	groups := map[string]*domain.OrphanGroup{}
	for _, row := range taskRows {
		// Resolve the bucket key the task was last bound to via the
		// previous resolver: that view still knows the id↔key mapping
		// the bucket had when the task was created or last moved. When
		// no previous resolver exists (first-import path), fall back to
		// the current resolver — a task whose id resolves in the
		// current workflow is not orphaned regardless of swaps.
		fromKey := ""
		if !isNilResolver(previous) {
			if b, ok := previous.BucketByID(row.bucketID); ok {
				fromKey = b.Key
			}
		}
		if fromKey == "" {
			// No previous-resolver mapping. The task is orphaned only if
			// its bucket_id is also absent from the current workflow.
			if _, ok := current.BucketByID(row.bucketID); ok {
				continue
			}
		} else if _, ok := activeKeysByKey[fromKey]; ok {
			// Key survives across the swap — not user-facing orphan.
			continue
		}
		toKey := defaultKey
		groupKey := fromKey + "→" + toKey
		group, ok := groups[groupKey]
		if !ok {
			group = &domain.OrphanGroup{FromBucketKey: fromKey, ToBucketKey: toKey}
			groups[groupKey] = group
		}
		group.Tasks = append(group.Tasks, domain.OrphanedTask{
			TaskID:        row.taskID,
			Title:         row.title,
			FromBucketKey: fromKey,
			ToBucketKey:   toKey,
		})
		group.Count++
	}

	for _, g := range groups {
		report.Groups = append(report.Groups, *g)
		report.Total += g.Count
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].FromBucketKey != report.Groups[j].FromBucketKey {
			return report.Groups[i].FromBucketKey < report.Groups[j].FromBucketKey
		}
		return report.Groups[i].ToBucketKey < report.Groups[j].ToBucketKey
	})
	return report, nil
}

// queryActiveTasksWithBucket returns every active task row with its
// stored bucket_id. The orphan-classification logic runs in Go so the
// previous-resolver lookup stays close to the diff that drives it.
func (s *Store) queryActiveTasksWithBucket(ctx context.Context, projectID int64) ([]orphanRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.title, COALESCE(t.bucket_id, 0)
FROM tasks t
WHERE t.project_id = ? AND t.state = 'active'
ORDER BY t.id
`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []orphanRow
	for rows.Next() {
		var row orphanRow
		if err := rows.Scan(&row.taskID, &row.title, &row.bucketID); err != nil {
			return nil, err
		}
		if row.bucketID == 0 {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type orphanRow struct {
	taskID   int64
	title    string
	bucketID int64
}

// RebindOrphanedTasks applies the migration: every active task pointing
// at an orphaned bucket is rebound to the active workflow's matching
// key (when the key survives) or to the default (first) bucket. A
// task.migrated event is emitted per task inside the same transaction.
func (s *Store) RebindOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error) {
	report, err := s.PreviewOrphanedTasks(ctx, projectID, current, previous)
	if err != nil {
		return domain.OrphanReport{}, err
	}
	if report.Total == 0 {
		return report, nil
	}

	wf := current.Workflow()
	idByKey := make(map[string]int64, len(wf.Buckets))
	for _, b := range wf.Buckets {
		idByKey[b.Key] = b.ID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.OrphanReport{}, err
	}
	defer func() { _ = tx.Rollback() }()

	type emitted struct {
		event domain.Event
	}
	var events []emitted

	for _, group := range report.Groups {
		toID, ok := idByKey[group.ToBucketKey]
		if !ok {
			return domain.OrphanReport{}, fmt.Errorf("rebind orphaned tasks: target bucket %q not in active workflow", group.ToBucketKey)
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
