package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenBridgesExactV030ReleaseDatabaseAndPreservesData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "omakiten.db")
	seedV030ReleaseDatabase(t, path, true)
	seedV030Data(t, path)
	seedStaleV030SearchIndex(t, path)

	beforeSource := bridgeSourceHash(t, path)
	beforeSequence := bridgeSequenceSnapshot(t, path)
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open exact v0.30.0 database: %v", err)
	}
	assertV030BridgeState(t, ctx, store, beforeSource, beforeSequence)
	if err := store.Close(); err != nil {
		t.Fatalf("Close after bridge: %v", err)
	}

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open after bridge: %v", err)
	}
	assertV030BridgeState(t, ctx, second, beforeSource, beforeSequence)
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func assertV030BridgeState(t *testing.T, ctx context.Context, store *Store, sourceHash, sequence string) {
	t.Helper()
	if got := bridgeSourceHashFromDB(t, store.db); got != sourceHash {
		t.Fatalf("bridge changed source data:\nbefore=%s\nafter=%s", sourceHash, got)
	}
	if got := bridgeSequenceSnapshotFromDB(t, store.db); got != sequence {
		t.Fatalf("bridge changed sqlite_sequence:\nbefore=%s\nafter=%s", sequence, got)
	}
	assertV030BridgeMarker(t, ctx, store.db)
	assertV030BridgeSearch(t, ctx, store)
}

func assertV030BridgeMarker(t *testing.T, ctx context.Context, db schemaQueryer) {
	t.Helper()
	var userVersion int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if userVersion != 2 {
		t.Fatalf("user_version = %d, want 2", userVersion)
	}
	var migrationTables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&migrationTables); err != nil {
		t.Fatalf("schema_migrations lookup: %v", err)
	}
	if migrationTables != 0 {
		t.Fatalf("schema_migrations count = %d, want 0", migrationTables)
	}
}

func assertV030BridgeSearch(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	var ftsContent string
	if err := store.db.QueryRowContext(ctx, `SELECT content FROM search_index WHERE entity_type = 'comment' AND entity_id = 31`).Scan(&ftsContent); err != nil {
		t.Fatalf("rebuilt FTS row: %v", err)
	}
	if ftsContent != "comment body comment title" {
		t.Fatalf("rebuilt FTS content = %q", ftsContent)
	}
	var orphanCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_index WHERE entity_type = 'note'`).Scan(&orphanCount); err != nil {
		t.Fatalf("orphan FTS rows: %v", err)
	}
	if orphanCount != 0 {
		t.Fatalf("orphan FTS rows = %d, want 0", orphanCount)
	}
	searchReport, err := store.CheckSearchIndex(ctx)
	if err != nil || !searchReport.Healthy || !searchReport.FTS5.OK {
		t.Fatalf("post-bridge search check = %+v, %v", searchReport, err)
	}
	if searchReport.Triggers.ExpectedCount != 16 || searchReport.Triggers.ActualCount != 16 {
		t.Fatalf("post-bridge trigger counts = %+v, want 16", searchReport.Triggers)
	}
	hits, err := store.Search(ctx, "comment title", 7, nil)
	if err != nil || len(hits) != 1 || hits[0].ID != 31 {
		t.Fatalf("title search hits = %+v, %v", hits, err)
	}
}

func TestOpenBridgeRollsBackSearchRebuildAndMarkerTogether(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "omakiten.db")
	seedV030ReleaseDatabase(t, path, false)
	seedV030Data(t, path)
	seedStaleV030SearchIndex(t, path)
	beforeSchema := schemaFileSnapshot(t, path)
	beforeData := bridgeDataSnapshot(t, path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	err = bridgeV030ReleaseDatabaseWithHooks(ctx, db, v030BridgeHooks{
		afterSearchRebuild: func(ctx context.Context, conn searchIndexDB) error {
			_, err := conn.ExecContext(ctx, `DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = 31`)
			return err
		},
	})
	if err == nil {
		t.Fatal("bridge accepted a corrupted post-rebuild search index")
	}
	if after := schemaFileSnapshot(t, path); after != beforeSchema {
		t.Fatalf("failed bridge changed schema:\nbefore=%s\nafter=%s", beforeSchema, after)
	}
	if after := bridgeDataSnapshot(t, path); after != beforeData {
		t.Fatalf("failed bridge changed marker or search index:\nbefore=%s\nafter=%s", beforeData, after)
	}
}

func TestOpenBridgeCancellationAfterCommitDoesNotReportDurableFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.db")
	seedV030ReleaseDatabase(t, path, false)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	err = bridgeV030ReleaseDatabaseWithHooks(ctx, db, v030BridgeHooks{afterCommit: cancel})
	if err != nil {
		t.Fatalf("bridge canceled after commit: %v", err)
	}
	var userVersion int
	if err := db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&userVersion); err != nil {
		t.Fatalf("user_version after committed cancellation: %v", err)
	}
	if userVersion != 2 {
		t.Fatalf("user_version after committed cancellation = %d, want 2", userVersion)
	}
}

func TestV030ReleaseSchemaFingerprintIsPinned(t *testing.T) {
	fingerprint, err := releaseSchemaSemanticFingerprint(context.Background())
	if err != nil {
		t.Fatalf("current schema fingerprint: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(fingerprint))); got != v030ReleaseSchemaFingerprint {
		t.Fatalf("current schema fingerprint = %s, want pinned v0.30.0 %s", got, v030ReleaseSchemaFingerprint)
	}
}

func TestOpenRejectsEveryNonExactV030SourceWithoutMutation(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing migration": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `DELETE FROM schema_migrations WHERE version = '035_events_order_by_indexes.sql'`)
		},
		"extra migration": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `INSERT INTO schema_migrations(version) VALUES ('036_future.sql')`)
		},
		"wrong history table shape": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `ALTER TABLE schema_migrations ADD COLUMN unexpected TEXT`)
		},
		"schema drift": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `DROP INDEX idx_events_project_created`)
		},
		"literal drift": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `DROP TRIGGER search_index_tasks_ai`)
			execBridgeSQL(t, path, `CREATE TRIGGER search_index_tasks_ai AFTER INSERT ON tasks BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.description, ''), 'TASK', NEW.id, NEW.project_id);
END`)
		},
		"foreign object": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `CREATE TABLE foreign_data (id INTEGER PRIMARY KEY, value TEXT)`)
		},
		"wrong marker": func(t *testing.T, path string) {
			execBridgeSQL(t, path, `PRAGMA user_version = 2`)
		},
	}

	for name, prepare := range cases {
		name, prepare := name, prepare
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "omakiten.db")
			seedV030ReleaseDatabase(t, path, false)
			prepare(t, path)
			beforeSchema := schemaFileSnapshot(t, path)
			beforeData := bridgeDataSnapshot(t, path)
			if store, err := Open(context.Background(), path); err == nil {
				_ = store.Close()
				t.Fatal("Open accepted a non-exact v0.30.0 database")
			}
			if after := schemaFileSnapshot(t, path); after != beforeSchema {
				t.Fatalf("rejected database schema changed:\nbefore=%s\nafter=%s", beforeSchema, after)
			}
			if after := bridgeDataSnapshot(t, path); after != beforeData {
				t.Fatalf("rejected database data changed:\nbefore=%s\nafter=%s", beforeData, after)
			}
		})
	}
}

func TestOpenBridgeIsAtomicWhenWriterLockCannotBeAcquired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.db")
	seedV030ReleaseDatabase(t, path, false)
	beforeSchema := schemaFileSnapshot(t, path)
	beforeData := bridgeDataSnapshot(t, path)

	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("lock sql.Open: %v", err)
	}
	defer func() { _ = locker.Close() }()
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("BEGIN IMMEDIATE lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if store, err := OpenWithOptions(ctx, path, Options{BusyTimeoutMs: 1}); err == nil {
		_ = store.Close()
		t.Fatal("Open succeeded while another writer held the lock")
	}
	if after := schemaFileSnapshot(t, path); after != beforeSchema {
		t.Fatalf("locked rejection changed schema:\nbefore=%s\nafter=%s", beforeSchema, after)
	}
	if after := bridgeDataSnapshot(t, path); after != beforeData {
		t.Fatalf("locked rejection changed data:\nbefore=%s\nafter=%s", beforeData, after)
	}
	if _, err := locker.Exec("ROLLBACK"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
}

func TestOpenSearchMaintenanceDoesNotBridgeV030ReleaseDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.db")
	seedV030ReleaseDatabase(t, path, false)
	beforeSchema := schemaFileSnapshot(t, path)
	if store, err := OpenSearchMaintenance(context.Background(), path); err == nil {
		_ = store.Close()
		t.Fatal("OpenSearchMaintenance accepted a v0.30.0 database")
	}
	if after := schemaFileSnapshot(t, path); after != beforeSchema {
		t.Fatalf("OpenSearchMaintenance changed schema:\nbefore=%s\nafter=%s", beforeSchema, after)
	}
}

func TestValidateV030ReleaseDatabaseIsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.db")
	seedV030ReleaseDatabase(t, path, false)
	seedV030Data(t, path)
	before := schemaFileSnapshot(t, path)
	beforeData := bridgeDataSnapshot(t, path)

	if err := ValidateV030ReleaseDatabase(context.Background(), path); err != nil {
		t.Fatalf("ValidateV030ReleaseDatabase: %v", err)
	}
	if after := schemaFileSnapshot(t, path); after != before {
		t.Fatalf("eligibility check changed database schema:\nbefore=%s\nafter=%s", before, after)
	}
	if after := bridgeDataSnapshot(t, path); after != beforeData {
		t.Fatalf("eligibility check changed database data:\nbefore=%s\nafter=%s", beforeData, after)
	}
}

func TestValidateV030ReleaseDatabaseDoesNotCreateMissingPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing", "nested", "omakiten.db")

	if err := ValidateV030ReleaseDatabase(context.Background(), path); err == nil {
		t.Fatal("ValidateV030ReleaseDatabase accepted a missing database")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("validator created missing parent directory; stat error = %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("validator created %s; stat error = %v", path+suffix, err)
		}
	}
}

func TestValidateV030ReleaseDatabaseAcceptsExactUncheckpointedWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omakiten.db")
	db := seedV030WALDatabase(t, path, nil)
	defer func() { _ = db.Close() }()
	before := bridgeDataSnapshot(t, path)

	if err := ValidateV030ReleaseDatabase(context.Background(), path); err != nil {
		t.Fatalf("ValidateV030ReleaseDatabase with exact WAL state: %v", err)
	}
	if after := bridgeDataSnapshot(t, path); after != before {
		t.Fatalf("WAL eligibility changed database data:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestNormalizeSchemaDefinitionPreservesLiteralsAndNormalizesIdentifiers(t *testing.T) {
	plain := `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`
	quoted := `create table [schema_migrations] ("version" text primary key, "applied_at" text not null default current_timestamp)`
	if normalizeSchemaDefinition(plain) != normalizeSchemaDefinition(quoted) {
		t.Fatalf("quoted identifiers did not canonicalize with bare identifiers")
	}
	withTask := `CREATE TRIGGER x AFTER INSERT ON tasks WHEN NEW.kind = 'task' BEGIN SELECT 1; END`
	withUpperTask := strings.Replace(withTask, "'task'", "'TASK'", 1)
	if normalizeSchemaDefinition(withTask) == normalizeSchemaDefinition(withUpperTask) {
		t.Fatal("literal mutation was normalized away")
	}
	withLiteralModifier := `CREATE TABLE x (value TEXT DEFAULT 'if not exists')`
	changedLiteralModifier := strings.Replace(withLiteralModifier, "'if not exists'", "'IF NOT EXISTS'", 1)
	if normalizeSchemaDefinition(withLiteralModifier) == normalizeSchemaDefinition(changedLiteralModifier) {
		t.Fatal("IF NOT EXISTS inside a literal was normalized away")
	}
}

func seedV030ReleaseDatabase(t *testing.T, path string, quoted bool) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("seed sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	schema := schemaSQL
	if quoted {
		for _, name := range []string{"projects", "tasks", "events", "tags", "task_tags", "project_tags", "errors", "solutions", "error_tags", "event_tags", "task_dependencies", "plans", "plan_waves"} {
			schema = strings.ReplaceAll(schema, "CREATE TABLE "+name, `CREATE TABLE "`+name+`"`)
		}
		schema = strings.ReplaceAll(schema, "CREATE VIRTUAL TABLE search_index", `CREATE VIRTUAL TABLE "search_index"`)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("seed current schema: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE document_metadata; PRAGMA user_version = 0`); err != nil {
		t.Fatalf("seed user_version: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE "schema_migrations" (
  "version" TEXT PRIMARY KEY,
  "applied_at" TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		t.Fatalf("seed schema_migrations: %v", err)
	}
	for _, version := range v030MigrationVersions {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			t.Fatalf("seed migration %s: %v", version, err)
		}
	}
}

func seedV030WALDatabase(t *testing.T, path string, mutate func(*testing.T, *sql.DB)) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("WAL seed sql.Open: %v", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed schema: %v", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed journal mode: %v", err)
	}
	if _, err := db.Exec(`PRAGMA wal_autocheckpoint = 0`); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed autocheckpoint: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE document_metadata; PRAGMA user_version = 0`); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed user_version: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed schema_migrations: %v", err)
	}
	for _, version := range v030MigrationVersions {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			_ = db.Close()
			t.Fatalf("WAL seed migration %s: %v", version, err)
		}
	}
	seedV030DataWithDB(t, db)
	if mutate != nil {
		mutate(t, db)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed did not leave a WAL sidecar: %v", err)
	}
	if _, err := os.Stat(path + "-shm"); err != nil {
		_ = db.Close()
		t.Fatalf("WAL seed did not leave an SHM sidecar: %v", err)
	}
	return db
}

func seedV030DataWithDB(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO projects(id, name, slug, root_path) VALUES (7, 'Project', 'project', '/project')`,
		`INSERT INTO tags(id, name, label) VALUES (13, 'tag', 'Tag')`,
		`INSERT INTO plans(id, project_id, slug, name) VALUES (17, 7, 'plan', 'Plan')`,
		`INSERT INTO plan_waves(id, plan_id, name, position) VALUES (19, 17, 'Wave', 1)`,
		`INSERT INTO tasks(id, project_id, title, priority_id, plan_id, wave_id) VALUES (23, 7, 'Parent', 2, 17, 19)`,
		`INSERT INTO tasks(id, project_id, title, priority_id, plan_id, wave_id, parent_id, depth) VALUES (29, 7, 'Child', 2, 17, 19, 23, 1)`,
		`INSERT INTO events(id, entity_type, entity_id, project_id, event_type, body, title, author_type) VALUES (31, 'task', 23, 7, 'comment', 'comment body', 'comment title', 'human')`,
		`INSERT INTO errors(id, description, project_id) VALUES (37, 'Error', 7)`,
		`INSERT INTO solutions(id, error_id, description) VALUES (41, 37, 'Solution')`,
		`INSERT INTO task_tags(project_id, task_id, tag_id) VALUES (7, 23, 13)`,
		`INSERT INTO project_tags(project_id, tag_id) VALUES (7, 13)`,
		`INSERT INTO error_tags(error_id, tag_id) VALUES (37, 13)`,
		`INSERT INTO event_tags(event_id, tag_id) VALUES (31, 13)`,
		`INSERT INTO task_dependencies(project_id, task_id, depends_on_task_id) VALUES (7, 29, 23)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("WAL seed data %q: %v", statement, err)
		}
	}
}

func seedV030Data(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("data sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	seedV030DataWithDB(t, db)
}

func seedStaleV030SearchIndex(t *testing.T, path string) {
	t.Helper()
	execBridgeSQL(t, path, `UPDATE search_index SET content = 'comment body' WHERE entity_type = 'comment' AND entity_id = 31`)
	execBridgeSQL(t, path, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('orphaned v0.30 content', 'note', 999, 7)`)
}

func execBridgeSQL(t *testing.T, path, statement string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("mutation sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(statement); err != nil {
		t.Fatalf("mutation %q: %v", statement, err)
	}
}

func bridgeDataSnapshot(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("snapshot sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	return bridgeDataSnapshotFromDB(t, db)
}

func bridgeDataSnapshotFromDB(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	for _, table := range []string{"projects", "tasks", "events", "tags", "task_tags", "project_tags", "errors", "solutions", "error_tags", "event_tags", "task_dependencies", "plans", "plan_waves", "search_index", "sqlite_sequence"} {
		rows, err := db.Query(`SELECT * FROM "` + table + `" ORDER BY rowid`)
		if err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			t.Fatalf("snapshot columns %s: %v", table, err)
		}
		fmt.Fprintf(&snapshot, "%s(%d):", table, len(columns))
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for index := range values {
				pointers[index] = &values[index]
			}
			if err := rows.Scan(pointers...); err != nil {
				_ = rows.Close()
				t.Fatalf("snapshot row %s: %v", table, err)
			}
			fmt.Fprintf(&snapshot, "%#v;", values)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("snapshot rows %s: %v", table, err)
		}
		_ = rows.Close()
		snapshot.WriteByte('\n')
	}
	return snapshot.String()
}

func bridgeSourceHash(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("source hash sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	return bridgeSourceHashFromDB(t, db)
}

func bridgeSourceHashFromDB(t *testing.T, db *sql.DB) string {
	t.Helper()
	return fmt.Sprintf("%x", sha256.Sum256([]byte(bridgeSourceSnapshotFromDB(t, db))))
}

func bridgeSourceSnapshotFromDB(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	for _, table := range []string{"projects", "tasks", "events", "tags", "task_tags", "project_tags", "errors", "solutions", "error_tags", "event_tags", "task_dependencies", "plans", "plan_waves"} {
		appendBridgeTableSnapshot(t, db, &snapshot, table)
	}
	return snapshot.String()
}

func bridgeSequenceSnapshot(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sequence snapshot sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	return bridgeSequenceSnapshotFromDB(t, db)
}

func bridgeSequenceSnapshotFromDB(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	appendBridgeTableSnapshot(t, db, &snapshot, "sqlite_sequence")
	return snapshot.String()
}

func appendBridgeTableSnapshot(t *testing.T, db *sql.DB, snapshot *strings.Builder, table string) {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + table + `" ORDER BY rowid`)
	if err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("snapshot columns %s: %v", table, err)
	}
	fmt.Fprintf(snapshot, "%s(%d):", table, len(columns))
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatalf("snapshot row %s: %v", table, err)
		}
		fmt.Fprintf(snapshot, "%#v;", values)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows %s: %v", table, err)
	}
	snapshot.WriteByte('\n')
}
