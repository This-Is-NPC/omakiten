package sqlite

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// syncRef replaces the column col of a synced row with the value expr (over
// the row alias r) under the key name.
type syncRef struct {
	col, name, expr string
}

// syncRule says how one synced table maps to wire rows.
type syncRule struct {
	// rowUID is the SQL over alias r that yields the wire row id.
	rowUID string
	// scope is the SQL predicate over alias r selecting the rows of the project bound to its single placeholder.
	scope string
	// omit lists the columns that never leave the device, besides id and uid.
	omit []string
	// refs replaces local-id columns with the values that identify them globally.
	refs []syncRef
}

func uidRef(col, name, table string) syncRef {
	return syncRef{col, name, fmt.Sprintf("(SELECT uid FROM %s WHERE id = r.%s)", table, col)}
}

func joinRowUID(parts ...string) string {
	return strings.Join(parts, " || '|' || ")
}

func parentUID(col, table string) string {
	return uidRef(col, "", table).expr
}

func inProject(col, table string) string {
	return fmt.Sprintf("r.%s IN (SELECT id FROM %s WHERE project_id = ?)", col, table)
}

const (
	tagNameExpr   = "(SELECT name FROM tags WHERE id = r.tag_id)"
	byProjectID   = "r.project_id = ?"
	entityUIDExpr = `CASE r.entity_type
  WHEN 'task' THEN (SELECT uid FROM tasks WHERE id = r.entity_id)
  WHEN 'plan' THEN (SELECT uid FROM plans WHERE id = r.entity_id)
  WHEN 'error' THEN (SELECT uid FROM errors WHERE id = r.entity_id)
  WHEN 'solution' THEN (SELECT uid FROM solutions WHERE id = r.entity_id)
END`
)

var tagRefs = []syncRef{
	{"tag_id", "tag_name", tagNameExpr},
	{"tag_id", "label", "(SELECT label FROM tags WHERE id = r.tag_id)"},
}

// syncRules holds the rule of every table in domain.SyncTables.
var syncRules = map[string]syncRule{
	"projects": {rowUID: "r.uid", scope: "r.id = ?", omit: []string{"root_path", "sync_scope", "archived_at"}},
	"plans":    {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"}},
	"plan_waves": {rowUID: "r.uid", scope: inProject("plan_id", "plans"),
		refs: []syncRef{uidRef("plan_id", "plan_uid", "plans")}},
	"tasks": {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"},
		refs: []syncRef{uidRef("parent_id", "parent_uid", "tasks"), uidRef("plan_id", "plan_uid", "plans"), uidRef("wave_id", "wave_uid", "plan_waves")}},
	"errors": {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"}},
	"solutions": {rowUID: "r.uid", scope: inProject("error_id", "errors"),
		refs: []syncRef{uidRef("error_id", "error_uid", "errors"), uidRef("task_id", "task_uid", "tasks")}},
	"events": {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"},
		refs: []syncRef{{"entity_id", "entity_uid", entityUIDExpr}}},
	"task_dependencies": {
		rowUID: joinRowUID(parentUID("task_id", "tasks"), parentUID("depends_on_task_id", "tasks")),
		scope:  byProjectID, omit: []string{"project_id"},
		refs: []syncRef{uidRef("task_id", "task_uid", "tasks"), uidRef("depends_on_task_id", "depends_on_task_uid", "tasks")}},
	"task_tags": {
		rowUID: joinRowUID(parentUID("task_id", "tasks"), tagNameExpr),
		scope:  byProjectID, omit: []string{"project_id"},
		refs: append([]syncRef{uidRef("task_id", "task_uid", "tasks")}, tagRefs...)},
	"error_tags": {
		rowUID: joinRowUID(parentUID("error_id", "errors"), tagNameExpr),
		scope:  inProject("error_id", "errors"),
		refs:   append([]syncRef{uidRef("error_id", "error_uid", "errors")}, tagRefs...)},
	"event_tags": {
		rowUID: joinRowUID(parentUID("event_id", "events"), tagNameExpr),
		scope:  inProject("event_id", "events"),
		refs:   append([]syncRef{uidRef("event_id", "event_uid", "events")}, tagRefs...)},
}

// selectList returns the SELECT list of the wire columns of table: its own
// columns minus id, uid, omitted and replaced ones, then the references.
func (r syncRule) selectList(ctx context.Context, q queryExecutor, table string) (string, error) {
	skip := map[string]bool{"id": true, "uid": true}
	for _, col := range r.omit {
		skip[col] = true
	}
	for _, ref := range r.refs {
		skip[ref.col] = true
	}
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var list []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		if !skip[name] {
			list = append(list, "r."+name)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, ref := range r.refs {
		list = append(list, ref.expr+" AS "+ref.name)
	}
	return strings.Join(list, ", "), nil
}

// enqueueProject records every existing row of the project in every synced
// table as a local change.
func (s *Store) enqueueProject(ctx context.Context, projectID int64) error {
	for _, table := range domain.SyncTables {
		rule := syncRules[table]
		_, err := s.query(ctx).ExecContext(ctx, fmt.Sprintf(`
INSERT INTO sync_rows(project_id, tbl, row_uid)
SELECT ?, '%s', u FROM (SELECT %s AS u FROM %s r WHERE %s) WHERE u IS NOT NULL`,
			table, rule.rowUID, table, rule.scope), projectID, projectID)
		if err != nil {
			return fmt.Errorf("enqueue %s: %w", table, err)
		}
	}
	return nil
}

// SyncedProjects lists the local projects whose scope is synced, with their
// replication cursors. KitKey is left for the caller, which knows the kit.
func (s *Store) SyncedProjects(ctx context.Context) ([]domain.SyncProject, error) {
	rows, err := s.query(ctx).QueryContext(ctx, `
SELECT p.id, p.uid, p.name, p.slug, p.description, COALESCE(sp.pushed_hlc, ''), COALESCE(sp.pull_cursor, 0)
FROM projects p LEFT JOIN sync_projects sp ON sp.project_id = p.id
WHERE p.sync_scope = 'synced' AND p.archived_at IS NULL
ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var projects []domain.SyncProject
	for rows.Next() {
		var p domain.SyncProject
		if err := rows.Scan(&p.ID, &p.Info.UID, &p.Info.Name, &p.Info.Slug, &p.Info.Description, &p.PushedHLC, &p.PullCursor); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// pendingRow is one sync_rows entry waiting to be pushed.
type pendingRow struct {
	table, uid, hlc string
	deleted         bool
}

// PendingChanges returns up to limit recorded changes of the project newer
// than its pushed HLC, oldest first, with the current state of each row.
func (s *Store) PendingChanges(ctx context.Context, projectID int64, limit int) ([]domain.SyncChange, error) {
	device, _, err := s.SyncIdentity(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := s.pendingRows(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	changes := make([]domain.SyncChange, 0, len(pending))
	for _, p := range pending {
		change := domain.SyncChange{Table: p.table, RowUID: p.uid, Op: domain.SyncOpDelete, HLC: p.hlc, Device: device}
		if !p.deleted {
			data, err := s.rowData(ctx, p.table, p.uid)
			if err != nil {
				return nil, err
			}
			if data != nil {
				change.Op, change.Data = domain.SyncOpUpsert, data
			}
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (s *Store) pendingRows(ctx context.Context, projectID int64, limit int) ([]pendingRow, error) {
	rows, err := s.query(ctx).QueryContext(ctx, `
SELECT tbl, row_uid, hlc, deleted FROM sync_rows
WHERE project_id = ? AND local = 1
  AND hlc > COALESCE((SELECT pushed_hlc FROM sync_projects WHERE project_id = sync_rows.project_id), '')
ORDER BY hlc LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var pending []pendingRow
	for rows.Next() {
		var p pendingRow
		if err := rows.Scan(&p.table, &p.uid, &p.hlc, &p.deleted); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

// rowData reads the wire columns of one row; nil means the row no longer exists.
func (s *Store) rowData(ctx context.Context, table, rowUID string) (map[string]any, error) {
	rule, ok := syncRules[table]
	if !ok {
		return nil, fmt.Errorf("sync table %q has no rule", table)
	}
	list, err := rule.selectList(ctx, s.query(ctx), table)
	if err != nil {
		return nil, err
	}
	rows, err := s.query(ctx).QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s r WHERE %s = ?", list, table, rule.rowUID), rowUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(names))
	targets := make([]any, len(names))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := rows.Scan(targets...); err != nil {
		return nil, err
	}
	data := make(map[string]any, len(names))
	for i, name := range names {
		data[name] = values[i]
	}
	return data, rows.Err()
}

// MarkPushed raises the project's push watermark to hlc.
func (s *Store) MarkPushed(ctx context.Context, projectID int64, hlc string) error {
	_, err := s.query(ctx).ExecContext(ctx, `
INSERT INTO sync_projects(project_id, pushed_hlc) VALUES (?, ?)
ON CONFLICT(project_id) DO UPDATE SET pushed_hlc = excluded.pushed_hlc
WHERE excluded.pushed_hlc > pushed_hlc`, projectID, hlc)
	return err
}

// SyncRemoteURL returns the configured sync server URL.
func (s *Store) SyncRemoteURL(ctx context.Context) (string, error) {
	var url string
	err := s.query(ctx).QueryRowContext(ctx, `SELECT remote_url FROM sync_identity WHERE id = 1`).Scan(&url)
	return url, err
}

// SetSyncRemoteURL stores the sync server URL.
func (s *Store) SetSyncRemoteURL(ctx context.Context, url string) error {
	_, err := s.query(ctx).ExecContext(ctx, `UPDATE sync_identity SET remote_url = ? WHERE id = 1`, url)
	return err
}

var _ interface {
	SyncedProjects(context.Context) ([]domain.SyncProject, error)
	PendingChanges(context.Context, int64, int) ([]domain.SyncChange, error)
	MarkPushed(context.Context, int64, string) error
	SyncRemoteURL(context.Context) (string, error)
	SetSyncRemoteURL(context.Context, string) error
} = (*Store)(nil)
