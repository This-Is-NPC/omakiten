package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"omakiten/internal/domain"
)

func TestSchemaV4UpgradeBackfillsSyncIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	seedVersion3Database(t, ctx, path)

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	seen := map[string]bool{}
	for _, table := range syncUIDTables {
		assertBackfilledUIDs(t, ctx, store, table, seen)
	}
	device, actor, err := store.SyncIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, uid := range []domain.UID{device, actor} {
		if _, err := domain.ParseUID(string(uid)); err != nil {
			t.Fatalf("identity uid %q: %v", uid, err)
		}
	}
	var scopes int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE sync_scope = 'local'`).Scan(&scopes); err != nil || scopes == 0 {
		t.Fatalf("local projects = %d, %v", scopes, err)
	}
	upgraded, err := schemaFingerprint(ctx, store.db)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := currentSchemaFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded != fresh {
		t.Fatalf("upgraded fingerprint %s != fresh %s", upgraded, fresh)
	}
}

// seedVersion3Database writes a version 3 database with rows in every table
// that gains a uid.
func seedVersion3Database(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, dropSyncIdentitySQL+"PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}
	project, err := store.UpsertProject(ctx, "Example", "example", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (?1, 1, 'one', '', 2, 'active'), (?1, 1, 'two', '', 2, 'active')`,
		`INSERT INTO events(entity_type, entity_id, project_id, event_type) VALUES ('task', 1, ?1, 'created'), ('task', 2, ?1, 'created')`,
		`INSERT INTO errors(id, description, project_id) VALUES (1, 'boom', ?1)`,
		`INSERT INTO solutions(error_id, description) VALUES (1, 'fix')`,
		`INSERT INTO plans(id, project_id, slug, name) VALUES (1, ?1, 'p', 'P')`,
		`INSERT INTO plan_waves(plan_id, name, position) VALUES (1, 'w1', 1)`,
	} {
		if _, err := store.db.ExecContext(ctx, statement, project.ID); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

// assertBackfilledUIDs checks table has rows and every uid is a valid UUIDv7
// absent from seen, then records them.
func assertBackfilledUIDs(t *testing.T, ctx context.Context, store *Store, table string, seen map[string]bool) {
	t.Helper()
	rows, err := store.db.QueryContext(ctx, "SELECT COALESCE(uid, '') FROM "+table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatal(err)
		}
		if _, err := domain.ParseUID(uid); err != nil {
			t.Fatalf("%s uid %q: %v", table, uid, err)
		}
		if seen[uid] {
			t.Fatalf("%s uid %q is not unique", table, uid)
		}
		seen[uid] = true
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("%s has no rows to backfill", table)
	}
}

func TestSyncIdentityIsStableAcrossReopen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	device, actor, err := first.SyncIdentity(ctx)
	if err != nil || device == "" || actor == "" || device == actor {
		t.Fatalf("identity = %q, %q, %v", device, actor, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	again, againActor, err := second.SyncIdentity(ctx)
	if err != nil || again != device || againActor != actor {
		t.Fatalf("identity after reopen = %q, %q, %v", again, againActor, err)
	}
}

func TestNextEventHLCIncreasesAcrossTransactions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	device, _, err := store.SyncIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var last domain.HLC
	for i := range 5 {
		stamp := commitNextEventHLC(t, ctx, store)
		if stamp.Device != device {
			t.Fatalf("stamp device = %q, want %q", stamp.Device, device)
		}
		if i > 0 && stamp.Compare(last) <= 0 {
			t.Fatalf("stamp %s does not follow %s", stamp, last)
		}
		last = stamp
	}
	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT hlc FROM sync_identity`).Scan(&stored); err != nil || stored != last.String() {
		t.Fatalf("stored hlc = %q, %v; want %s", stored, err, last)
	}
}

func commitNextEventHLC(t *testing.T, ctx context.Context, store *Store) domain.HLC {
	t.Helper()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := nextEventHLC(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return stamp
}
