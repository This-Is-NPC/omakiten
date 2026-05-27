package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"

	"omakiten/internal/domain"
)

// orphanDepthLimit caps the recursive CTE that derives task.parent_id
// chains. Real-world hierarchies are well under 64 (sub-tasks usually
// stack one or two deep); the cap exists so a pathological chain or a
// cycle escapes the query in bounded time. The earlier value (1024)
// was over-generous and silently truncated to `depth=0` via the LEFT
// JOIN's COALESCE — review finding §B.2 of #297.
const orphanDepthLimit = 64

// orphanScopedQuery returns the pre-built SQL for the given scope. The
// previous implementation concatenated a `scopeFilter` string into the
// query at call time; pre-building keeps the SQL plan stable and
// removes the Replace Primitive with Object smell flagged by review
// opportunity §D.16 of #297. scopeRootTasks does not use this — it has
// its own non-recursive query in queryActiveRootTasks.
func orphanScopedQuery(scope orphanScope) (string, bool) {
	q, ok := orphanScopedQueries[scope]
	return q, ok
}

var orphanScopedQueries = func() map[orphanScope]string {
	template := `
WITH RECURSIVE depths(id, parent_id, depth) AS (
    SELECT id, parent_id, 0 FROM tasks
        WHERE project_id = ? AND parent_id IS NULL
    UNION ALL
    SELECT t.id, t.parent_id, d.depth + 1 FROM tasks t
        INNER JOIN depths d ON t.parent_id = d.id
        WHERE t.project_id = ? AND d.depth < %d
)
SELECT t.id, t.title, COALESCE(t.bucket_id, 0), t.parent_id, COALESCE(d.depth, 0)
FROM tasks t
LEFT JOIN depths d ON d.id = t.id
WHERE t.project_id = ? AND t.state = 'active' %s
ORDER BY t.id
`
	return map[orphanScope]string{
		scopeAllTasks: fmt.Sprintf(template, orphanDepthLimit, ""),
		scopeSubTasks: fmt.Sprintf(template, orphanDepthLimit, "AND t.parent_id IS NOT NULL"),
	}
}()

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

// orphanScope selects which subset of active tasks the orphan helpers
// inspect. The zero value covers every active task — the pre-#281
// behaviour preserved for projects without subtask_kit.
type orphanScope uint8

const (
	scopeAllTasks orphanScope = iota
	scopeRootTasks
	scopeSubTasks
)

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
	return s.previewOrphansScoped(ctx, projectID, current, previous, scopeAllTasks)
}

// previewOrphansScoped is the shared implementation behind
// PreviewOrphanedTasks plus the depth-scoped helpers introduced by the
// sub-task kit cascade (#285). Callers select the row filter via scope:
// scopeAllTasks keeps the pre-#281 behaviour for projects without a
// sub-task kit; scopeRootTasks restricts to tasks.parent_id IS NULL;
// scopeSubTasks restricts to tasks.parent_id IS NOT NULL.
func (s *Store) previewOrphansScoped(ctx context.Context, projectID int64, current, previous domain.BucketResolver, scope orphanScope) (domain.OrphanReport, error) {
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

	taskRows, err := s.queryActiveTasksScoped(ctx, projectID, scope)
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
			ParentID:      row.parentID,
			Depth:         row.depth,
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

// queryActiveTasksScoped returns every active task row visible to the
// orphan scope. The root-only scope reads through a plain SELECT
// (depth is always 0 for parent_id IS NULL); the all-tasks and
// sub-task scopes pay for the recursive ancestor walk so the
// sub-task path emits the correct depth for grandchildren and below.
func (s *Store) queryActiveTasksScoped(ctx context.Context, projectID int64, scope orphanScope) ([]orphanRow, error) {
	if scope == scopeRootTasks {
		return s.queryActiveRootTasks(ctx, projectID)
	}
	query, ok := orphanScopedQuery(scope)
	if !ok {
		return nil, fmt.Errorf("orphan scope %d has no query template", scope)
	}
	rows, err := s.db.QueryContext(ctx, query, projectID, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	scanned, err := scanOrphanRows(rows)
	if err != nil {
		return nil, err
	}
	// Truncation marker: a row with parent_id != nil but depth == 0
	// only happens when the CTE excluded it (depth chain longer than
	// orphanDepthLimit), so LEFT JOIN -> NULL -> COALESCE -> 0. Surface
	// via slog so audit consumers see the gap; do not fail the
	// migration because the rebind itself uses bucket_id and is still
	// correct — only the depth payload is unreliable for the affected
	// rows.
	var truncated int
	for _, row := range scanned {
		if row.parentID != nil && row.depth == 0 {
			truncated++
		}
	}
	if truncated > 0 {
		slog.Warn("orphan depth CTE truncated; deeper rows report depth=0",
			"project_id", projectID,
			"scope", scope,
			"truncated_rows", truncated,
			"depth_cap", orphanDepthLimit,
		)
	}
	return scanned, nil
}

// queryActiveRootTasks is the simple non-recursive path used when the
// orphan scope only needs depth=0 rows. Skipping the recursive CTE on
// the root-tasks path avoids walking the full sub-task tree once per
// migration.
func (s *Store) queryActiveRootTasks(ctx context.Context, projectID int64) ([]orphanRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id, t.title, COALESCE(t.bucket_id, 0), t.parent_id, 0 AS depth
FROM tasks t
WHERE t.project_id = ? AND t.state = 'active' AND t.parent_id IS NULL
ORDER BY t.id
`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanOrphanRows(rows)
}

func scanOrphanRows(rows *sql.Rows) ([]orphanRow, error) {
	var out []orphanRow
	for rows.Next() {
		var (
			row      orphanRow
			parentFK sql.NullInt64
		)
		if err := rows.Scan(&row.taskID, &row.title, &row.bucketID, &parentFK, &row.depth); err != nil {
			return nil, err
		}
		if parentFK.Valid {
			pid := parentFK.Int64
			row.parentID = &pid
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
	parentID *int64
	depth    int
}

// RebindOrphanedTasks applies the migration: every active task pointing
// at an orphaned bucket is rebound to the active workflow's matching
// key (when the key survives) or to the default (first) bucket. A
// task.migrated event is emitted per task inside the same transaction.
func (s *Store) RebindOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error) {
	return s.rebindOrphansScoped(ctx, projectID, current, previous, scopeAllTasks, domain.EventTypeTaskMigrated, orphanEventContext{})
}

// RebindOrphanedRootTasks is the root-only variant invoked when a
// project has a sub-task kit configured: the legacy "all tasks"
// migration path would otherwise pull sub-tasks through the root kit's
// workflow even though their resolved kit is the sub-kit. Emits
// task.migrated per affected root task; sub-tasks travel through
// RebindOrphanedSubtasks instead.
func (s *Store) RebindOrphanedRootTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error) {
	return s.rebindOrphansScoped(ctx, projectID, current, previous, scopeRootTasks, domain.EventTypeTaskMigrated, orphanEventContext{})
}

// RebindOrphanedSubtasks emits the dedicated task.bucket_orphaned event
// for every sub-task whose bucket key is absent from the incoming
// sub-kit's workflow. The payload follows the lock from umbrella #281
// (task_id, parent_id, depth, old_bucket, from_kit, to_kit,
// resolved_kit, reason=bucket_missing_in_resolved_kit). Root tasks are
// not visible to this method — the caller pairs it with
// RebindOrphanedRootTasks for the root tree.
func (s *Store) RebindOrphanedSubtasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver, fromKit, toKit string) (domain.OrphanReport, error) {
	return s.rebindOrphansScoped(ctx, projectID, current, previous, scopeSubTasks, domain.EventTypeTaskBucketOrphaned, orphanEventContext{FromKit: fromKit, ToKit: toKit})
}

// orphanEventContext carries the kit-identity strings the sub-task path
// needs in its event payload. The legacy task.migrated path leaves both
// fields empty — its payload predates the resolved-kit metadata.
type orphanEventContext struct {
	FromKit string
	ToKit   string
}

// rebindOrphansScoped is the shared rebind dispatcher. When the event
// type is disabled in the store's log filter (`shouldLogEvent(eventType)`
// returns false), the slice appended to `events` carries bare
// `domain.Event{}` rows: no `ID`, no `CreatedAt`, no autoincrement-derived
// fields — only the entity/project/type/payload that the caller supplied.
// Consumers of the returned slice that only emit downstream hooks or copy
// the payload tolerate this; consumers that depend on the audit-log row
// id (e.g. correlated event IDs) must check `shouldLogEvent` themselves
// before treating the slice as audit-quality data. Documented per
// review finding §C.11 of #297.
func (s *Store) rebindOrphansScoped(ctx context.Context, projectID int64, current, previous domain.BucketResolver, scope orphanScope, eventType string, evCtx orphanEventContext) (domain.OrphanReport, error) {
	report, err := s.previewOrphansScoped(ctx, projectID, current, previous, scope)
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

			payload := buildOrphanPayload(eventType, task, evCtx)
			var ev domain.Event
			if s.shouldLogEvent(eventType) {
				ev, err = insertTaskEvent(ctx, tx, projectID, task.TaskID, eventType, "", payload)
				if err != nil {
					return domain.OrphanReport{}, err
				}
			} else {
				ev = domain.Event{EntityType: domain.EventEntityTask, EntityID: task.TaskID, ProjectID: projectID, EventType: eventType, Payload: payload}
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

// orphanBucketPayload is the locked sub-task kit cascade payload —
// task_id, parent_id (nullable), depth, old_bucket, from_kit, to_kit,
// resolved_kit, reason — emitted as task.bucket_orphaned per affected
// sub-task. Marshalled via encoding/json so kit / bucket keys with
// special characters (line separators, control bytes) cannot produce
// malformed payloads.
type orphanBucketPayload struct {
	TaskID      int64  `json:"task_id"`
	ParentID    *int64 `json:"parent_id"`
	Depth       int    `json:"depth"`
	OldBucket   string `json:"old_bucket"`
	FromKit     string `json:"from_kit"`
	ToKit       string `json:"to_kit"`
	ResolvedKit string `json:"resolved_kit"`
	Reason      string `json:"reason"`
}

// taskMigratedPayload is the legacy task.migrated payload — kept on
// its own typed struct so future schema changes round-trip safely
// through encoding/json.
type taskMigratedPayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// buildOrphanPayload composes the per-event JSON payload via
// encoding/json so kit / bucket strings with exotic characters
// (U+2028/U+2029 line separators, NULs, control bytes) round-trip as
// valid JSON. task.migrated keeps the legacy {from, to, reason} shape
// so existing audit consumers continue to match; task.bucket_orphaned
// carries the locked sub-task kit cascade payload.
func buildOrphanPayload(eventType string, task domain.OrphanedTask, evCtx orphanEventContext) string {
	if eventType == domain.EventTypeTaskBucketOrphaned {
		payload := orphanBucketPayload{
			TaskID:      task.TaskID,
			ParentID:    task.ParentID,
			Depth:       task.Depth,
			OldBucket:   task.FromBucketKey,
			FromKit:     evCtx.FromKit,
			ToKit:       evCtx.ToKit,
			ResolvedKit: evCtx.ToKit,
			Reason:      "bucket_missing_in_resolved_kit",
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			// Marshal failure on a struct with primitive fields would be
			// a runtime invariant violation, not a data issue. Surface
			// the error context so the audit log row makes the fault
			// auditable instead of silently dropping the event.
			return fmt.Sprintf(`{"reason":"orphan_payload_marshal_failed","error":%q}`, err.Error())
		}
		return string(raw)
	}
	raw, err := json.Marshal(taskMigratedPayload{From: task.FromBucketKey, To: task.ToBucketKey, Reason: "workflow_swap"})
	if err != nil {
		return fmt.Sprintf(`{"reason":"task_migrated_payload_marshal_failed","error":%q}`, err.Error())
	}
	return string(raw)
}
