package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func openSyncStore(t *testing.T) (*Store, domain.Project) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project, err := store.UpsertProject(ctx, "Example", "example", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store, project
}

func mustExec(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func mustScan[T any](t *testing.T, store *Store, query string, args ...any) T {
	t.Helper()
	var v T
	if err := store.db.QueryRowContext(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func setScope(t *testing.T, store *Store, projectID int64, scope domain.SyncScope) {
	t.Helper()
	if _, err := store.UpdateProject(context.Background(), projectID, domain.ProjectPatch{SyncScope: &scope}); err != nil {
		t.Fatal(err)
	}
}

// seedRowGraph inserts one row in every synced table except projects and
// returns the uid of each by name.
func seedRowGraph(t *testing.T, store *Store, projectID int64) map[string]string {
	t.Helper()
	uids := map[string]string{}
	for _, name := range []string{"plan", "wave", "parent", "child", "other", "error", "solution", "event"} {
		uids[name] = string(domain.NewUID())
	}
	mustExec(t, store, `INSERT OR IGNORE INTO tags(name, label) VALUES ('urgent', 'Urgent')`)
	mustExec(t, store, `INSERT INTO plans(project_id, slug, name, uid) VALUES (?, 'p', 'Plan', ?)`, projectID, uids["plan"])
	planID := mustScan[int64](t, store, `SELECT id FROM plans WHERE uid = ?`, uids["plan"])
	mustExec(t, store, `INSERT INTO plan_waves(plan_id, name, position, uid) VALUES (?, 'W', 1, ?)`, planID, uids["wave"])
	waveID := mustScan[int64](t, store, `SELECT id FROM plan_waves WHERE uid = ?`, uids["wave"])
	mustExec(t, store, `INSERT INTO tasks(project_id, bucket_id, title, priority_id, uid) VALUES (?, 1, 'parent', 2, ?)`, projectID, uids["parent"])
	parentID := mustScan[int64](t, store, `SELECT id FROM tasks WHERE uid = ?`, uids["parent"])
	mustExec(t, store, `INSERT INTO tasks(project_id, bucket_id, title, priority_id, parent_id, plan_id, wave_id, depth, uid) VALUES (?, 1, 'child', 2, ?, ?, ?, 1, ?)`,
		projectID, parentID, planID, waveID, uids["child"])
	mustExec(t, store, `INSERT INTO tasks(project_id, bucket_id, title, priority_id, uid) VALUES (?, 1, 'other', 2, ?)`, projectID, uids["other"])
	childID := mustScan[int64](t, store, `SELECT id FROM tasks WHERE uid = ?`, uids["child"])
	otherID := mustScan[int64](t, store, `SELECT id FROM tasks WHERE uid = ?`, uids["other"])
	mustExec(t, store, `INSERT INTO task_dependencies(project_id, task_id, depends_on_task_id) VALUES (?, ?, ?)`, projectID, childID, otherID)
	mustExec(t, store, `INSERT INTO task_tags(project_id, task_id, tag_id) SELECT ?, ?, id FROM tags WHERE name = 'urgent'`, projectID, childID)
	mustExec(t, store, `INSERT INTO errors(description, project_id, uid) VALUES ('boom', ?, ?)`, projectID, uids["error"])
	errorID := mustScan[int64](t, store, `SELECT id FROM errors WHERE uid = ?`, uids["error"])
	mustExec(t, store, `INSERT INTO solutions(error_id, description, task_id, uid) VALUES (?, 'fix', ?, ?)`, errorID, childID, uids["solution"])
	mustExec(t, store, `INSERT INTO error_tags(error_id, tag_id) SELECT ?, id FROM tags WHERE name = 'urgent'`, errorID)
	mustExec(t, store, `INSERT INTO events(entity_type, entity_id, project_id, event_type, body, uid) VALUES ('task', ?, ?, 'comment', 'hi', ?)`, childID, projectID, uids["event"])
	eventID := mustScan[int64](t, store, `SELECT id FROM events WHERE uid = ?`, uids["event"])
	mustExec(t, store, `INSERT INTO event_tags(event_id, tag_id) SELECT ?, id FROM tags WHERE name = 'urgent'`, eventID)
	return uids
}

// capturedRows maps "table/row_uid" to the deleted flag of every sync_rows entry.
func capturedRows(t *testing.T, store *Store) map[string]int {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), `SELECT tbl, row_uid, deleted FROM sync_rows`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]int{}
	for rows.Next() {
		var tbl, uid string
		var deleted int
		if err := rows.Scan(&tbl, &uid, &deleted); err != nil {
			t.Fatal(err)
		}
		got[tbl+"/"+uid] = deleted
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func capturedTables(rows map[string]int) []string {
	var tables []string
	for key := range rows {
		table, _, _ := strings.Cut(key, "/")
		if !slices.Contains(tables, table) {
			tables = append(tables, table)
		}
	}
	slices.Sort(tables)
	return tables
}

func sortedSyncTables() []string {
	return slices.Sorted(slices.Values(domain.SyncTables))
}

func TestSyncRulesCoverEverySyncTable(t *testing.T) {
	t.Parallel()
	if len(syncRules) != len(domain.SyncTables) {
		t.Fatalf("%d rules for %d tables", len(syncRules), len(domain.SyncTables))
	}
	for _, table := range domain.SyncTables {
		if _, ok := syncRules[table]; !ok {
			t.Fatalf("no rule for %s", table)
		}
	}
}

func TestHLCTickFunctionParsesAndIncreases(t *testing.T) {
	t.Parallel()
	store, _ := openSyncStore(t)
	device := string(domain.NewUID())
	last := ""
	for range 50 {
		next := mustScan[string](t, store, `SELECT okt_hlc_tick(?, ?)`, last, device)
		parsed, err := domain.ParseHLC(next)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Device != domain.UID(device) {
			t.Fatalf("device = %q", parsed.Device)
		}
		if next <= last {
			t.Fatalf("%q not after %q", next, last)
		}
		last = next
	}
}

func TestCaptureRecordsEveryTableOfSyncedProject(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	mustExec(t, store, `UPDATE projects SET description = 'edited' WHERE id = ?`, project.ID)
	uids := seedRowGraph(t, store, project.ID)

	got := capturedRows(t, store)
	if want := sortedSyncTables(); !reflect.DeepEqual(capturedTables(got), want) {
		t.Fatalf("captured tables = %v, want %v", capturedTables(got), want)
	}
	for key, deleted := range got {
		if deleted != 0 {
			t.Fatalf("%s deleted after insert", key)
		}
	}
	for _, key := range []string{
		"tasks/" + uids["child"], "plan_waves/" + uids["wave"], "solutions/" + uids["solution"],
		"task_dependencies/" + uids["child"] + "|" + uids["other"],
		"task_tags/" + uids["child"] + "|urgent", "error_tags/" + uids["error"] + "|urgent", "event_tags/" + uids["event"] + "|urgent",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %s in %v", key, got)
		}
	}
	assertEveryRowStamped(t, store)
}

func assertEveryRowStamped(t *testing.T, store *Store) {
	t.Helper()
	if n := mustScan[int](t, store, `SELECT COUNT(*) FROM sync_rows WHERE hlc IS NULL OR local != 1`); n != 0 {
		t.Fatalf("%d sync_rows without hlc or not local", n)
	}
}

func TestCaptureUpdatesRestampEveryMutableTable(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	projectUID := mustScan[string](t, store, `SELECT uid FROM projects WHERE id = ?`, project.ID)
	updates := map[string]string{
		"plans/" + uids["plan"]:         `UPDATE plans SET name = 'n2' WHERE uid = '` + uids["plan"] + `'`,
		"plan_waves/" + uids["wave"]:    `UPDATE plan_waves SET name = 'n2' WHERE uid = '` + uids["wave"] + `'`,
		"tasks/" + uids["child"]:        `UPDATE tasks SET title = 't2' WHERE uid = '` + uids["child"] + `'`,
		"errors/" + uids["error"]:       `UPDATE errors SET context = 'c2' WHERE uid = '` + uids["error"] + `'`,
		"solutions/" + uids["solution"]: `UPDATE solutions SET likes = 3 WHERE uid = '` + uids["solution"] + `'`,
		"events/" + uids["event"]:       `UPDATE events SET pinned = 1 WHERE uid = '` + uids["event"] + `'`,
		"projects/" + projectUID:        `UPDATE projects SET name = 'Renamed' WHERE uid = '` + projectUID + `'`,
	}
	for key, statement := range updates {
		table, uid, _ := strings.Cut(key, "/")
		before := mustScan[string](t, store, `SELECT hlc FROM sync_rows WHERE tbl = ? AND row_uid = ?`, table, uid)
		mustExec(t, store, statement)
		after := mustScan[string](t, store, `SELECT hlc FROM sync_rows WHERE tbl = ? AND row_uid = ?`, table, uid)
		if after <= before {
			t.Fatalf("%s hlc %q not after %q", key, after, before)
		}
	}
}

func TestCaptureRecordsDeletes(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	for _, statement := range []string{
		`DELETE FROM event_tags`, `DELETE FROM error_tags`, `DELETE FROM task_tags`, `DELETE FROM task_dependencies`,
		`DELETE FROM events`, `DELETE FROM solutions`, `DELETE FROM errors`, `DELETE FROM tasks WHERE uid = '` + uids["other"] + `'`,
		`DELETE FROM plan_waves`,
	} {
		mustExec(t, store, statement)
	}
	got := capturedRows(t, store)
	for _, key := range []string{
		"event_tags/" + uids["event"] + "|urgent", "error_tags/" + uids["error"] + "|urgent",
		"task_tags/" + uids["child"] + "|urgent", "task_dependencies/" + uids["child"] + "|" + uids["other"],
		"events/" + uids["event"], "solutions/" + uids["solution"], "errors/" + uids["error"],
		"tasks/" + uids["other"], "plan_waves/" + uids["wave"],
	} {
		if got[key] != 1 {
			t.Fatalf("%s deleted = %d, want 1", key, got[key])
		}
	}
}

func TestCascadeDeleteRecordsParentTombstoneOnly(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	mustExec(t, store, `DELETE FROM tasks WHERE uid = ?`, uids["child"])
	got := capturedRows(t, store)
	if got["tasks/"+uids["child"]] != 1 {
		t.Fatal("task tombstone missing")
	}
	if got["task_tags/"+uids["child"]+"|urgent"] != 0 || got["task_dependencies/"+uids["child"]+"|"+uids["other"]] != 0 {
		t.Fatal("join rows of a deleted task were tombstoned")
	}
}

func TestProjectDeleteIsNeverCaptured(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	seedRowGraph(t, store, project.ID)
	mustExec(t, store, `DELETE FROM projects WHERE id = ?`, project.ID)
	if n := mustScan[int](t, store, `SELECT COUNT(*) FROM sync_rows`); n != 0 {
		t.Fatalf("%d sync_rows survive project deletion", n)
	}
}

func TestLocalProjectCapturesNothing(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	seedRowGraph(t, store, project.ID)
	mustExec(t, store, `UPDATE projects SET name = 'Renamed' WHERE id = ?`, project.ID)
	if got := capturedRows(t, store); len(got) != 0 {
		t.Fatalf("captured %v", got)
	}
}

func TestApplyingFlagSuspendsCapture(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	mustExec(t, store, `DELETE FROM sync_rows`)
	mustExec(t, store, `UPDATE sync_identity SET applying = 1`)
	seedRowGraph(t, store, project.ID)
	mustExec(t, store, `UPDATE projects SET name = 'Renamed' WHERE id = ?`, project.ID)
	if got := capturedRows(t, store); len(got) != 0 {
		t.Fatalf("captured %v while applying", got)
	}
}

func TestSwitchToSyncedEnqueuesExistingRows(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	seedRowGraph(t, store, project.ID)
	if got := capturedRows(t, store); len(got) != 0 {
		t.Fatalf("captured %v while local", got)
	}
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	got := capturedRows(t, store)
	if want := sortedSyncTables(); !reflect.DeepEqual(capturedTables(got), want) {
		t.Fatalf("enqueued tables = %v, want %v", capturedTables(got), want)
	}
	if len(got) != 13 {
		t.Fatalf("enqueued %d rows, want 13: %v", len(got), got)
	}
	assertEveryRowStamped(t, store)

	mustExec(t, store, `DELETE FROM sync_rows`)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	if got := capturedRows(t, store); len(got) != 0 {
		t.Fatalf("re-saving the synced scope enqueued %v", got)
	}
	setScope(t, store, project.ID, domain.SyncScopeLocal)
	mustExec(t, store, `UPDATE tasks SET title = 'quiet'`)
	if got := capturedRows(t, store); len(got) != 0 {
		t.Fatalf("captured %v after switching back to local", got)
	}
}

func pending(t *testing.T, store *Store, projectID int64) map[string]domain.SyncChange {
	t.Helper()
	changes, err := store.PendingChanges(context.Background(), projectID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]domain.SyncChange{}
	for _, change := range changes {
		byKey[change.Table+"/"+change.RowUID] = change
	}
	return byKey
}

func TestPendingChangesTranslateForeignKeys(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	got := pending(t, store, project.ID)
	device, _, err := store.SyncIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	child := got["tasks/"+uids["child"]]
	if child.Op != domain.SyncOpUpsert || child.Device != device || child.Data["title"] != "child" {
		t.Fatalf("child = %+v", child)
	}
	for key, want := range map[string]any{"parent_uid": uids["parent"], "plan_uid": uids["plan"], "wave_uid": uids["wave"], "priority_id": int64(2)} {
		if child.Data[key] != want {
			t.Fatalf("child.%s = %v, want %v", key, child.Data[key], want)
		}
	}
	for _, key := range []string{"id", "uid", "project_id", "parent_id", "plan_id", "wave_id"} {
		if _, ok := child.Data[key]; ok {
			t.Fatalf("child.Data carries %s", key)
		}
	}
	assertData(t, got["plan_waves/"+uids["wave"]], map[string]any{"plan_uid": uids["plan"]})
	assertData(t, got["solutions/"+uids["solution"]], map[string]any{"error_uid": uids["error"], "task_uid": uids["child"]})
	assertData(t, got["events/"+uids["event"]], map[string]any{"entity_uid": uids["child"]})
	assertData(t, got["task_dependencies/"+uids["child"]+"|"+uids["other"]], map[string]any{"task_uid": uids["child"], "depends_on_task_uid": uids["other"]})
	assertData(t, got["task_tags/"+uids["child"]+"|urgent"], map[string]any{"task_uid": uids["child"], "tag_name": "urgent", "label": "Urgent"})
	assertData(t, got["error_tags/"+uids["error"]+"|urgent"], map[string]any{"error_uid": uids["error"], "tag_name": "urgent", "label": "Urgent"})
	assertData(t, got["event_tags/"+uids["event"]+"|urgent"], map[string]any{"event_uid": uids["event"], "tag_name": "urgent", "label": "Urgent"})

	projectChange := got["projects/"+mustScan[string](t, store, `SELECT uid FROM projects WHERE id = ?`, project.ID)]
	if projectChange.Data["name"] != "Example" {
		t.Fatalf("project = %+v", projectChange)
	}
	for _, key := range []string{"root_path", "sync_scope", "archived_at"} {
		if _, ok := projectChange.Data[key]; ok {
			t.Fatalf("project.Data carries local-only %s", key)
		}
	}
}

func assertData(t *testing.T, change domain.SyncChange, want map[string]any) {
	t.Helper()
	if change.Op != domain.SyncOpUpsert {
		t.Fatalf("%s/%s = %+v", change.Table, change.RowUID, change)
	}
	for key, value := range want {
		if change.Data[key] != value {
			t.Fatalf("%s.%s = %v, want %v", change.Table, key, change.Data[key], value)
		}
	}
}

func TestPendingChangesCoalesceAndOrderByHLC(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	mustExec(t, store, `DELETE FROM sync_rows`)
	mustExec(t, store, `INSERT INTO tasks(project_id, bucket_id, title, priority_id, uid) VALUES (?, 1, 'a', 2, ?), (?, 1, 'b', 2, ?)`,
		project.ID, string(domain.NewUID()), project.ID, string(domain.NewUID()))
	mustExec(t, store, `UPDATE tasks SET title = 'a2' WHERE title = 'a'`)
	mustExec(t, store, `UPDATE tasks SET title = 'a3' WHERE title = 'a2'`)

	changes, err := store.PendingChanges(context.Background(), project.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("%d changes, want 2 (coalesced): %+v", len(changes), changes)
	}
	if changes[0].HLC >= changes[1].HLC {
		t.Fatalf("not ordered by hlc: %s, %s", changes[0].HLC, changes[1].HLC)
	}
	if changes[1].Data["title"] != "a3" {
		t.Fatalf("latest state = %v, want a3", changes[1].Data["title"])
	}
	limited, err := store.PendingChanges(context.Background(), project.ID, 1)
	if err != nil || len(limited) != 1 || limited[0].RowUID != changes[0].RowUID {
		t.Fatalf("limit 1 = %+v, %v", limited, err)
	}
}

func TestMarkPushedFiltersPushedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	seedRowGraph(t, store, project.ID)
	changes, err := store.PendingChanges(ctx, project.ID, 1000)
	if err != nil || len(changes) == 0 {
		t.Fatalf("pending = %d, %v", len(changes), err)
	}
	last := changes[len(changes)-1].HLC
	if err := store.MarkPushed(ctx, project.ID, last); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPushed(ctx, project.ID, changes[0].HLC); err != nil {
		t.Fatal(err)
	}
	if left, err := store.PendingChanges(ctx, project.ID, 1000); err != nil || len(left) != 0 {
		t.Fatalf("after push: %d pending, %v", len(left), err)
	}
	mustExec(t, store, `UPDATE tasks SET title = 'again' WHERE title = 'other'`)
	left, err := store.PendingChanges(ctx, project.ID, 1000)
	if err != nil || len(left) != 1 || left[0].Data["title"] != "again" {
		t.Fatalf("after edit: %+v, %v", left, err)
	}
	projects, err := store.SyncedProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0].PushedHLC != last {
		t.Fatalf("synced projects = %+v, %v", projects, err)
	}
}

func TestPendingChangesReportDeletes(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	mustExec(t, store, `DELETE FROM tasks WHERE uid = ?`, uids["other"])
	if got := pending(t, store, project.ID)["tasks/"+uids["other"]]; got.Op != domain.SyncOpDelete || got.Data != nil {
		t.Fatalf("deleted task = %+v", got)
	}
	// A tombstone-less row whose source vanished (applied behind the triggers) still reports a delete.
	mustExec(t, store, `UPDATE sync_identity SET applying = 1`)
	mustExec(t, store, `DELETE FROM errors WHERE uid = ?`, uids["error"])
	if got := pending(t, store, project.ID)["errors/"+uids["error"]]; got.Op != domain.SyncOpDelete {
		t.Fatalf("vanished error = %+v", got)
	}
}

func TestSyncedProjectsListsOnlySynced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := openSyncStore(t)
	if projects, err := store.SyncedProjects(ctx); err != nil || len(projects) != 0 {
		t.Fatalf("local project listed: %+v, %v", projects, err)
	}
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	mustExec(t, store, `INSERT INTO sync_projects(project_id, pull_cursor) VALUES (?, 7)`, project.ID)
	projects, err := store.SyncedProjects(ctx)
	if err != nil || len(projects) != 1 {
		t.Fatalf("synced projects = %+v, %v", projects, err)
	}
	p := projects[0]
	if p.ID != project.ID || p.Info.Slug != "example" || p.Info.Name != "Example" || p.PullCursor != 7 || p.PushedHLC != "" || p.Info.KitKey != "" {
		t.Fatalf("project = %+v", p)
	}
	if _, err := domain.ParseUID(string(p.Info.UID)); err != nil {
		t.Fatal(err)
	}
}

func TestSyncRemoteURLRoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _ := openSyncStore(t)
	if url, err := store.SyncRemoteURL(ctx); err != nil || url != "" {
		t.Fatalf("default url = %q, %v", url, err)
	}
	if err := store.SetSyncRemoteURL(ctx, "https://sync.example"); err != nil {
		t.Fatal(err)
	}
	if url, err := store.SyncRemoteURL(ctx); err != nil || url != "https://sync.example" {
		t.Fatalf("url = %q, %v", url, err)
	}
}

func TestActivityLogsAreNeverCaptured(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	insert := func(eventType string) string {
		uid := string(domain.NewUID())
		mustExec(t, store, `INSERT INTO events(entity_type, project_id, event_type, uid) VALUES ('system', ?, ?, ?)`, project.ID, eventType, uid)
		return "events/" + uid
	}
	localCall := insert("cli.tool_call")
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	if got := capturedRows(t, store); slices.Contains(capturedTables(got), "events") {
		t.Fatalf("enqueued %v", got)
	}
	cli, tui, comment := insert("cli.tool_call"), insert("tui.tool_call"), insert("comment")
	mustExec(t, store, `UPDATE events SET status = 'ok', finished_at = CURRENT_TIMESTAMP WHERE event_type LIKE '%.tool_call'`)
	mustExec(t, store, `DELETE FROM events WHERE event_type LIKE '%.tool_call'`)
	got := capturedRows(t, store)
	if _, ok := got[comment]; !ok {
		t.Fatalf("captured %v, want %s", got, comment)
	}
	for _, key := range []string{localCall, cli, tui} {
		if _, ok := got[key]; ok {
			t.Fatalf("activity log %s captured", key)
		}
	}
}

func TestUpdateTriggersFireOnlyOnWireColumnChange(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	for table, key := range map[string]string{"plans": "plan", "plan_waves": "wave", "tasks": "child", "errors": "error", "solutions": "solution", "events": "event"} {
		before := mustScan[string](t, store, `SELECT hlc FROM sync_rows WHERE tbl = ? AND row_uid = ?`, table, uids[key])
		mustExec(t, store, `UPDATE `+table+` SET uid = uid WHERE uid = ?`, uids[key])
		if after := mustScan[string](t, store, `SELECT hlc FROM sync_rows WHERE tbl = ? AND row_uid = ?`, table, uids[key]); after != before {
			t.Fatalf("%s: unchanged update restamped %q to %q", table, before, after)
		}
	}
	latest := mustScan[string](t, store, `SELECT MAX(hlc) FROM sync_rows`)
	mustExec(t, store, `UPDATE tasks SET project_id = project_id, title = title, state = state`)
	if after := mustScan[string](t, store, `SELECT MAX(hlc) FROM sync_rows`); after != latest {
		t.Fatalf("no-op update restamped rows: %q -> %q", latest, after)
	}
}

func TestUpdateTriggersGuardExactlyTheWireColumns(t *testing.T) {
	t.Parallel()
	store, _ := openSyncStore(t)
	ctx := context.Background()
	for _, table := range []string{"plans", "plan_waves", "tasks", "errors", "solutions", "events"} {
		rule := syncRules[table]
		wire, err := rule.columns(ctx, store.db, table)
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range rule.refs {
			wire = append(wire, ref.col)
		}
		var def string
		name := "sync_capture_" + table + "_update"
		if err := store.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&def); err != nil {
			t.Fatal(err)
		}
		for _, col := range wire {
			if !strings.Contains(def, "NEW."+col+" IS NOT OLD."+col) {
				t.Errorf("%s does not guard wire column %s", name, col)
			}
		}
		if got, want := strings.Count(def, " IS NOT OLD."), len(wire); got != want {
			t.Errorf("%s guards %d columns, want the %d wire columns", name, got, want)
		}
	}
}

func TestReenablingSyncRecordsRowsDeletedWhileLocal(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	uids := seedRowGraph(t, store, project.ID)
	setScope(t, store, project.ID, domain.SyncScopeLocal)
	mustExec(t, store, `DELETE FROM tasks WHERE uid = ?`, uids["other"])
	mustExec(t, store, `DELETE FROM sync_rows WHERE row_uid = ?`, uids["child"])
	if _, err := store.db.ExecContext(context.Background(), `UPDATE sync_rows SET local = 0`); err != nil {
		t.Fatal(err)
	}
	old := mustScan[string](t, store, `SELECT MAX(hlc) FROM sync_rows`)
	setScope(t, store, project.ID, domain.SyncScopeSynced)

	if got := pending(t, store, project.ID)["tasks/"+uids["other"]]; got.Op != domain.SyncOpDelete || got.HLC <= old {
		t.Fatalf("task deleted while local = %+v, want a fresh delete", got)
	}
	if n := mustScan[int](t, store, `SELECT COUNT(*) FROM sync_rows WHERE local != 1 OR hlc <= ?`, old); n != 0 {
		t.Fatalf("%d sync_rows not re-stamped", n)
	}
}

func TestSyncRowsAreScopedByProject(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	other, err := store.UpsertProject(context.Background(), "Other", "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{project.ID, other.ID} {
		mustExec(t, store, `INSERT INTO sync_rows(project_id, tbl, row_uid) VALUES (?, 'tasks', 'shared')`, id)
		mustExec(t, store, `INSERT INTO sync_inbox(project_id, tbl, row_uid, hlc, change, missing_tbl, missing_uid) VALUES (?, 'tasks', 'shared', 'h', '{}', 'tasks', 'x')`, id)
	}
	mustExec(t, store, `INSERT INTO sync_rows(project_id, tbl, row_uid, deleted) VALUES (?, 'tasks', 'shared', 1)`, project.ID)
	if n := mustScan[int](t, store, `SELECT COUNT(*) FROM sync_rows WHERE row_uid = 'shared'`); n != 2 {
		t.Fatalf("%d sync_rows, want one per project", n)
	}
	if n := mustScan[int](t, store, `SELECT deleted FROM sync_rows WHERE project_id = ?`, other.ID); n != 0 {
		t.Fatalf("a replace in one project changed the other: deleted = %d", n)
	}
	if n := mustScan[int](t, store, `SELECT COUNT(*) FROM sync_inbox WHERE row_uid = 'shared'`); n != 2 {
		t.Fatalf("%d sync_inbox rows, want one per project", n)
	}
}

func TestRowDataReusesTheCachedSelectList(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	uids := seedRowGraph(t, store, project.ID)
	lists := map[string]string{}
	full, err := store.rowData(context.Background(), lists, "tasks", uids["child"])
	if err != nil || len(full) < 5 || lists["tasks"] == "" {
		t.Fatalf("rowData = %v, %v, cache = %v", full, err, lists)
	}
	lists["tasks"] = "r.title"
	data, err := store.rowData(context.Background(), lists, "tasks", uids["child"])
	if err != nil || len(data) != 1 || data["title"] != "child" {
		t.Fatalf("cached rowData = %v, %v", data, err)
	}
}

func TestRowDataOfJoinTablesNeverScansTheJoinTable(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	uids := seedRowGraph(t, store, project.ID)
	tag := "urgent"
	rowUIDs := map[string]string{
		"task_dependencies": uids["child"] + "|" + uids["other"],
		"task_tags":         uids["child"] + "|" + tag,
		"error_tags":        uids["error"] + "|" + tag,
		"event_tags":        uids["event"] + "|" + tag,
	}
	ctx := context.Background()
	lists := map[string]string{}
	for table, rowUID := range rowUIDs {
		data, err := store.rowData(ctx, lists, table, rowUID)
		if err != nil || data == nil {
			t.Fatalf("%s rowData = %v, %v", table, data, err)
		}
		rule := syncRules[table]
		where, args, found, err := store.rowFilter(ctx, rule, rowUID)
		if err != nil || !found {
			t.Fatalf("%s rowFilter: %v, %v", table, found, err)
		}
		for _, detail := range queryPlan(t, store, "SELECT "+lists[table]+" FROM "+table+" r WHERE "+where, args...) {
			if strings.HasPrefix(detail, "SCAN "+table) {
				t.Errorf("%s plan scans the join table: %s", table, detail)
			}
			t.Logf("%s: %s", table, detail)
		}
		if missing, err := store.rowData(ctx, lists, table, "missing|"+tag); err != nil || missing != nil {
			t.Fatalf("%s missing rowData = %v, %v", table, missing, err)
		}
	}
}

func queryPlan(t *testing.T, store *Store, query string, args ...any) []string {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return details
}

func TestRowDataKeepsSeparatorInsideTagName(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	uids := seedRowGraph(t, store, project.ID)
	mustExec(t, store, `INSERT INTO tags(name, label) VALUES ('a|b', 'AB')`)
	mustExec(t, store, `INSERT INTO task_tags(project_id, task_id, tag_id) SELECT ?, t.id, g.id FROM tasks t, tags g WHERE t.uid = ? AND g.name = 'a|b'`, project.ID, uids["child"])
	data, err := store.rowData(context.Background(), map[string]string{}, "task_tags", uids["child"]+"|a|b")
	if err != nil || data["tag_name"] != "a|b" {
		t.Fatalf("rowData = %v, %v", data, err)
	}
}

func TestActivityLogEventTagsAreNeverCaptured(t *testing.T) {
	t.Parallel()
	store, project := openSyncStore(t)
	tag := func(eventType string) (string, int64) {
		uid := string(domain.NewUID())
		mustExec(t, store, `INSERT INTO events(entity_type, project_id, event_type, uid) VALUES ('system', ?, ?, ?)`, project.ID, eventType, uid)
		id := mustScan[int64](t, store, `SELECT id FROM events WHERE uid = ?`, uid)
		mustExec(t, store, `INSERT OR IGNORE INTO tags(name, label) VALUES ('urgent', 'Urgent')`)
		mustExec(t, store, `INSERT INTO event_tags(event_id, tag_id) SELECT ?, id FROM tags WHERE name = 'urgent'`, id)
		return "event_tags/" + uid + "|urgent", id
	}
	localLog, _ := tag("cli.tool_call")
	setScope(t, store, project.ID, domain.SyncScopeSynced)
	if got := capturedRows(t, store); slices.Contains(capturedTables(got), "event_tags") {
		t.Fatalf("enqueued %v", got)
	}
	log, logID := tag("tui.tool_call")
	comment, commentID := tag("comment")
	mustExec(t, store, `DELETE FROM event_tags WHERE event_id = ?`, logID)
	mustExec(t, store, `DELETE FROM event_tags WHERE event_id = ?`, commentID)
	got := capturedRows(t, store)
	for _, key := range []string{localLog, log} {
		if _, ok := got[key]; ok {
			t.Fatalf("activity log tag %s captured: %v", key, got)
		}
	}
	if deleted, ok := got[comment]; !ok || deleted != 1 {
		t.Fatalf("captured %v, want delete of %s", got, comment)
	}
}
