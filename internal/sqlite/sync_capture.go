package sqlite

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"omakiten/internal/domain"
)

// syncRef replaces the column col of a synced row with the value expr (over
// the row alias r) under the key name.
type syncRef struct {
	col, name, expr string
	// table is the table whose uid the value names; empty for tags and entities.
	table string
	// required marks a parent the row cannot exist without; an optional ref to a deleted row becomes NULL.
	required bool
}

// syncRule says how one synced table maps to wire rows.
type syncRule struct {
	// rowUID is the SQL over alias r that yields the wire row id.
	rowUID string
	// scope is the SQL predicate over alias r selecting the rows of the project bound to its single
	// placeholder: how the table reaches its project, directly or through a parent. Capture and apply both read it.
	scope string
	// deviceOnly is the SQL predicate over alias r selecting the rows that never leave the device; empty when none do.
	deviceOnly string
	// omit lists the columns that never leave the device, besides id and uid.
	omit []string
	// refs replaces local-id columns with the values that identify them globally.
	refs []syncRef
	// key lists, for join tables, the ref names whose values make up the wire row id, in order.
	key []string
}

func uidRef(col, name, table string) syncRef {
	return syncRef{col: col, name: name, expr: fmt.Sprintf("(SELECT uid FROM %s WHERE id = r.%s)", table, col), table: table}
}

// parentRef is a uidRef to a row the referencing row cannot exist without.
func parentRef(col, name, table string) syncRef {
	ref := uidRef(col, name, table)
	ref.required = true
	return ref
}

// joinRule builds the rule of a join table, whose wire row id joins the key refs.
func joinRule(scope string, key []string, refs ...syncRef) syncRule {
	exprs := make([]string, len(key))
	for i, name := range key {
		exprs[i] = refs[slices.IndexFunc(refs, func(ref syncRef) bool { return ref.name == name })].expr
	}
	omit := []string(nil)
	if scope == byProjectID {
		omit = []string{"project_id"}
	}
	return syncRule{rowUID: strings.Join(exprs, " || '|' || "), scope: scope, omit: omit, refs: refs, key: key}
}

// captured is the SQL predicate over alias r selecting the rows of the project bound to its single placeholder that sync captures.
func (r syncRule) captured() string {
	if r.deviceOnly == "" {
		return r.scope
	}
	return r.scope + " AND NOT (" + r.deviceOnly + ")"
}

// projectScoped reports whether the table carries a project_id column on the wire side.
func (r syncRule) projectScoped() bool {
	return slices.Contains(r.omit, "project_id")
}

func inProject(col, table string) string {
	return fmt.Sprintf("r.%s IN (SELECT id FROM %s WHERE project_id = ?)", col, table)
}

const (
	tagNameExpr = "(SELECT name FROM tags WHERE id = r.tag_id)"
	byProjectID = "r.project_id = ?"
)

// entityTables maps the entity_type of an event to the table holding its subject.
var entityTables = []struct{ kind, table string }{
	{"task", "tasks"}, {"plan", "plans"}, {"error", "errors"}, {"solution", "solutions"}, {"project", "projects"},
}

// entityUIDExpr yields the uid of the subject of an events row.
var entityUIDExpr = func() string {
	var b strings.Builder
	b.WriteString("CASE r.entity_type\n")
	for _, e := range entityTables {
		fmt.Fprintf(&b, "  WHEN '%s' THEN (SELECT uid FROM %s WHERE id = r.entity_id)\n", e.kind, e.table)
	}
	b.WriteString("END")
	return b.String()
}()

var tagRefs = []syncRef{
	{col: "tag_id", name: "tag_name", expr: tagNameExpr},
	{col: "tag_id", name: "label", expr: "(SELECT label FROM tags WHERE id = r.tag_id)"},
}

// syncRules holds the rule of every table in domain.SyncTables.
var syncRules = map[string]syncRule{
	"projects": {rowUID: "r.uid", scope: "r.id = ?", omit: []string{"root_path", "sync_scope", "archived_at"}},
	"plans":    {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"}},
	"plan_waves": {rowUID: "r.uid", scope: inProject("plan_id", "plans"),
		refs: []syncRef{parentRef("plan_id", "plan_uid", "plans")}},
	"tasks": {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"},
		refs: []syncRef{parentRef("parent_id", "parent_uid", "tasks"), uidRef("plan_id", "plan_uid", "plans"), uidRef("wave_id", "wave_uid", "plan_waves")}},
	"errors": {rowUID: "r.uid", scope: byProjectID, omit: []string{"project_id"}},
	"solutions": {rowUID: "r.uid", scope: inProject("error_id", "errors"),
		refs: []syncRef{parentRef("error_id", "error_uid", "errors"), uidRef("task_id", "task_uid", "tasks")}},
	"events": {rowUID: "r.uid", scope: byProjectID, deviceOnly: "r.event_type IN (SELECT event_type FROM sync_activity_event_types)", omit: []string{"project_id"},
		refs: []syncRef{{col: "entity_id", name: "entity_uid", expr: entityUIDExpr}}},
	"task_dependencies": joinRule(byProjectID, []string{"task_uid", "depends_on_task_uid"},
		parentRef("task_id", "task_uid", "tasks"), parentRef("depends_on_task_id", "depends_on_task_uid", "tasks")),
	"task_tags": joinRule(byProjectID, []string{"task_uid", "tag_name"},
		append([]syncRef{parentRef("task_id", "task_uid", "tasks")}, tagRefs...)...),
	"error_tags": joinRule(inProject("error_id", "errors"), []string{"error_uid", "tag_name"},
		append([]syncRef{parentRef("error_id", "error_uid", "errors")}, tagRefs...)...),
	"event_tags": joinRule(inProject("event_id", "events"), []string{"event_uid", "tag_name"},
		append([]syncRef{parentRef("event_id", "event_uid", "events")}, tagRefs...)...),
}

// columns lists the wire columns of table: its own columns minus id, uid,
// omitted and replaced ones.
func (r syncRule) columns(ctx context.Context, q queryExecutor, table string) ([]string, error) {
	skip := map[string]bool{"id": true, "uid": true}
	for _, col := range r.omit {
		skip[col] = true
	}
	for _, ref := range r.refs {
		skip[ref.col] = true
	}
	rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var list []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !skip[name] {
			list = append(list, name)
		}
	}
	return list, rows.Err()
}

// selectList returns the SELECT list of the wire columns of table, then the references.
func (r syncRule) selectList(ctx context.Context, q queryExecutor, table string) (string, error) {
	columns, err := r.columns(ctx, q, table)
	if err != nil {
		return "", err
	}
	list := make([]string, 0, len(columns)+len(r.refs))
	for _, name := range columns {
		list = append(list, "r."+name)
	}
	for _, ref := range r.refs {
		list = append(list, ref.expr+" AS "+ref.name)
	}
	return strings.Join(list, ", "), nil
}

// enqueueProject records every existing row of the project in every synced
// table as a local change, then re-records the entries no live row replaced,
// so rows deleted while the project was local go out as deletes.
func (s *Store) enqueueProject(ctx context.Context, projectID int64) error {
	var before string
	if err := s.query(ctx).QueryRowContext(ctx, `SELECT hlc FROM sync_identity WHERE id = 1`).Scan(&before); err != nil {
		return err
	}
	for _, table := range domain.SyncTables {
		rule := syncRules[table]
		_, err := s.query(ctx).ExecContext(ctx, fmt.Sprintf(`
INSERT INTO sync_rows(project_id, tbl, row_uid)
SELECT ?, '%s', u FROM (SELECT %s AS u FROM %s r WHERE %s) WHERE u IS NOT NULL`,
			table, rule.rowUID, table, rule.captured()), projectID, projectID)
		if err != nil {
			return fmt.Errorf("enqueue %s: %w", table, err)
		}
	}
	_, err := s.query(ctx).ExecContext(ctx, `
INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
SELECT project_id, tbl, row_uid, deleted FROM sync_rows WHERE project_id = ? AND COALESCE(hlc, '') <= ?`, projectID, before)
	if err != nil {
		return fmt.Errorf("re-enqueue stale sync rows: %w", err)
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
	lists := map[string]string{}
	changes := make([]domain.SyncChange, 0, len(pending))
	for _, p := range pending {
		change := domain.SyncChange{Table: p.table, RowUID: p.uid, Op: domain.SyncOpDelete, HLC: p.hlc, Device: device}
		if !p.deleted {
			data, err := s.rowData(ctx, lists, p.table, p.uid)
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
// lists caches the select list of each table across calls.
func (s *Store) rowData(ctx context.Context, lists map[string]string, table, rowUID string) (map[string]any, error) {
	rule, ok := syncRules[table]
	if !ok {
		return nil, fmt.Errorf("sync table %q has no rule", table)
	}
	list, cached := lists[table]
	if !cached {
		var err error
		if list, err = rule.selectList(ctx, s.query(ctx), table); err != nil {
			return nil, err
		}
		lists[table] = list
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
