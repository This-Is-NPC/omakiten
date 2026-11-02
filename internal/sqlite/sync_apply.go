package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"omakiten/internal/domain"
	"omakiten/internal/sqlite/sqlutil"
)

// maxSyncInbox caps the changes parked per project; past it the oldest go.
const maxSyncInbox = 10_000

// maxParentDepth bounds the walk up a task's parent chain when looking for a cycle.
const maxParentDepth = 10_000

// deleteIsFinal reports whether a delete of a row of table beats every later upsert.
// Entity rows are final; join rows (the tables with a key) compare stamps like any other change.
func deleteIsFinal(table string) bool {
	return syncRules[table].key == nil
}

// rowKey names a synced row across tables.
type rowKey struct{ table, uid string }

// pendingChange is a validated incoming change with its parsed stamp.
type pendingChange struct {
	domain.SyncChange
	stamp domain.HLC
	order int
	// missing is the reference this change waits for, set while it is held.
	// The zero rowKey means it waits for the local clock to reach its stamp.
	missing *missingRefError
}

// waitsForClock reports whether the change is parked until the local clock catches up.
func (c pendingChange) waitsForClock() bool {
	return c.missing != nil && c.missing.rowKey == (rowKey{})
}

// missingRefError reports a reference to a row the database does not hold yet.
type missingRefError struct {
	rowKey
}

func (e *missingRefError) Error() string {
	return fmt.Sprintf("missing %s %s", e.table, e.uid)
}

// errParentGone says a required parent of the change was deleted, so the row is gone too.
var errParentGone = errors.New("required parent was deleted")

// rejectedError says a change must not be applied: it is invalid or names a row of another project.
type rejectedError struct{ reason string }

func (e *rejectedError) Error() string { return "sync change rejected: " + e.reason }

func reject(format string, args ...any) error {
	return &rejectedError{reason: fmt.Sprintf(format, args...)}
}

// ApplyChanges applies remote rows of the project last-writer-wins, stores the
// pull cursor and observes the clock in one transaction. Capture is off while
// it runs, so nothing it writes is queued for push. A change whose reference
// has not arrived yet, or whose stamp runs ahead of this machine's clock by
// more than domain.MaxHLCDrift, waits in sync_inbox for a later batch. A change that is
// invalid, names a row of another project or violates a constraint is skipped
// and counted in the result.
func (s *Store) ApplyChanges(ctx context.Context, projectID int64, changes []domain.SyncChange, cursor int64) (int, error) {
	rejected := 0
	err := s.WithinTransaction(ctx, func(ctx context.Context) error {
		a := &applier{s: s, q: s.query(ctx), projectID: projectID, columns: map[string][]string{}, now: time.Now()}
		defer func() { rejected = a.rejected }()
		if _, err := a.q.ExecContext(ctx, `UPDATE sync_identity SET applying = 1 WHERE id = 1`); err != nil {
			return err
		}
		if err := a.applyWithInbox(ctx, changes); err != nil {
			return err
		}
		if _, err := a.q.ExecContext(ctx, `
INSERT INTO sync_projects(project_id, pull_cursor) VALUES (?, ?)
ON CONFLICT(project_id) DO UPDATE SET pull_cursor = excluded.pull_cursor`, projectID, cursor); err != nil {
			return err
		}
		if a.latest != (domain.HLC{}) {
			if err := observeClock(ctx, a.q, a.latest); err != nil {
				return err
			}
		}
		_, err := a.q.ExecContext(ctx, `UPDATE sync_identity SET applying = 0 WHERE id = 1`)
		return err
	})
	return rejected, err
}

// applyWithInbox applies the changes, then the parked ones whose missing
// reference was applied meanwhile, and parks what still waits.
func (a *applier) applyWithInbox(ctx context.Context, changes []domain.SyncChange) error {
	keys := make([]rowKey, len(changes))
	for i, change := range changes {
		keys[i] = rowKey{change.Table, change.RowUID}
	}
	superseded, err := a.takeParked(ctx, "tbl", "row_uid", keys)
	if err != nil {
		return err
	}
	clockWaiters, err := a.takeParked(ctx, "missing_tbl", "missing_uid", []rowKey{{}})
	if err != nil {
		return err
	}
	queue := a.prepare(mergeChanges(append(superseded, clockWaiters...), changes))
	var held []pendingChange
	for len(queue) > 0 {
		if held, err = a.applyAll(ctx, queue); err != nil {
			return err
		}
		woken, err := a.takeParked(ctx, "missing_tbl", "missing_uid", a.settled)
		if err != nil {
			return err
		}
		a.settled = nil
		if len(woken) == 0 {
			break
		}
		queue = sortPending(append(held, a.prepare(woken)...))
	}
	if err := storeInbox(ctx, a.q, a.projectID, append(held, a.future...)); err != nil {
		return err
	}
	dropped, err := capInbox(ctx, a.q, a.projectID)
	a.rejected += dropped
	return err
}

// takeParked removes from the inbox and returns the changes of the project
// whose (tblCol, uidCol) is one of keys.
func (a *applier) takeParked(ctx context.Context, tblCol, uidCol string, keys []rowKey) ([]domain.SyncChange, error) {
	var taken []domain.SyncChange
	query := fmt.Sprintf(`DELETE FROM sync_inbox WHERE project_id = ? AND %s = ? AND %s = ? RETURNING change`, tblCol, uidCol)
	for _, key := range keys {
		rows, err := a.q.QueryContext(ctx, query, a.projectID, key.table, key.uid)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var raw string
			var change domain.SyncChange
			if err := rows.Scan(&raw); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if err := json.Unmarshal([]byte(raw), &change); err != nil {
				_ = rows.Close()
				return nil, err
			}
			taken = append(taken, change)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
	}
	return taken, nil
}

// storeInbox parks the changes still waiting for a reference.
func storeInbox(ctx context.Context, q queryExecutor, projectID int64, waiting []pendingChange) error {
	for _, change := range waiting {
		raw, err := json.Marshal(change.SyncChange)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
INSERT OR REPLACE INTO sync_inbox(project_id, tbl, row_uid, hlc, change, missing_tbl, missing_uid) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			projectID, change.Table, change.RowUID, change.HLC, string(raw), change.missing.table, change.missing.uid); err != nil {
			return err
		}
	}
	return nil
}

// capInbox drops the oldest parked changes of the project beyond maxSyncInbox and returns how many.
func capInbox(ctx context.Context, q queryExecutor, projectID int64) (int, error) {
	var parked int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM sync_inbox WHERE project_id = ?`, projectID).Scan(&parked); err != nil || parked <= maxSyncInbox {
		return 0, err
	}
	_, err := q.ExecContext(ctx, `
DELETE FROM sync_inbox WHERE rowid IN (SELECT rowid FROM sync_inbox WHERE project_id = ? ORDER BY hlc LIMIT ?)`,
		projectID, parked-maxSyncInbox)
	return parked - maxSyncInbox, err
}

// mergeChanges joins parked and incoming changes, keeping the greater stamp per row.
// A malformed stamp is kept as is, so validation reports it.
func mergeChanges(parked, incoming []domain.SyncChange) []domain.SyncChange {
	index := map[rowKey]int{}
	var merged []domain.SyncChange
	for _, change := range append(slices.Clone(parked), incoming...) {
		key := rowKey{change.Table, change.RowUID}
		i, seen := index[key]
		if !seen {
			index[key] = len(merged)
			merged = append(merged, change)
			continue
		}
		kept, errKept := domain.ParseHLC(merged[i].HLC)
		next, errNext := domain.ParseHLC(change.HLC)
		if errKept == nil && (errNext != nil || next.Compare(kept) > 0) {
			merged[i] = change
		}
	}
	return merged
}

// prepare validates the changes, counting the invalid ones as rejected, and
// orders the rest. A change stamped ahead of the clock is set aside in a.future.
func (a *applier) prepare(changes []domain.SyncChange) []pendingChange {
	queue := make([]pendingChange, 0, len(changes))
	for _, change := range changes {
		pending, err := a.check(change)
		switch {
		case err != nil:
			a.rejected++
		case pending.waitsForClock():
			a.future = append(a.future, pending)
		default:
			queue = append(queue, pending)
		}
	}
	return sortPending(queue)
}

// check validates a change and returns it with its stamp and apply order. A
// stamp more than domain.MaxHLCDrift ahead of the clock is not an error: the
// change comes back marked as waiting for the clock.
func (a *applier) check(change domain.SyncChange) (pendingChange, error) {
	stamp, err := domain.ParseHLC(change.HLC)
	if err != nil {
		return pendingChange{}, err
	}
	pending := pendingChange{SyncChange: change, stamp: stamp, order: slices.Index(domain.SyncTables, change.Table)}
	now := a.now
	if stamp.Wall > a.now.Add(domain.MaxHLCDrift).UnixMilli() {
		now, pending.missing = time.UnixMilli(stamp.Wall), &missingRefError{}
	}
	if err := domain.ValidateSyncChange(change, now); err != nil {
		return pendingChange{}, err
	}
	rule := syncRules[change.Table]
	switch {
	case change.Table == "projects" && change.Op == domain.SyncOpDelete:
		return pendingChange{}, reject("a project is never deleted by sync")
	case change.Table != "projects" && hasControlText(change.Data):
		return pendingChange{}, reject("%s %s holds control characters", change.Table, change.RowUID)
	case rule.key != nil && !validJoinKey(rule, change):
		return pendingChange{}, reject("row_uid does not join %s", strings.Join(rule.key, " and "))
	}
	return pending, nil
}

// validJoinKey reports whether the row id of a join change is made of its key parts:
// exactly the data refs for an upsert, one part per key name for a delete.
func validJoinKey(rule syncRule, change domain.SyncChange) bool {
	if change.Op == domain.SyncOpDelete {
		return len(strings.SplitN(change.RowUID, "|", len(rule.key))) == len(rule.key)
	}
	parts := make([]string, len(rule.key))
	for i, name := range rule.key {
		parts[i], _ = change.Data[name].(string)
	}
	return strings.Join(parts, "|") == change.RowUID
}

// hasControlText reports whether a string value holds a C0 control character other than tab, newline and carriage return.
func hasControlText(data map[string]any) bool {
	for _, value := range data {
		text, ok := value.(string)
		if ok && strings.ContainsFunc(text, func(r rune) bool { return r < ' ' && !strings.ContainsRune("\t\n\r", r) }) {
			return true
		}
	}
	return false
}

// sortPending sorts the changes by table apply order, then by stamp.
func sortPending(queue []pendingChange) []pendingChange {
	slices.SortStableFunc(queue, func(a, b pendingChange) int {
		if a.order != b.order {
			return a.order - b.order
		}
		return a.stamp.Compare(b.stamp)
	})
	return queue
}

// observeClock moves the stored clock past the greatest incoming stamp.
func observeClock(ctx context.Context, q queryExecutor, latest domain.HLC) error {
	var last, device string
	if err := q.QueryRowContext(ctx, `SELECT hlc, device_id FROM sync_identity WHERE id = 1`).Scan(&last, &device); err != nil {
		return err
	}
	var prev domain.HLC
	if last != "" {
		parsed, err := domain.ParseHLC(last)
		if err != nil {
			return err
		}
		prev = parsed
	}
	next, err := domain.ObserveHLC(prev, latest, time.Now().UnixMilli(), domain.UID(device))
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `UPDATE sync_identity SET hlc = ? WHERE id = 1`, next.String())
	return err
}

// applier writes one batch of changes into the project inside a transaction.
type applier struct {
	s         *Store
	q         queryExecutor
	projectID int64
	columns   map[string][]string
	now       time.Time
	// latest is the greatest stamp of the changes applied.
	latest domain.HLC
	// future holds the changes stamped ahead of the clock, to be parked.
	future []pendingChange
	// rejected counts the changes skipped as invalid, foreign or violating a constraint.
	rejected int
	// settled lists the rows applied since the inbox was last woken.
	settled []rowKey
}

// applyAll applies the queue, retrying changes whose references are missing
// while a pass still makes progress. It returns the changes still waiting.
func (a *applier) applyAll(ctx context.Context, queue []pendingChange) ([]pendingChange, error) {
	for len(queue) > 0 {
		var held []pendingChange
		for _, change := range queue {
			err := a.applyIsolated(ctx, change)
			var missing *missingRefError
			var rejection *rejectedError
			var constraint *sqlutil.ConstraintError
			switch {
			case err == nil:
				a.settled = append(a.settled, rowKey{change.Table, change.RowUID})
				if change.stamp.Compare(a.latest) > 0 {
					a.latest = change.stamp
				}
			case errors.As(err, &missing):
				change.missing = missing
				held = append(held, change)
			case errors.As(err, &rejection), errors.As(err, &constraint):
				a.rejected++
			default:
				return nil, err
			}
		}
		if len(held) == len(queue) {
			return held, nil
		}
		queue = held
	}
	return nil, nil
}

// applyIsolated applies one change under a savepoint, so a change that fails
// leaves no partial write behind.
func (a *applier) applyIsolated(ctx context.Context, change pendingChange) error {
	if _, err := a.q.ExecContext(ctx, `SAVEPOINT sync_change`); err != nil {
		return err
	}
	err := sqlutil.MapSQLiteError(a.apply(ctx, change))
	if err == nil {
		_, err = a.q.ExecContext(ctx, `RELEASE sync_change`)
		return err
	}
	if _, rollback := a.q.ExecContext(ctx, `ROLLBACK TO sync_change; RELEASE sync_change`); rollback != nil {
		return errors.Join(err, rollback)
	}
	return err
}

// superseded reports whether the row's recorded state already beats the change.
// A delete of an entity row beats every upsert whatever its stamp and is final, as on the server;
// join rows compare stamps in both directions.
func (a *applier) superseded(ctx context.Context, change pendingChange) (bool, error) {
	var stored string
	var deleted bool
	err := a.q.QueryRowContext(ctx, `SELECT COALESCE(hlc, ''), deleted FROM sync_rows WHERE project_id = ? AND tbl = ? AND row_uid = ?`,
		a.projectID, change.Table, change.RowUID).Scan(&stored, &deleted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, err
	case deleted && deleteIsFinal(change.Table):
		return true, nil
	case change.Op == domain.SyncOpDelete && deleteIsFinal(change.Table):
		return false, nil
	}
	var current domain.HLC
	if stored != "" {
		if current, err = domain.ParseHLC(stored); err != nil {
			return false, err
		}
	}
	return change.stamp.Compare(current) <= 0, nil
}

// apply writes one change unless the row's recorded state supersedes it.
func (a *applier) apply(ctx context.Context, change pendingChange) error {
	stale, err := a.superseded(ctx, change)
	if err != nil || stale {
		return err
	}
	switch {
	case change.Table == "projects":
		err = a.updateProject(ctx, change)
	case change.Op == domain.SyncOpDelete:
		err = a.remove(ctx, change)
	default:
		err = a.upsert(ctx, change)
	}
	if errors.Is(err, errParentGone) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = a.q.ExecContext(ctx, `
INSERT INTO sync_rows(project_id, tbl, row_uid, hlc, deleted, local) VALUES (?, ?, ?, ?, ?, 0)`,
		a.projectID, change.Table, change.RowUID, change.HLC, change.Op == domain.SyncOpDelete)
	return err
}

// locate returns the id of the row of table with uid inside the project; found
// is false when no project holds it. A uid held by another project is a rejected change.
func (a *applier) locate(ctx context.Context, table, uid string) (id int64, found bool, err error) {
	err = a.q.QueryRowContext(ctx, fmt.Sprintf("SELECT r.id FROM %s r WHERE r.uid = ? AND %s", table, syncRules[table].scope),
		uid, a.projectID).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	var elsewhere bool
	if err := a.q.QueryRowContext(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE uid = ?)", table), uid).Scan(&elsewhere); err != nil {
		return 0, false, err
	}
	if elsewhere {
		return 0, false, reject("%s %s belongs to another project", table, uid)
	}
	return 0, false, nil
}

// updateProject copies the shared fields onto the local project.
func (a *applier) updateProject(ctx context.Context, change pendingChange) error {
	if err := a.validateProject(ctx, change); err != nil {
		return err
	}
	result, err := a.q.ExecContext(ctx, `
UPDATE projects SET name = COALESCE(?, name), description = COALESCE(?, description), updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND uid = ?`, change.Data["name"], change.Data["description"], a.projectID, change.RowUID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return err
		}
		return reject("project %s is not the one being synced", change.RowUID)
	}
	return nil
}

// validateProject runs the domain text checks on the local project as the change would leave it.
func (a *applier) validateProject(ctx context.Context, change pendingChange) error {
	var info domain.SyncProjectInfo
	var uid sql.NullString
	err := a.q.QueryRowContext(ctx, `SELECT uid, name, slug, description FROM projects WHERE id = ?`, a.projectID).
		Scan(&uid, &info.Name, &info.Slug, &info.Description)
	if err != nil {
		return err
	}
	info.UID = domain.UID(uid.String)
	if name, ok := change.Data["name"].(string); ok {
		info.Name = name
	}
	if description, ok := change.Data["description"].(string); ok {
		info.Description = description
	}
	if err := domain.ValidateSyncProjectInfo(info); err != nil {
		return reject("%v", err)
	}
	return nil
}

// upsert updates the project's row with the uid, or inserts it. A uid held by another project is rejected.
func (a *applier) upsert(ctx context.Context, change pendingChange) error {
	rule := syncRules[change.Table]
	wire, err := a.wireColumns(ctx, change.Table)
	if err != nil {
		return err
	}
	var cols []string
	var vals []any
	for _, col := range wire {
		if value, ok := change.Data[col]; ok {
			cols, vals = append(cols, col), append(vals, value)
		}
	}
	refCols, refVals, err := a.resolveRefs(ctx, change, change.Data, true)
	if err != nil {
		return err
	}
	cols, vals = append(cols, refCols...), append(vals, refVals...)
	var id int64
	found := false
	if rule.key == nil {
		if id, found, err = a.locate(ctx, change.Table, change.RowUID); err != nil {
			return err
		}
	}
	if found {
		return a.update(ctx, change.Table, id, cols, vals)
	}
	if rule.projectScoped() {
		cols, vals = append(cols, "project_id"), append(vals, a.projectID)
	}
	verb := "INSERT"
	if rule.key != nil {
		verb = "INSERT OR IGNORE"
	} else {
		cols, vals = append(cols, "uid"), append(vals, change.RowUID)
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	_, err = a.q.ExecContext(ctx, fmt.Sprintf("%s INTO %s(%s) VALUES (%s)", verb, change.Table, strings.Join(cols, ", "), marks), vals...)
	return err
}

// update sets the columns of the row with id, unless that would make a task its own ancestor.
func (a *applier) update(ctx context.Context, table string, id int64, cols []string, vals []any) error {
	if err := a.checkParentCycle(ctx, table, id, cols, vals); err != nil {
		return err
	}
	set := make([]string, len(cols))
	for i, col := range cols {
		set[i] = col + " = ?"
	}
	_, err := a.q.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", table, strings.Join(set, ", ")), append(vals, id)...)
	return err
}

// checkParentCycle rejects a task update whose new parent has the task among its ancestors.
func (a *applier) checkParentCycle(ctx context.Context, table string, id int64, cols []string, vals []any) error {
	i := slices.Index(cols, "parent_id")
	if table != "tasks" || i < 0 || vals[i] == nil {
		return nil
	}
	var cycle bool
	err := a.q.QueryRowContext(ctx, `
WITH RECURSIVE ancestors(id, depth) AS (
  SELECT ?, 0
  UNION ALL
  SELECT t.parent_id, a.depth + 1 FROM tasks t JOIN ancestors a ON t.id = a.id
  WHERE t.parent_id IS NOT NULL AND a.depth < ?
)
SELECT EXISTS(SELECT 1 FROM ancestors WHERE id = ?)`, vals[i], maxParentDepth, id).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle {
		return reject("task parent would make a cycle")
	}
	return nil
}

// remove deletes the project's row; a row that is already gone is fine, one of another project is rejected.
func (a *applier) remove(ctx context.Context, change pendingChange) error {
	rule := syncRules[change.Table]
	if rule.key != nil {
		return a.removeJoinRow(ctx, change, rule)
	}
	id, found, err := a.locate(ctx, change.Table, change.RowUID)
	if err != nil || !found {
		return err
	}
	if change.Table == "tasks" {
		return deleteTaskRows(ctx, a.q, a.projectID, id)
	}
	_, err = a.q.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", change.Table), id)
	return err
}

// removeJoinRow deletes the join row named by the parts of its wire row id.
func (a *applier) removeJoinRow(ctx context.Context, change pendingChange, rule syncRule) error {
	parts := strings.SplitN(change.RowUID, "|", len(rule.key))
	data := make(map[string]any, len(parts))
	for i, name := range rule.key {
		data[name] = parts[i]
	}
	cols, vals, err := a.resolveRefs(ctx, change, data, false)
	var missing *missingRefError
	if errors.As(err, &missing) {
		return nil
	}
	if err != nil {
		return err
	}
	where := make([]string, len(cols))
	for i, col := range cols {
		where[i] = col + " = ?"
	}
	_, err = a.q.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", change.Table, strings.Join(where, " AND ")), vals...)
	return err
}

// wireColumns lists the plain wire columns of table, read once per batch.
func (a *applier) wireColumns(ctx context.Context, table string) ([]string, error) {
	if cols, ok := a.columns[table]; ok {
		return cols, nil
	}
	cols, err := syncRules[table].columns(ctx, a.q, table)
	if err != nil {
		return nil, err
	}
	a.columns[table] = cols
	return cols, nil
}

// resolveRefs turns the uid-valued entries of data back into local ids,
// returning the columns they fill. Tags are created when create is set.
func (a *applier) resolveRefs(ctx context.Context, change pendingChange, data map[string]any, create bool) (cols []string, vals []any, err error) {
	for _, ref := range syncRules[change.Table].refs {
		if slices.Contains(cols, ref.col) {
			continue
		}
		id, err := a.resolveRef(ctx, ref, data, create)
		if err != nil {
			return nil, nil, err
		}
		cols, vals = append(cols, ref.col), append(vals, id)
	}
	return cols, vals, nil
}

// resolveRef returns the local id a reference names, nil when it is empty.
func (a *applier) resolveRef(ctx context.Context, ref syncRef, data map[string]any, create bool) (any, error) {
	if ref.col == "tag_id" {
		return a.resolveTag(ctx, data, create)
	}
	uid, _ := data[ref.name].(string)
	if uid == "" {
		return nil, nil
	}
	table := ref.table
	if ref.col == "entity_id" {
		kind, _ := data["entity_type"].(string)
		i := slices.IndexFunc(entityTables, func(e struct{ kind, table string }) bool { return e.kind == kind })
		if i < 0 {
			return nil, reject("unknown entity_type %q", kind)
		}
		table = entityTables[i].table
	}
	id, found, err := a.locate(ctx, table, uid)
	if err != nil {
		return nil, err
	}
	if found {
		return id, nil
	}
	if create {
		var deleted bool
		err := a.q.QueryRowContext(ctx, `SELECT deleted FROM sync_rows WHERE project_id = ? AND tbl = ? AND row_uid = ?`, a.projectID, table, uid).Scan(&deleted)
		switch {
		case err == nil && deleted && ref.required:
			return nil, errParentGone
		case err == nil && deleted:
			return nil, nil
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return nil, err
		}
	}
	return nil, &missingRefError{rowKey{table, uid}}
}

func (a *applier) resolveTag(ctx context.Context, data map[string]any, create bool) (any, error) {
	name, _ := data["tag_name"].(string)
	if name == "" {
		return nil, reject("change has no tag_name")
	}
	if create {
		label, _ := data["label"].(string)
		if label == "" {
			label = name
		}
		tag, err := a.s.FindOrCreateTag(ctx, name, label)
		return tag.ID, err
	}
	var id int64
	err := a.q.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &missingRefError{rowKey{"tags", name}}
	}
	return id, err
}
