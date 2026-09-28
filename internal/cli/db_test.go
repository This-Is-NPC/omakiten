package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBBackupIncludesPinnedCommittedWALFrames(t *testing.T) {
	for _, mode := range []string{"rolling", "out"} {
		t.Run(mode, func(t *testing.T) { testDBBackupIncludesPinnedCommittedWALFrames(t, mode) })
	}
}

func testDBBackupIncludesPinnedCommittedWALFrames(t *testing.T, mode string) {
	t.Helper()
	ctx := context.Background()
	fixture := newCLIDBFixture(t, "omakiten.db")
	tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(3)
	reader := beginPinnedWALReader(t, ctx, db)
	defer func() { _ = reader.Close() }()
	defer func() { _, _ = reader.ExecContext(context.Background(), `ROLLBACK`) }()
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (1, 1, 'committed wal backup row', '', 2, 'active')`); err != nil {
		t.Fatalf("insert committed WAL row: %v", err)
	}

	args := []string{"db", "backup"}
	if mode == "out" {
		args = append(args, "--out", filepath.Join(tmp, "manual", "snapshot.db"))
	}
	output := runCLI(t, dbPath, configPath, args...)
	envelope := decodeBackupEnvelope(t, output)
	snapshot, err := sql.Open("sqlite", envelope.Data.Path)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = snapshot.Close() }()
	var count int
	if err := snapshot.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE title = 'committed wal backup row'`).Scan(&count); err != nil {
		t.Fatalf("query backup: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed WAL rows in %s backup = %d, want 1", mode, count)
	}
}

func beginPinnedWALReader(t *testing.T, ctx context.Context, db *sql.DB) *sql.Conn {
	t.Helper()
	for _, pragma := range []struct{ sql, message string }{
		{`PRAGMA journal_mode = WAL`, "enable WAL"},
		{`PRAGMA wal_autocheckpoint = 0`, "disable autocheckpoint"},
		{`PRAGMA wal_checkpoint(TRUNCATE)`, "baseline checkpoint"},
	} {
		if _, err := db.ExecContext(ctx, pragma.sql); err != nil {
			t.Fatalf("%s: %v", pragma.message, err)
		}
	}
	reader, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reader conn: %v", err)
	}
	if _, err := reader.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatalf("reader BEGIN: %v", err)
	}
	var baseline int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&baseline); err != nil {
		t.Fatalf("reader baseline: %v", err)
	}
	return reader
}

type backupEnvelope struct {
	Data struct {
		Path string `json:"path"`
	} `json:"data"`
}

func decodeBackupEnvelope(t *testing.T, output string) backupEnvelope {
	t.Helper()
	var envelope backupEnvelope
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return envelope
}

func TestDBBackupCommand_DefaultPathAndOutOverride(t *testing.T) {
	fixture := newCLIDBFixture(t, "omakiten.db")
	tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath
	stateRoot := filepath.Join(tmp, "state")

	output := runCLI(t, dbPath, configPath, "db", "backup")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v (raw %q)", err, output)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.data missing: %v", envelope)
	}
	pathAny, ok := data["path"].(string)
	if !ok || pathAny == "" {
		t.Fatalf("envelope.data.path missing or empty: %v", data)
	}
	if !strings.HasPrefix(pathAny, filepath.Join(stateRoot, "omakiten", "backups")) {
		t.Fatalf("backup path = %q, want under %s", pathAny, filepath.Join(stateRoot, "omakiten", "backups"))
	}
	if _, err := os.Stat(pathAny); err != nil {
		t.Fatalf("backup file missing on disk: %v", err)
	}
	if data["pruned"] != true {
		t.Fatalf("envelope.data.pruned = %v, want true (default path runs prune)", data["pruned"])
	}

	// --out override writes to the supplied file path and skips prune.
	explicit := filepath.Join(tmp, "manual", "snapshot.db")
	outputOut := runCLI(t, dbPath, configPath, "db", "backup", "--out", explicit)
	var envOut map[string]any
	if err := json.Unmarshal([]byte(outputOut), &envOut); err != nil {
		t.Fatalf("unmarshal --out envelope: %v", err)
	}
	dataOut, ok := envOut["data"].(map[string]any)
	if !ok {
		t.Fatalf("--out envelope.data missing: %v", envOut)
	}
	if dataOut["path"] != explicit {
		t.Fatalf("--out path = %v, want %s", dataOut["path"], explicit)
	}
	if dataOut["pruned"] != false {
		t.Fatalf("--out pruned = %v, want false", dataOut["pruned"])
	}
	if _, err := os.Stat(explicit); err != nil {
		t.Fatalf("--out file missing: %v", err)
	}
}

func TestDBBackupCommand_SourceMissingFails(t *testing.T) {
	fixture := newCLIDBFixture(t, "no-such.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath

	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove db: %v", err)
	}
	runCLIExpectError(t, dbPath, configPath, "internal_error", "db", "backup")
}
