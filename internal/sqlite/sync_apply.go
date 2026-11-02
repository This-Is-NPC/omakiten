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
)

// pendingChange is a validated incoming change with its parsed stamp.
type pendingChange struct {
	domain.SyncChange
	stamp domain.HLC
	order int
}

// missingRefError reports a reference to a row the database does not hold yet.
type missingRefError struct {
	ref, uid string
}

func (e *missingRefError) Error() string {
	return fmt.Sprintf("missing %s %s", e.ref, e.uid)
}

// errParentGone says a required parent of the change was deleted, so the row is gone too.
var errParentGone = errors.New("required parent was deleted")

// ApplyChanges applies remote rows of the project last-writer-wins, stores the
// pull cursor and observes the clock in one transaction. Capture is off while
// it runs, so nothing it writes is queued for push. A change whose reference
// has not arrived yet waits in sync_inbox for a later batch.
func (s *Store) ApplyChanges(ctx context.Context, projectID int64, changes []domain.SyncChange, cursor int64) error {
	return s.WithinTransaction(ctx, func(ctx context.Context) error {
		q := s.query(ctx)
		if _, err := q.ExecContext(ctx, `UPDATE sync_identity SET applying = 1 WHERE id = 1`); err != nil {
			return err
		}
		a := &applier{s: s, q: q, projectID: projectID, columns: map[string][]string{}}
		latest, err := a.applyWithInbox(ctx, changes)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `
INSERT INTO sync_projects(project_id, pull_cursor) VALUES (?, ?)
ON CONFLICT(project_id) DO UPDATE SET pull_cursor = excluded.pull_cursor`, projectID, cursor); err != nil {
			return err
		}
		if latest != nil {
			if err := observeClock(ctx, q, *latest); err != nil {
				return err
			}
		}
		_, err = q.ExecContext(ctx, `UPDATE sync_identity SET applying = 0 WHERE id = 1`)
		return err
	})
}

// applyWithInbox applies the changes together with the parked ones and parks
// what still waits. It returns the greatest stamp seen, nil when there was none.
func (a *applier) applyWithInbox(ctx context.Context, changes []domain.SyncChange) (*domain.HLC, error) {
	parked, err := loadInbox(ctx, a.q, a.projectID)
	if err != nil {
		return nil, err
	}
	queue, latest, err := orderChanges(mergeChanges(parked, changes))
	if err != nil {
		return nil, err
	}
	unresolved, err := a.applyAll(ctx, queue)
	if err != nil {
		return nil, err
	}
	if err := storeInbox(ctx, a.q, a.projectID, unresolved); err != nil {
		return nil, err
	}
	if len(queue) == 0 {
		return nil, nil
	}
	return &latest, nil
}

// loadInbox returns the changes parked by earlier batches of the project.
func loadInbox(ctx context.Context, q queryExecutor, projectID int64) ([]domain.SyncChange, error) {
	rows, err := q.QueryContext(ctx, `SELECT change FROM sync_inbox WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var parked []domain.SyncChange
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var change domain.SyncChange
		if err := json.Unmarshal([]byte(raw), &change); err != nil {
			return nil, err
		}
		parked = append(parked, change)
	}
	return parked, rows.Err()
}

// storeInbox replaces the project's inbox with the changes still waiting for a reference.
func storeInbox(ctx context.Context, q queryExecutor, projectID int64, waiting []pendingChange) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM sync_inbox WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	for _, change := range waiting {
		raw, err := json.Marshal(change.SyncChange)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `INSERT OR REPLACE INTO sync_inbox(project_id, tbl, row_uid, hlc, change) VALUES (?, ?, ?, ?, ?)`,
			projectID, change.Table, change.RowUID, change.HLC, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

// mergeChanges joins parked and incoming changes, keeping the greater stamp per row.
// A malformed stamp is kept as is, so validation reports it.
func mergeChanges(parked, incoming []domain.SyncChange) []domain.SyncChange {
	type rowKey struct{ table, uid string }
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

// orderChanges validates the changes and sorts them by table apply order,
// then by stamp. It also returns the greatest stamp.
func orderChanges(changes []domain.SyncChange) ([]pendingChange, domain.HLC, error) {
	queue := make([]pendingChange, 0, len(changes))
	var latest domain.HLC
	for _, change := range changes {
		rule, known := syncRules[change.Table]
		order := slices.Index(domain.SyncTables, change.Table)
		switch {
		case !known || order < 0:
			return nil, latest, syncChangeInvalid(change, "table is not synced")
		case change.RowUID == "":
			return nil, latest, syncChangeInvalid(change, "row_uid is empty")
		case change.Op != domain.SyncOpUpsert && change.Op != domain.SyncOpDelete:
			return nil, latest, syncChangeInvalid(change, "unknown op "+string(change.Op))
		case change.Table == "projects" && change.Op == domain.SyncOpDelete:
			return nil, latest, syncChangeInvalid(change, "a project is never deleted by sync")
		case rule.key != nil && len(strings.SplitN(change.RowUID, "|", len(rule.key))) != len(rule.key):
			return nil, latest, syncChangeInvalid(change, "row_uid does not join "+strings.Join(rule.key, " and "))
		}
		stamp, err := domain.ParseHLC(change.HLC)
		if err != nil {
			return nil, latest, err
		}
		if stamp.Compare(latest) > 0 {
			latest = stamp
		}
		queue = append(queue, pendingChange{SyncChange: change, stamp: stamp, order: order})
	}
	slices.SortStableFunc(queue, func(a, b pendingChange) int {
		if a.order != b.order {
			return a.order - b.order
		}
		return a.stamp.Compare(b.stamp)
	})
	return queue, latest, nil
}

func syncChangeInvalid(change domain.SyncChange, reason string) error {
	return domain.NewError(domain.ErrValidation,
		fmt.Sprintf("invalid sync change %s %s: %s", change.Table, change.RowUID, reason),
		map[string]any{"table": change.Table, "row_uid": change.RowUID})
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
}

// applyAll applies the queue, retrying changes whose references are missing
// while a pass still makes progress. It returns the changes still waiting.
func (a *applier) applyAll(ctx context.Context, queue []pendingChange) ([]pendingChange, error) {
	for len(queue) > 0 {
		var held []pendingChange
		for _, change := range queue {
			err := a.apply(ctx, change)
			var missing *missingRefError
			if errors.As(err, &missing) {
				held = append(held, change)
				continue
			}
			if err != nil {
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

// apply writes one change when it is newer than what the row last recorded.
func (a *applier) apply(ctx context.Context, change pendingChange) error {
	var stored string
	var deleted bool
	err := a.q.QueryRowContext(ctx, `SELECT COALESCE(hlc, ''), deleted FROM sync_rows WHERE project_id = ? AND tbl = ? AND row_uid = ?`,
		a.projectID, change.Table, change.RowUID).Scan(&stored, &deleted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case deleted && change.Op == domain.SyncOpUpsert:
		return nil
	default:
		var current domain.HLC
		if stored != "" {
			if current, err = domain.ParseHLC(stored); err != nil {
				return err
			}
		}
		if change.stamp.Compare(current) <= 0 {
			return nil
		}
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

// updateProject copies the shared fields onto the local project.
func (a *applier) updateProject(ctx context.Context, change pendingChange) error {
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
		return syncChangeInvalid(change.SyncChange, "the project is not the one being synced")
	}
	return nil
}

// upsert inserts the row, or overwrites it when its uid is already local.
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
	if rule.projectScoped() {
		cols, vals = append(cols, "project_id"), append(vals, a.projectID)
	}
	if rule.key != nil {
		return a.exec(ctx, "INSERT OR IGNORE", change.Table, cols, vals, "")
	}
	update := make([]string, len(cols))
	for i, col := range cols {
		update[i] = col + " = excluded." + col
	}
	cols, vals = append(cols, "uid"), append(vals, change.RowUID)
	return a.exec(ctx, "INSERT", change.Table, cols, vals, " ON CONFLICT(uid) DO UPDATE SET "+strings.Join(update, ", "))
}

func (a *applier) exec(ctx context.Context, verb, table string, cols []string, vals []any, tail string) error {
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	_, err := a.q.ExecContext(ctx,
		fmt.Sprintf("%s INTO %s(%s) VALUES (%s)%s", verb, table, strings.Join(cols, ", "), marks, tail), vals...)
	return err
}

// remove deletes the local row; a row that is already gone is fine.
func (a *applier) remove(ctx context.Context, change pendingChange) error {
	rule := syncRules[change.Table]
	switch {
	case rule.key != nil:
		return a.removeJoinRow(ctx, change, rule)
	case change.Table == "tasks":
		var id, projectID int64
		err := a.q.QueryRowContext(ctx, `SELECT id, project_id FROM tasks WHERE uid = ?`, change.RowUID).Scan(&id, &projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return deleteTaskRows(ctx, a.q, projectID, id)
	default:
		_, err := a.q.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE uid = ?", change.Table), change.RowUID)
		return err
	}
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
			return nil, domain.NewError(domain.ErrValidation, fmt.Sprintf("unknown entity_type %q", kind), map[string]any{"entity_type": kind})
		}
		table = entityTables[i].table
	}
	var id int64
	err := a.q.QueryRowContext(ctx, fmt.Sprintf("SELECT id FROM %s WHERE uid = ?", table), uid).Scan(&id)
	if !errors.Is(err, sql.ErrNoRows) {
		return id, err
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
	return nil, &missingRefError{ref: ref.name, uid: uid}
}

func (a *applier) resolveTag(ctx context.Context, data map[string]any, create bool) (any, error) {
	name, _ := data["tag_name"].(string)
	if name == "" {
		return nil, domain.NewError(domain.ErrValidation, "sync change has no tag_name", nil)
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
		return nil, &missingRefError{ref: "tag_name", uid: name}
	}
	return id, err
}
