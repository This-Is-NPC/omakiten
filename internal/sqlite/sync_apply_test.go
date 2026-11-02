package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"omakiten/internal/domain"
)

// syncPair is a synced source store with a populated project, a destination
// store holding its clone, and the source's pending changes.
type syncPair struct {
	a, b     *Store
	aProject domain.Project
	bProject domain.Project
	uids     map[string]string
	changes  []domain.SyncChange
	deviceA  domain.UID
	maxHLC   domain.HLC
	ctx      context.Context
}

func newSyncPair(t *testing.T) *syncPair {
	t.Helper()
	ctx := context.Background()
	a, project := openSyncStore(t)
	setScope(t, a, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, a, project.ID)
	pending, err := a.PendingChanges(ctx, project.ID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	changes := wireRoundTrip(t, pending)
	b, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	info := domain.SyncProjectInfo{UID: domain.UID(mustScan[string](t, a, `SELECT uid FROM projects WHERE id = ?`, project.ID)),
		Name: project.Name, Slug: project.Slug, Description: project.Description}
	cloned, err := b.CreateClonedProject(ctx, info, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	device, _, err := a.SyncIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := &syncPair{a: a, b: b, aProject: project, bProject: cloned, uids: uids, changes: changes, deviceA: device, ctx: ctx}
	for _, c := range changes {
		if stamp := mustParseHLC(t, c.HLC); stamp.Compare(p.maxHLC) > 0 {
			p.maxHLC = stamp
		}
	}
	return p
}

// wireRoundTrip passes changes through JSON, which turns every number into a float64 as on the wire.
func wireRoundTrip(t *testing.T, changes []domain.SyncChange) []domain.SyncChange {
	t.Helper()
	raw, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.SyncChange
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustParseHLC(t *testing.T, s string) domain.HLC {
	t.Helper()
	h, err := domain.ParseHLC(s)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// soon is a wall clock reading minutes ahead of now, newer than every stamp the source made.
func soon(minutes int64) int64 {
	return time.Now().UnixMilli() + minutes*60_000
}

func (p *syncPair) apply(t *testing.T, changes []domain.SyncChange, cursor int64) (int, error) {
	t.Helper()
	return p.b.ApplyChanges(p.ctx, p.bProject.ID, changes, cursor)
}

func (p *syncPair) mustApply(t *testing.T, changes []domain.SyncChange, cursor int64) {
	t.Helper()
	if rejected, err := p.apply(t, changes, cursor); err != nil || rejected != 0 {
		t.Fatalf("apply: rejected %d, err %v", rejected, err)
	}
}

// change returns a copy of the pending change of one row with a new stamp and data patch.
func (p *syncPair) change(t *testing.T, table, rowUID string, wall int64, op domain.SyncOp, patch map[string]any) domain.SyncChange {
	t.Helper()
	for _, c := range p.changes {
		if c.Table == table && c.RowUID == rowUID {
			c.Op, c.HLC = op, domain.HLC{Wall: wall, Device: p.deviceA}.String()
			data := map[string]any{}
			for k, v := range c.Data {
				data[k] = v
			}
			for k, v := range patch {
				data[k] = v
			}
			c.Data = data
			return c
		}
	}
	t.Fatalf("no pending change for %s %s", table, rowUID)
	return domain.SyncChange{}
}

// dump renders every row of the query as one string, sorted.
func dump(t *testing.T, store *Store, query string) []string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		values := make([]any, len(cols))
		targets := make([]any, len(cols))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprint(values...))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// syncedState lists every synced row by uid, with references as uids.
var syncedState = []string{
	`SELECT uid, name, description FROM projects`,
	`SELECT uid, slug, name, goal_body, status FROM plans`,
	`SELECT uid, (SELECT uid FROM plans WHERE id = plan_id), name, position FROM plan_waves`,
	`SELECT t.uid, title, priority_id, bucket_id, depth, (SELECT uid FROM tasks WHERE id = t.parent_id),
  (SELECT uid FROM plans WHERE id = t.plan_id), (SELECT uid FROM plan_waves WHERE id = t.wave_id) FROM tasks t`,
	`SELECT uid, description FROM errors`,
	`SELECT uid, (SELECT uid FROM errors WHERE id = error_id), description, (SELECT uid FROM tasks WHERE id = task_id) FROM solutions`,
	`SELECT uid, entity_type, (SELECT uid FROM tasks WHERE id = entity_id), event_type, body, hlc FROM events`,
	`SELECT (SELECT uid FROM tasks WHERE id = task_id), (SELECT uid FROM tasks WHERE id = depends_on_task_id) FROM task_dependencies`,
	`SELECT (SELECT uid FROM tasks WHERE id = task_id), (SELECT name FROM tags WHERE id = tag_id) FROM task_tags`,
	`SELECT (SELECT uid FROM errors WHERE id = error_id), (SELECT name FROM tags WHERE id = tag_id) FROM error_tags`,
	`SELECT (SELECT uid FROM events WHERE id = event_id), (SELECT name FROM tags WHERE id = tag_id) FROM event_tags`,
}

func snapshot(t *testing.T, store *Store) [][]string {
	t.Helper()
	var out [][]string
	for _, query := range syncedState {
		out = append(out, dump(t, store, query))
	}
	out = append(out, dump(t, store, `SELECT tbl, row_uid, hlc, deleted, local FROM sync_rows`))
	return out
}

func assertCoded(t *testing.T, err error, code domain.ErrorCode) *domain.CodedError {
	t.Helper()
	var coded *domain.CodedError
	if !errors.As(err, &coded) || coded.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
	return coded
}

func TestApplyChangesRoundTrip(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	if got := dump(t, p.b, `SELECT uid FROM tasks`); len(got) != 0 {
		t.Fatalf("clone starts with tasks %v", got)
	}
	p.mustApply(t, p.changes, 7)

	want := snapshot(t, p.a)
	got := snapshot(t, p.b)
	for i, query := range syncedState {
		if !slices.Equal(want[i], got[i]) {
			t.Fatalf("%s\nA: %v\nB: %v", query, want[i], got[i])
		}
	}
	if n := len(dump(t, p.b, `SELECT uid FROM tasks WHERE parent_id IS NOT NULL`)); n != 1 {
		t.Fatalf("child tasks on B = %d", n)
	}
	if local := dump(t, p.b, `SELECT tbl FROM sync_rows WHERE local = 1`); len(local) != 0 {
		t.Fatalf("apply captured local rows: %v", local)
	}
	if cursor := mustScan[int64](t, p.b, `SELECT pull_cursor FROM sync_projects WHERE project_id = ?`, p.bProject.ID); cursor != 7 {
		t.Fatalf("cursor = %d", cursor)
	}
	if applying := mustScan[int](t, p.b, `SELECT applying FROM sync_identity`); applying != 0 {
		t.Fatal("applying flag left on")
	}
	clock := mustParseHLC(t, mustScan[string](t, p.b, `SELECT hlc FROM sync_identity`))
	if clock.Compare(p.maxHLC) <= 0 {
		t.Fatalf("clock %v not past incoming %v", clock, p.maxHLC)
	}
	root := mustScan[string](t, p.b, `SELECT root_path FROM projects WHERE id = ?`, p.bProject.ID)
	if root != p.bProject.RootPath {
		t.Fatalf("apply moved the root to %q", root)
	}
}

func TestApplyChangesOrdersParentsBeforeChildren(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	parent := p.change(t, "tasks", p.uids["parent"], soon(1), domain.SyncOpUpsert, nil)
	child := p.change(t, "tasks", p.uids["child"], 1e12, domain.SyncOpUpsert, nil)
	plan := p.change(t, "plans", p.uids["plan"], 1e12, domain.SyncOpUpsert, nil)
	wave := p.change(t, "plan_waves", p.uids["wave"], 1e12, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{child, parent, wave, plan}, 1)
	got := dump(t, p.b, `SELECT title, (SELECT title FROM tasks WHERE id = t.parent_id) FROM tasks t WHERE parent_id IS NOT NULL`)
	if !slices.Equal(got, []string{"childparent"}) {
		t.Fatalf("child = %v", got)
	}
}

func TestApplyChangesLastWriterWins(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	p.mustApply(t, p.changes, 1)
	title := func() string {
		return mustScan[string](t, p.b, `SELECT title FROM tasks WHERE uid = ?`, p.uids["child"])
	}

	p.mustApply(t, []domain.SyncChange{p.change(t, "tasks", p.uids["child"], 1, domain.SyncOpUpsert, map[string]any{"title": "stale"})}, 2)
	if title() != "child" {
		t.Fatalf("older change applied: %q", title())
	}
	p.mustApply(t, []domain.SyncChange{p.change(t, "tasks", p.uids["child"], soon(1), domain.SyncOpUpsert, map[string]any{"title": "fresh"})}, 3)
	if title() != "fresh" {
		t.Fatalf("newer change ignored: %q", title())
	}

	mustExec(t, p.b, `UPDATE tasks SET title = 'local' WHERE uid = ?`, p.uids["child"])
	p.mustApply(t, []domain.SyncChange{p.change(t, "tasks", p.uids["child"], 1e12, domain.SyncOpUpsert, map[string]any{"title": "older"})}, 4)
	if title() != "local" {
		t.Fatalf("older change beat a newer local edit: %q", title())
	}
	if local := mustScan[int](t, p.b, `SELECT local FROM sync_rows WHERE row_uid = ?`, p.uids["child"]); local != 1 {
		t.Fatal("local edit lost its pending flag")
	}
}

func TestApplyChangesDeleteWins(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	p.mustApply(t, p.changes, 1)
	remove := p.change(t, "tasks", p.uids["other"], soon(1), domain.SyncOpDelete, nil)
	remove.Data = nil
	p.mustApply(t, []domain.SyncChange{remove}, 2)
	if got := dump(t, p.b, `SELECT uid FROM tasks WHERE uid = '`+p.uids["other"]+`'`); len(got) != 0 {
		t.Fatalf("task survived delete: %v", got)
	}
	if got := dump(t, p.b, `SELECT task_id FROM task_dependencies`); len(got) != 0 {
		t.Fatalf("dependency survived delete: %v", got)
	}
	revive := p.change(t, "tasks", p.uids["other"], soon(2), domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{revive}, 3)
	if got := dump(t, p.b, `SELECT uid FROM tasks WHERE uid = '`+p.uids["other"]+`'`); len(got) != 0 {
		t.Fatalf("upsert resurrected a deleted task: %v", got)
	}
	if deleted := mustScan[int](t, p.b, `SELECT deleted FROM sync_rows WHERE row_uid = ?`, p.uids["other"]); deleted != 1 {
		t.Fatal("tombstone lost")
	}
}

func TestApplyChangesDeletesJoinRows(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	p.mustApply(t, p.changes, 1)
	rowUID := p.uids["child"] + "|urgent"
	p.mustApply(t, []domain.SyncChange{{Table: "task_tags", RowUID: rowUID, Op: domain.SyncOpDelete,
		HLC: domain.HLC{Wall: soon(1), Device: p.deviceA}.String(), Device: p.deviceA}}, 2)
	if got := dump(t, p.b, `SELECT task_id FROM task_tags`); len(got) != 0 {
		t.Fatalf("task tag survived: %v", got)
	}
}

func TestApplyChangesIsIdempotent(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	p.mustApply(t, p.changes, 1)
	first := snapshot(t, p.b)
	p.mustApply(t, p.changes, 1)
	second := snapshot(t, p.b)
	for i := range first {
		if !slices.Equal(first[i], second[i]) {
			t.Fatalf("re-apply changed state\nbefore: %v\nafter: %v", first[i], second[i])
		}
	}
}

func inboxSize(t *testing.T, p *syncPair) int {
	t.Helper()
	return len(dump(t, p.b, `SELECT row_uid FROM sync_inbox`))
}

func TestApplyChangesParksChangeWithMissingParent(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	p.mustApply(t, nil, 0)
	child := p.change(t, "tasks", p.uids["child"], 1e12, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{p.change(t, "plans", p.uids["plan"], 1e12, domain.SyncOpUpsert, nil), child}, 5)

	if got := dump(t, p.b, `SELECT uid FROM tasks`); len(got) != 0 {
		t.Fatalf("parked child was written: %v", got)
	}
	if inboxSize(t, p) != 1 {
		t.Fatalf("inbox = %d, want 1", inboxSize(t, p))
	}
	if cursor := mustScan[int64](t, p.b, `SELECT pull_cursor FROM sync_projects WHERE project_id = ?`, p.bProject.ID); cursor != 5 {
		t.Fatalf("cursor = %d", cursor)
	}
	if len(dump(t, p.b, `SELECT uid FROM plans`)) != 1 {
		t.Fatal("resolvable change was not applied")
	}

	parent := p.change(t, "tasks", p.uids["parent"], 1e12, domain.SyncOpUpsert, nil)
	wave := p.change(t, "plan_waves", p.uids["wave"], 1e12, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{parent, wave}, 6)
	got := dump(t, p.b, `SELECT title, (SELECT title FROM tasks WHERE id = t.parent_id) FROM tasks t WHERE parent_id IS NOT NULL`)
	if !slices.Equal(got, []string{"childparent"}) {
		t.Fatalf("child = %v", got)
	}
	if inboxSize(t, p) != 0 {
		t.Fatal("inbox not emptied")
	}
}

func TestApplyChangesDropsChildOfDeletedParent(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	p.mustApply(t, nil, 0)
	child := p.change(t, "tasks", p.uids["child"], 1e12, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{child}, 1)
	if inboxSize(t, p) != 1 {
		t.Fatal("child not parked")
	}
	gone := p.change(t, "tasks", p.uids["parent"], 1.5e12, domain.SyncOpDelete, nil)
	gone.Data = nil
	p.mustApply(t, []domain.SyncChange{gone}, 2)
	if inboxSize(t, p) != 0 {
		t.Fatal("inbox not emptied")
	}
	p.mustApply(t, []domain.SyncChange{child}, 3)
	if got := dump(t, p.b, `SELECT uid FROM tasks`); len(got) != 0 {
		t.Fatalf("child of a deleted parent was written: %v", got)
	}
	if inboxSize(t, p) != 0 {
		t.Fatal("dropped child was parked again")
	}
}

func TestApplyChangesNullsOptionalRefToDeletedRow(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	plan := p.change(t, "plans", p.uids["plan"], 1.5e12, domain.SyncOpDelete, nil)
	plan.Data = nil
	wave := p.change(t, "plan_waves", p.uids["wave"], 1.5e12, domain.SyncOpDelete, nil)
	wave.Data = nil
	p.mustApply(t, []domain.SyncChange{plan, wave}, 1)
	parent := p.change(t, "tasks", p.uids["parent"], 1e12, domain.SyncOpUpsert, nil)
	child := p.change(t, "tasks", p.uids["child"], 1e12, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{parent, child}, 2)
	got := dump(t, p.b, `SELECT title, plan_id IS NULL, wave_id IS NULL, parent_id IS NULL FROM tasks WHERE title = 'child'`)
	if !slices.Equal(got, []string{"child1 1 0"}) {
		t.Fatalf("child = %v", got)
	}
	if inboxSize(t, p) != 0 {
		t.Fatal("inbox not empty")
	}
}

func TestCreateClonedProject(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	if p.bProject.SyncScope != domain.SyncScopeSynced || p.bProject.Slug != p.aProject.Slug {
		t.Fatalf("clone = %+v", p.bProject)
	}
	if uid := mustScan[string](t, p.b, `SELECT uid FROM projects WHERE id = ?`, p.bProject.ID); uid != mustScan[string](t, p.a, `SELECT uid FROM projects WHERE id = ?`, p.aProject.ID) {
		t.Fatalf("clone uid = %s", uid)
	}
	if n := len(dump(t, p.b, `SELECT tbl FROM sync_rows`)); n != 0 {
		t.Fatalf("clone queued %d rows", n)
	}
	info := domain.SyncProjectInfo{UID: domain.NewUID(), Name: "Other", Slug: "other"}
	_, err := p.b.CreateClonedProject(p.ctx, info, p.bProject.RootPath)
	assertCoded(t, err, domain.ErrProjectPathConflict)
	info.Slug = p.bProject.Slug
	_, err = p.b.CreateClonedProject(p.ctx, info, t.TempDir())
	assertCoded(t, err, domain.ErrProjectAmbiguous)
	info.UID = "not-a-uid"
	_, err = p.b.CreateClonedProject(p.ctx, info, t.TempDir())
	assertCoded(t, err, domain.ErrValidation)
	info.UID, info.Slug = domain.NewUID(), "Not A Slug"
	_, err = p.b.CreateClonedProject(p.ctx, info, t.TempDir())
	assertCoded(t, err, domain.ErrValidation)
	if strings.Contains(fmt.Sprint(dump(t, p.b, `SELECT slug FROM projects`)), "other") {
		t.Fatal("rejected clone left a row")
	}
}

// attackPair is a pair whose clone also holds a local project with its own rows.
type attackPair struct {
	*syncPair
	local domain.Project
	l     map[string]string
}

func newAttackPair(t *testing.T) *attackPair {
	t.Helper()
	p := newSyncPair(t)
	local, err := p.b.UpsertProject(p.ctx, "Local", "local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &attackPair{syncPair: p, local: local, l: seedRowGraph(t, p.b, local.ID)}
	p.mustApply(t, p.changes, 1)
	return a
}

func (a *attackPair) stamp() string {
	return domain.HLC{Wall: soon(1), Device: a.deviceA}.String()
}

func (a *attackPair) upsert(table, rowUID string, data map[string]any) domain.SyncChange {
	return domain.SyncChange{Table: table, RowUID: rowUID, Op: domain.SyncOpUpsert, HLC: a.stamp(), Device: a.deviceA, Data: data}
}

func (a *attackPair) remove(table, rowUID string) domain.SyncChange {
	return domain.SyncChange{Table: table, RowUID: rowUID, Op: domain.SyncOpDelete, HLC: a.stamp(), Device: a.deviceA}
}

func TestApplyChangesNeverTouchesAnotherProject(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	var localProjectUID = mustScan[string](t, a.b, `SELECT uid FROM projects WHERE id = ?`, a.local.ID)
	task := func(title string) map[string]any {
		return map[string]any{"title": title, "bucket_id": 1.0, "priority_id": 2.0}
	}
	attacks := map[string]domain.SyncChange{
		"upsert task":        a.upsert("tasks", a.l["parent"], task("pwned")),
		"upsert plan":        a.upsert("plans", a.l["plan"], map[string]any{"slug": "p", "name": "pwned"}),
		"upsert error":       a.upsert("errors", a.l["error"], map[string]any{"description": "pwned"}),
		"upsert project":     a.upsert("projects", localProjectUID, map[string]any{"name": "pwned"}),
		"delete task":        a.remove("tasks", a.l["parent"]),
		"delete error":       a.remove("errors", a.l["error"]),
		"delete plan":        a.remove("plans", a.l["plan"]),
		"delete wave":        a.remove("plan_waves", a.l["wave"]),
		"delete solution":    a.remove("solutions", a.l["solution"]),
		"delete event":       a.remove("events", a.l["event"]),
		"delete task tag":    a.remove("task_tags", a.l["child"]+"|urgent"),
		"delete error tag":   a.remove("error_tags", a.l["error"]+"|urgent"),
		"delete event tag":   a.remove("event_tags", a.l["event"]+"|urgent"),
		"delete dependency":  a.remove("task_dependencies", a.l["child"]+"|"+a.l["other"]),
		"parent of a task":   a.upsert("tasks", string(domain.NewUID()), map[string]any{"title": "t", "bucket_id": 1.0, "priority_id": 2.0, "parent_uid": a.l["parent"]}),
		"plan of a task":     a.upsert("tasks", string(domain.NewUID()), map[string]any{"title": "t", "bucket_id": 1.0, "priority_id": 2.0, "plan_uid": a.l["plan"]}),
		"task of a solution": a.upsert("solutions", string(domain.NewUID()), map[string]any{"description": "s", "error_uid": a.uids["error"], "task_uid": a.l["parent"]}),
		"error of solution":  a.upsert("solutions", string(domain.NewUID()), map[string]any{"description": "s", "error_uid": a.l["error"]}),
		"entity of an event": a.upsert("events", string(domain.NewUID()), map[string]any{"entity_type": "task", "entity_uid": a.l["parent"], "event_type": "comment", "body": "b"}),
		"project of event":   a.upsert("events", string(domain.NewUID()), map[string]any{"entity_type": "project", "entity_uid": localProjectUID, "event_type": "comment", "body": "b"}),
		"plan of a wave":     a.upsert("plan_waves", string(domain.NewUID()), map[string]any{"name": "w", "position": 1.0, "plan_uid": a.l["plan"]}),
		"dependency":         a.upsert("task_dependencies", a.uids["parent"]+"|"+a.l["parent"], map[string]any{"task_uid": a.uids["parent"], "depends_on_task_uid": a.l["parent"]}),
		"task tag":           a.upsert("task_tags", a.l["parent"]+"|urgent", map[string]any{"task_uid": a.l["parent"], "tag_name": "urgent", "label": "Urgent"}),
		"event tag":          a.upsert("event_tags", a.l["event"]+"|urgent", map[string]any{"event_uid": a.l["event"], "tag_name": "urgent", "label": "Urgent"}),
	}
	for name, attack := range attacks {
		before := snapshot(t, a.b)
		rejected, err := a.apply(t, []domain.SyncChange{attack}, 2)
		if err != nil || rejected != 1 {
			t.Fatalf("%s: rejected %d, err %v", name, rejected, err)
		}
		after := snapshot(t, a.b)
		for i := range before {
			if !slices.Equal(before[i], after[i]) {
				t.Fatalf("%s changed the store\nbefore: %v\nafter: %v", name, before[i], after[i])
			}
		}
	}
	if title := mustScan[string](t, a.b, `SELECT title FROM tasks WHERE uid = ?`, a.l["parent"]); title != "parent" {
		t.Fatalf("local task renamed to %q", title)
	}
	if project := mustScan[int64](t, a.b, `SELECT project_id FROM tasks WHERE uid = ?`, a.l["parent"]); project != a.local.ID {
		t.Fatalf("local task moved to project %d", project)
	}
}

func TestApplyChangesRejectsAndContinues(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	plan := func(slug string) domain.SyncChange {
		return a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": slug, "name": slug})
	}
	projectUID := mustScan[string](t, a.b, `SELECT uid FROM projects WHERE id = ?`, a.bProject.ID)
	bad := map[string]domain.SyncChange{
		"table":      {Table: "tags", RowUID: "x", Op: domain.SyncOpUpsert, HLC: a.stamp(), Device: a.deviceA, Data: map[string]any{"a": "b"}},
		"op":         {Table: "plans", RowUID: "x", Op: "merge", HLC: a.stamp(), Device: a.deviceA},
		"hlc":        {Table: "plans", RowUID: "x", Op: domain.SyncOpUpsert, HLC: "nope", Device: a.deviceA, Data: map[string]any{"a": "b"}},
		"control":    a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "ctl", "name": "bad\x01name"}),
		"join key":   a.upsert("task_tags", a.uids["parent"]+"|other", map[string]any{"task_uid": a.uids["parent"], "tag_name": "urgent"}),
		"cycle":      a.upsert("tasks", a.uids["parent"], map[string]any{"title": "parent", "bucket_id": 1.0, "priority_id": 2.0, "parent_uid": a.uids["child"]}),
		"self":       a.upsert("tasks", a.uids["parent"], map[string]any{"title": "parent", "bucket_id": 1.0, "priority_id": 2.0, "parent_uid": a.uids["parent"]}),
		"empty name": a.upsert("projects", projectUID, map[string]any{"name": ""}),
		"name ctl":   a.upsert("projects", projectUID, map[string]any{"name": "a\nb"}),
		"nested":     a.upsert("plans", "x", map[string]any{"slug": []any{"a"}}),
		"join":       a.remove("task_tags", "no-separator"),
		"project":    a.remove("projects", string(domain.NewUID())),
		"foreign":    a.upsert("projects", string(domain.NewUID()), map[string]any{"name": "x"}),
		"state":      a.upsert("tasks", string(domain.NewUID()), map[string]any{"title": "t", "bucket_id": 1.0, "priority_id": 2.0, "state": "bogus"}),
		"not null":   a.upsert("tasks", string(domain.NewUID()), map[string]any{"title": nil, "bucket_id": 1.0, "priority_id": 2.0}),
		"check":      a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "c", "name": "c", "status": "bogus"}),
		"unique":     a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "p", "name": "dup"}),
		"entity":     a.upsert("events", string(domain.NewUID()), map[string]any{"entity_type": "nothing", "entity_uid": "x", "event_type": "comment", "body": "b"}),
		"no tag":     a.upsert("task_tags", a.uids["parent"]+"|", map[string]any{"task_uid": a.uids["parent"]}),
		"zero stamp": {Table: "plans", RowUID: "x", Op: domain.SyncOpUpsert, HLC: domain.HLC{}.String(), Device: a.deviceA, Data: map[string]any{"a": "b"}},
	}
	for name, change := range bad {
		first, second := plan("first-"+name), plan("second-"+name)
		before := len(dump(t, a.b, `SELECT uid FROM plans`))
		rejected, err := a.apply(t, []domain.SyncChange{first, change, second}, 40)
		if err != nil || rejected != 1 {
			t.Fatalf("%s: rejected %d, err %v", name, rejected, err)
		}
		if after := len(dump(t, a.b, `SELECT uid FROM plans`)); after != before+2 {
			t.Fatalf("%s: plans %d -> %d, the rest of the batch must apply", name, before, after)
		}
		if cursor := mustScan[int64](t, a.b, `SELECT pull_cursor FROM sync_projects WHERE project_id = ?`, a.bProject.ID); cursor != 40 {
			t.Fatalf("%s: cursor = %d", name, cursor)
		}
		if applying := mustScan[int](t, a.b, `SELECT applying FROM sync_identity`); applying != 0 {
			t.Fatalf("%s: applying flag left on", name)
		}
	}
}

func parkedTask(a *attackPair, wall int64, parent string) domain.SyncChange {
	change := a.upsert("tasks", string(domain.NewUID()), map[string]any{"title": "t", "bucket_id": 1.0, "priority_id": 2.0, "parent_uid": parent})
	change.HLC = domain.HLC{Wall: wall, Device: a.deviceA}.String()
	return change
}

func TestApplyChangesCapsTheInbox(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	missing := string(domain.NewUID())
	const extra = 5
	changes := make([]domain.SyncChange, maxSyncInbox+extra)
	for i := range changes {
		changes[i] = parkedTask(a, 1e12+int64(i), missing)
	}
	rejected, err := a.apply(t, changes, 9)
	if err != nil || rejected != extra {
		t.Fatalf("rejected %d, err %v", rejected, err)
	}
	if n := mustScan[int](t, a.b, `SELECT count(*) FROM sync_inbox WHERE project_id = ?`, a.bProject.ID); n != maxSyncInbox {
		t.Fatalf("inbox = %d, want %d", n, maxSyncInbox)
	}
	oldest := mustScan[string](t, a.b, `SELECT min(hlc) FROM sync_inbox`)
	if want := changes[extra].HLC; oldest != want {
		t.Fatalf("oldest parked = %s, want %s: the oldest rows must be dropped", oldest, want)
	}
}

func TestApplyChangesRetriesOnlyWhatArrived(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	parent := string(domain.NewUID())
	child := parkedTask(a, 1e12, parent)
	a.mustApply(t, []domain.SyncChange{child}, 10)
	if inboxSize(t, a.syncPair) != 1 {
		t.Fatal("child not parked")
	}
	raw := mustScan[string](t, a.b, `SELECT change FROM sync_inbox`)
	// An unreadable parked row proves the next batch never touches it.
	mustExec(t, a.b, `UPDATE sync_inbox SET change = 'unreadable'`)
	a.mustApply(t, []domain.SyncChange{a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "unrelated", "name": "u"})}, 11)
	if inboxSize(t, a.syncPair) != 1 {
		t.Fatal("unrelated batch disturbed the inbox")
	}
	mustExec(t, a.b, `UPDATE sync_inbox SET change = ?`, raw)
	a.mustApply(t, []domain.SyncChange{a.upsert("tasks", parent, map[string]any{"title": "parent", "bucket_id": 1.0, "priority_id": 2.0})}, 12)
	if got := dump(t, a.b, `SELECT title FROM tasks WHERE uid = '`+child.RowUID+`'`); len(got) != 1 || inboxSize(t, a.syncPair) != 0 {
		t.Fatalf("child = %v, inbox = %d", got, inboxSize(t, a.syncPair))
	}
}

func TestApplyChangesWakesChainsOfParkedChanges(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	root := string(domain.NewUID())
	middle := parkedTask(a, 1e12, root)
	leaf := parkedTask(a, 1.1e12, middle.RowUID)
	a.mustApply(t, []domain.SyncChange{leaf, middle}, 10)
	if inboxSize(t, a.syncPair) != 2 {
		t.Fatalf("inbox = %d, want 2", inboxSize(t, a.syncPair))
	}
	a.mustApply(t, []domain.SyncChange{a.upsert("tasks", root, map[string]any{"title": "root", "bucket_id": 1.0, "priority_id": 2.0})}, 11)
	if got := dump(t, a.b, `SELECT uid FROM tasks WHERE uid IN ('`+middle.RowUID+`', '`+leaf.RowUID+`')`); len(got) != 2 || inboxSize(t, a.syncPair) != 0 {
		t.Fatalf("chain = %v, inbox = %d", got, inboxSize(t, a.syncPair))
	}
}

func TestApplyChangesDeleteBeatsNewerLocalEdit(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	mustExec(t, a.b, `UPDATE tasks SET title = 'edited' WHERE uid = ?`, a.uids["other"])
	remove := a.remove("tasks", a.uids["other"])
	remove.HLC = domain.HLC{Wall: 1.5e12, Device: a.deviceA}.String()
	a.mustApply(t, []domain.SyncChange{remove}, 3)
	if got := dump(t, a.b, `SELECT uid FROM tasks WHERE uid = '`+a.uids["other"]+`'`); len(got) != 0 {
		t.Fatalf("an older delete lost to a local edit: %v", got)
	}
}

func TestApplyChangesMapsProjectEvents(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	projectUID := mustScan[string](t, p.a, `SELECT uid FROM projects WHERE id = ?`, p.aProject.ID)
	eventUID := string(domain.NewUID())
	mustExec(t, p.a, `INSERT INTO events(entity_type, entity_id, project_id, event_type, body, uid) VALUES ('project', ?, ?, 'comment', 'about the project', ?)`,
		p.aProject.ID, p.aProject.ID, eventUID)
	pending, err := p.a.PendingChanges(p.ctx, p.aProject.ID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var pushed *domain.SyncChange
	for i, c := range pending {
		if c.Table == "events" && c.RowUID == eventUID {
			pushed = &pending[i]
		}
	}
	if pushed == nil || pushed.Data["entity_uid"] != projectUID {
		t.Fatalf("pushed event = %+v, want entity_uid %s", pushed, projectUID)
	}
	p.mustApply(t, wireRoundTrip(t, []domain.SyncChange{*pushed}), 1)
	got := mustScan[int64](t, p.b, `SELECT entity_id FROM events WHERE uid = ? AND entity_type = 'project'`, eventUID)
	if got != p.bProject.ID {
		t.Fatalf("entity_id = %d, want the clone %d", got, p.bProject.ID)
	}
}

func TestApplyChangesJoinRowsReturnAfterDelete(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	rowUID := a.uids["child"] + "|urgent"
	tagged := func() int {
		return len(dump(t, a.b, `SELECT task_id FROM task_tags WHERE task_id = (SELECT id FROM tasks WHERE uid = '`+a.uids["child"]+`')`))
	}
	at := func(minutes int64, op domain.SyncOp) domain.SyncChange {
		change := a.change(t, "task_tags", rowUID, soon(minutes), op, nil)
		if op == domain.SyncOpDelete {
			change.Data = nil
		}
		return change
	}
	a.mustApply(t, []domain.SyncChange{at(2, domain.SyncOpDelete)}, 20)
	if tagged() != 0 {
		t.Fatal("tag survived delete")
	}
	a.mustApply(t, []domain.SyncChange{at(1, domain.SyncOpUpsert)}, 21)
	if tagged() != 0 {
		t.Fatal("older upsert beat the tombstone")
	}
	a.mustApply(t, []domain.SyncChange{at(3, domain.SyncOpUpsert)}, 22)
	if tagged() != 1 {
		t.Fatal("later upsert did not re-add the tag")
	}
	a.mustApply(t, []domain.SyncChange{at(4, domain.SyncOpDelete)}, 23)
	if tagged() != 0 {
		t.Fatal("later tombstone lost to the older upsert")
	}
}

func TestDeleteIsFinalOnlyForEntityRows(t *testing.T) {
	t.Parallel()
	for _, table := range domain.SyncTables {
		join := slices.Contains([]string{"task_dependencies", "task_tags", "error_tags", "event_tags"}, table)
		if deleteIsFinal(table) == join {
			t.Fatalf("deleteIsFinal(%s) = %v", table, !join)
		}
	}
}

func TestApplyChangesParksFutureStampsUntilTheClockCatchesUp(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	clockBefore := mustScan[string](t, a.b, `SELECT hlc FROM sync_identity`)
	future := a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "later", "name": "later"})
	future.HLC = domain.HLC{Wall: time.Now().Add(2 * domain.MaxHLCDrift).UnixMilli(), Device: a.deviceA}.String()
	a.mustApply(t, []domain.SyncChange{future}, 30)
	if inboxSize(t, a.syncPair) != 1 || len(dump(t, a.b, `SELECT uid FROM plans WHERE uid = '`+future.RowUID+`'`)) != 0 {
		t.Fatal("future change not parked")
	}
	if clock := mustScan[string](t, a.b, `SELECT hlc FROM sync_identity`); clock != clockBefore {
		t.Fatalf("clock moved by a parked change: %s -> %s", clockBefore, clock)
	}
	a.mustApply(t, nil, 31)
	if inboxSize(t, a.syncPair) != 1 {
		t.Fatal("future change left the inbox early")
	}
	arrived := future
	arrived.HLC = domain.HLC{Wall: time.Now().UnixMilli(), Device: a.deviceA}.String()
	raw, err := json.Marshal(arrived)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, a.b, `UPDATE sync_inbox SET hlc = ?, change = ?`, arrived.HLC, string(raw))
	a.mustApply(t, nil, 32)
	if inboxSize(t, a.syncPair) != 0 || len(dump(t, a.b, `SELECT uid FROM plans WHERE uid = '`+future.RowUID+`'`)) != 1 {
		t.Fatal("parked change not applied once the clock caught up")
	}
}

func TestApplyChangesClockObservesOnlyAppliedChanges(t *testing.T) {
	t.Parallel()
	a := newAttackPair(t)
	wall := time.Now().Add(10 * time.Hour).UnixMilli()
	waiting := parkedTask(a, wall, string(domain.NewUID()))
	a.mustApply(t, []domain.SyncChange{waiting}, 33)
	invalid := a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "bad", "name": "bad\x00"})
	invalid.HLC = domain.HLC{Wall: wall, Device: a.deviceA}.String()
	if rejected, err := a.apply(t, []domain.SyncChange{invalid}, 34); err != nil || rejected != 1 {
		t.Fatalf("rejected %d, err %v", rejected, err)
	}
	if clock := mustScan[string](t, a.b, `SELECT hlc FROM sync_identity`); clock != "" && mustParseHLC(t, clock).Wall >= wall {
		t.Fatalf("clock %s observed a change that did not apply", clock)
	}
	good := a.upsert("plans", string(domain.NewUID()), map[string]any{"slug": "good", "name": "good"})
	good.HLC = domain.HLC{Wall: wall, Device: a.deviceA}.String()
	a.mustApply(t, []domain.SyncChange{good}, 35)
	if clock := mustParseHLC(t, mustScan[string](t, a.b, `SELECT hlc FROM sync_identity`)); clock.Wall < wall {
		t.Fatalf("clock %v did not observe the applied change", clock)
	}
}
