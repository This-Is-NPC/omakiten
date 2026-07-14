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
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			fixture := newCLIDBFixture(t, "omakiten.db")
			tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath

			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			defer func() { _ = db.Close() }()
			db.SetMaxOpenConns(3)
			if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
				t.Fatalf("enable WAL: %v", err)
			}
			if _, err := db.ExecContext(ctx, `PRAGMA wal_autocheckpoint = 0`); err != nil {
				t.Fatalf("disable autocheckpoint: %v", err)
			}
			if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
				t.Fatalf("baseline checkpoint: %v", err)
			}
			reader, err := db.Conn(ctx)
			if err != nil {
				t.Fatalf("reader conn: %v", err)
			}
			defer func() { _ = reader.Close() }()
			if _, err := reader.ExecContext(ctx, `BEGIN`); err != nil {
				t.Fatalf("reader BEGIN: %v", err)
			}
			defer func() { _, _ = reader.ExecContext(context.Background(), `ROLLBACK`) }()
			var baseline int
			if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&baseline); err != nil {
				t.Fatalf("reader baseline: %v", err)
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO tasks(project_id, bucket_id, title, description, priority_id, state) VALUES (1, 1, 'committed wal backup row', '', 2, 'active')`); err != nil {
				t.Fatalf("insert committed WAL row: %v", err)
			}

			args := []string{"db", "backup"}
			if mode == "out" {
				args = append(args, "--out", filepath.Join(tmp, "manual", "snapshot.db"))
			}
			output := runCLI(t, dbPath, configPath, args...)
			var envelope struct {
				Data struct {
					Path string `json:"path"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(output), &envelope); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
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
		})
	}
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
