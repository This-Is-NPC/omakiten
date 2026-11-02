package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	changes, err := a.PendingChanges(ctx, project.ID, 1000)
	if err != nil {
		t.Fatal(err)
	}
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

func mustParseHLC(t *testing.T, s string) domain.HLC {
	t.Helper()
	h, err := domain.ParseHLC(s)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (p *syncPair) apply(t *testing.T, changes []domain.SyncChange, cursor int64) error {
	t.Helper()
	return p.b.ApplyChanges(p.ctx, p.bProject.ID, changes, cursor)
}

func (p *syncPair) mustApply(t *testing.T, changes []domain.SyncChange, cursor int64) {
	t.Helper()
	if err := p.apply(t, changes, cursor); err != nil {
		t.Fatal(err)
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
	parent := p.change(t, "tasks", p.uids["parent"], 9e14, domain.SyncOpUpsert, nil)
	child := p.change(t, "tasks", p.uids["child"], 1e14, domain.SyncOpUpsert, nil)
	plan := p.change(t, "plans", p.uids["plan"], 1e14, domain.SyncOpUpsert, nil)
	wave := p.change(t, "plan_waves", p.uids["wave"], 1e14, domain.SyncOpUpsert, nil)
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
	p.mustApply(t, []domain.SyncChange{p.change(t, "tasks", p.uids["child"], 9e14, domain.SyncOpUpsert, map[string]any{"title": "fresh"})}, 3)
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
	remove := p.change(t, "tasks", p.uids["other"], 9e14, domain.SyncOpDelete, nil)
	remove.Data = nil
	p.mustApply(t, []domain.SyncChange{remove}, 2)
	if got := dump(t, p.b, `SELECT uid FROM tasks WHERE uid = '`+p.uids["other"]+`'`); len(got) != 0 {
		t.Fatalf("task survived delete: %v", got)
	}
	if got := dump(t, p.b, `SELECT task_id FROM task_dependencies`); len(got) != 0 {
		t.Fatalf("dependency survived delete: %v", got)
	}
	revive := p.change(t, "tasks", p.uids["other"], 9.5e14, domain.SyncOpUpsert, nil)
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
		HLC: domain.HLC{Wall: 9e14, Device: p.deviceA}.String(), Device: p.deviceA}}, 2)
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
	child := p.change(t, "tasks", p.uids["child"], 1e14, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{p.change(t, "plans", p.uids["plan"], 1e14, domain.SyncOpUpsert, nil), child}, 5)

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

	parent := p.change(t, "tasks", p.uids["parent"], 1e14, domain.SyncOpUpsert, nil)
	wave := p.change(t, "plan_waves", p.uids["wave"], 1e14, domain.SyncOpUpsert, nil)
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
	child := p.change(t, "tasks", p.uids["child"], 1e14, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{child}, 1)
	if inboxSize(t, p) != 1 {
		t.Fatal("child not parked")
	}
	gone := p.change(t, "tasks", p.uids["parent"], 2e14, domain.SyncOpDelete, nil)
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
	plan := p.change(t, "plans", p.uids["plan"], 2e14, domain.SyncOpDelete, nil)
	plan.Data = nil
	wave := p.change(t, "plan_waves", p.uids["wave"], 2e14, domain.SyncOpDelete, nil)
	wave.Data = nil
	p.mustApply(t, []domain.SyncChange{plan, wave}, 1)
	parent := p.change(t, "tasks", p.uids["parent"], 1e14, domain.SyncOpUpsert, nil)
	child := p.change(t, "tasks", p.uids["child"], 1e14, domain.SyncOpUpsert, nil)
	p.mustApply(t, []domain.SyncChange{parent, child}, 2)
	got := dump(t, p.b, `SELECT title, plan_id IS NULL, wave_id IS NULL, parent_id IS NULL FROM tasks WHERE title = 'child'`)
	if !slices.Equal(got, []string{"child1 1 0"}) {
		t.Fatalf("child = %v", got)
	}
	if inboxSize(t, p) != 0 {
		t.Fatal("inbox not empty")
	}
}

func TestApplyChangesRejectsInvalidChanges(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	good := p.change(t, "plans", p.uids["plan"], 1e14, domain.SyncOpUpsert, nil)
	for name, bad := range map[string]domain.SyncChange{
		"table": {Table: "tags", RowUID: "x", Op: domain.SyncOpUpsert, HLC: good.HLC},
		"op":    {Table: "plans", RowUID: "x", Op: "merge", HLC: good.HLC},
		"hlc":   {Table: "plans", RowUID: "x", Op: domain.SyncOpUpsert, HLC: "nope"},
		"join":  {Table: "task_tags", RowUID: "no-separator", Op: domain.SyncOpDelete, HLC: good.HLC},
	} {
		assertCoded(t, p.apply(t, []domain.SyncChange{good, bad}, 1), domain.ErrValidation)
		if n := len(dump(t, p.b, `SELECT uid FROM plans`)); n != 0 {
			t.Fatalf("%s: invalid batch wrote %d plans", name, n)
		}
	}
}

func TestApplyChangesRefusesAnotherProjectsRow(t *testing.T) {
	t.Parallel()
	p := newSyncPair(t)
	other := domain.SyncChange{Table: "projects", RowUID: string(domain.NewUID()), Op: domain.SyncOpUpsert,
		HLC: domain.HLC{Wall: 1e14, Device: p.deviceA}.String(), Data: map[string]any{"name": "x"}}
	assertCoded(t, p.apply(t, []domain.SyncChange{other}, 1), domain.ErrValidation)
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
	if strings.Contains(fmt.Sprint(dump(t, p.b, `SELECT slug FROM projects`)), "other") {
		t.Fatal("rejected clone left a row")
	}
}
