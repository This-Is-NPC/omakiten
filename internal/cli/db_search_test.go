package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestDBSearchCommandsRegistered(t *testing.T) {
	t.Parallel()

	root := NewRootCommand("test")
	for _, path := range [][]string{{"db", "check"}, {"db", "reindex"}} {
		command, _, err := root.Find(path)
		if err != nil || command == nil || command.Name() != path[1] {
			t.Fatalf("Find(%v) = %v, %v", path, command, err)
		}
	}
}

func TestDBCheckHealthyJSONHonorsDBAndIgnoresInvalidConfig(t *testing.T) {
	fixture := newCLIDBFixture(t, "explicit.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath
	if err := os.WriteFile(configPath, []byte("not: [valid"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	beforeSchema := dbSchemaSnapshot(t, dbPath)
	output := runCLI(t, dbPath, configPath, "db", "check")
	if afterSchema := dbSchemaSnapshot(t, dbPath); afterSchema != beforeSchema {
		t.Fatalf("healthy db check mutated schema or journal\nbefore=%s\nafter=%s", beforeSchema, afterSchema)
	}
	var envelope struct {
		Data struct {
			Healthy bool `json:"healthy"`
			FTS5    struct {
				OK bool `json:"ok"`
			} `json:"fts5"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !envelope.Data.Healthy || !envelope.Data.FTS5.OK {
		t.Fatalf("healthy check output = %s", output)
	}
}

func TestDBCheckUnhealthyReturnsCompleteCodedReport(t *testing.T) {
	fixture := newCLIDBFixture(t, "corrupt.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath
	execCLIDBSQL(t, dbPath, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private retired text', 'note', 999, 1)`)

	envelope := runCLIExpectError(t, dbPath, configPath, "search_index_invalid", "db", "check")
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing: %v", envelope)
	}
	report, ok := details["report"].(map[string]any)
	if !ok || report["healthy"] != false || report["source_total"] == nil || report["index_total"] == nil || report["types"] == nil || report["triggers"] == nil || report["fts5"] == nil {
		t.Fatalf("complete report missing: %v", details)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(raw) == 0 || bytes.Contains(raw, []byte("private retired text")) {
		t.Fatalf("unhealthy envelope exposed indexed text: %s", raw)
	}
}

func TestDBSearchCommandsRefuseMissingDatabaseWithoutCreatingIt(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "missing.db")
	configPath := filepath.Join(tmp, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte("invalid: ["), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, subcommand := range []string{"check", "reindex"} {
		runCLIExpectError(t, dbPath, configPath, "validation_error", "db", subcommand)
		if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
			t.Fatalf("db %s created missing path; stat error = %v", subcommand, err)
		}
	}
}

func TestDBCheckRejectsOldSchemaWithoutMigrating(t *testing.T) {
	fixture := newCLIDBFixture(t, "old.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath
	execCLIDBSQL(t, dbPath, `DELETE FROM schema_migrations WHERE version = '035_events_order_by_indexes.sql'`)
	execCLIDBSQL(t, dbPath, `DROP INDEX idx_events_project_created`)
	execCLIDBSQL(t, dbPath, `DROP INDEX idx_events_project_type_created`)
	execCLIDBSQL(t, dbPath, `CREATE INDEX idx_events_project_type ON events(project_id, event_type, entity_type, created_at)`)
	before := dbSchemaSnapshot(t, dbPath)

	runCLIExpectError(t, dbPath, configPath, "validation_error", "db", "check")
	after := dbSchemaSnapshot(t, dbPath)
	if before != after {
		t.Fatalf("db check migrated old schema\nbefore=%s\nafter=%s", before, after)
	}
}

func TestDBCheckRejectsNonOmakitenDatabaseWithoutMutation(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "foreign.db")
	configPath := filepath.Join(tmp, "invalid.yaml")
	execCLIDBSQL(t, dbPath, `CREATE TABLE foreign_data(id INTEGER PRIMARY KEY, value TEXT)`)
	before := dbSchemaSnapshot(t, dbPath)

	runCLIExpectError(t, dbPath, configPath, "validation_error", "db", "check")
	if after := dbSchemaSnapshot(t, dbPath); after != before {
		t.Fatalf("db check mutated non-Omakiten schema\nbefore=%s\nafter=%s", before, after)
	}
}

func TestDBCheckRejectsSymlinkDatabasePath(t *testing.T) {
	fixture := newCLIDBFixture(t, "real.db")
	tmp := fixture.root
	realPath := filepath.Join(tmp, "real.db")
	configPath := fixture.configPath
	symlinkPath := filepath.Join(tmp, "linked.db")
	if err := os.Symlink(realPath, symlinkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	runCLIExpectError(t, symlinkPath, configPath, "validation_error", "db", "check")
}

func TestDBCheckRejectsSymlinkedParentDirectory(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	realPath := filepath.Join(realDir, "real.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	t.Chdir(projectRoot)
	runCLI(t, realPath, configPath, "init", "--name", "Project", "--slug", "project")
	linkedDir := filepath.Join(tmp, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	runCLIExpectError(t, filepath.Join(linkedDir, "real.db"), configPath, "validation_error", "db", "check")
}

func TestDBReindexRepairsAndReturnsBeforeAfterJSON(t *testing.T) {
	fixture := newCLIDBFixture(t, "repair database.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath
	execCLIDBSQL(t, dbPath, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('private malformed row', 'task', CAST(999 AS TEXT), 1)`)
	preflightSchema := dbSchemaSnapshot(t, dbPath)
	refused := runCLIExpectError(t, dbPath, configPath, "validation_error", "db", "reindex")
	if afterPreflight := dbSchemaSnapshot(t, dbPath); afterPreflight != preflightSchema {
		t.Fatalf("reindex preflight mutated schema, migrations, or journal\nbefore=%s\nafter=%s", preflightSchema, afterPreflight)
	}
	details, ok := refused["details"].(map[string]any)
	wantCommand, wantArgs := dbReindexRetryGuidance(dbPath)
	if !ok || details["requires_confirmation"] != true || details["retry_command"] != wantCommand || !reflect.DeepEqual(details["retry_args"], []any{wantArgs[0], wantArgs[1], wantArgs[2], wantArgs[3], wantArgs[4]}) {
		t.Fatalf("confirmation error details = %v", refused)
	}
	var stillMalformed int
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM search_index WHERE typeof(entity_id) = 'text'`).Scan(&stillMalformed); err != nil {
		_ = db.Close()
		t.Fatalf("count malformed after refusal: %v", err)
	}
	_ = db.Close()
	if stillMalformed != 1 {
		t.Fatalf("malformed rows after refusal = %d, want 1", stillMalformed)
	}

	cmd := NewRootCommand("test")
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--db", dbPath, "--config", configPath, "db", "reindex", "--confirm"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("db reindex: %v, output=%s", err, stdout.String())
	}
	output := strings.TrimSpace(stdout.String())
	var envelope struct {
		Data struct {
			Before struct {
				Healthy bool `json:"healthy"`
				Types   []struct {
					EntityType string `json:"entity_type"`
					Malformed  struct {
						Count int `json:"count"`
					} `json:"malformed"`
				} `json:"types"`
			} `json:"before"`
			After struct {
				Healthy bool `json:"healthy"`
			} `json:"after"`
			BackupRecommended bool   `json:"backup_recommended"`
			DatabasePath      string `json:"database_path"`
			BackupPath        string `json:"backup_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Data.Before.Healthy || !envelope.Data.After.Healthy || !envelope.Data.BackupRecommended || envelope.Data.DatabasePath != dbPath || envelope.Data.BackupPath == "" {
		t.Fatalf("repair output = %s", output)
	}
	malformedCount := 0
	for _, typeReport := range envelope.Data.Before.Types {
		malformedCount += typeReport.Malformed.Count
	}
	if malformedCount != 1 {
		t.Fatalf("before malformed count = %d, want 1", malformedCount)
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "backup") || !strings.Contains(stderr.String(), envelope.Data.BackupPath) {
		t.Fatalf("malformed-only backup warning = %q, want actual verified backup path", stderr.String())
	}
	runCLI(t, dbPath, configPath, "db", "check")
}

func TestReindexConfirmationErrorGuidanceEnrichesTransactionalRecheck(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "database with spaces.db")
	storageErr := domain.NewError(domain.ErrValidation, "confirmation required before discarding existing search-index evidence", map[string]any{
		"requires_confirmation": true,
		"report":                domain.SearchIndexIntegrityReport{},
	})
	got := reindexConfirmationErrorWithRetryGuidance(storageErr, dbPath, "confirmation required; run %s")
	var coded *domain.CodedError
	if !errors.As(got, &coded) {
		t.Fatalf("enriched error = %v, want CodedError", got)
	}
	wantCommand, wantArgs := dbReindexRetryGuidance(dbPath)
	if coded.Message != fmt.Sprintf("confirmation required; run %s", wantCommand) || coded.Details["retry_command"] != wantCommand || !reflect.DeepEqual(coded.Details["retry_args"], wantArgs) || coded.Details["database_path"] != dbPath {
		t.Fatalf("enriched transactional error = %+v", coded)
	}
}

func TestDBReindexReconstructiveSuccessIdentifiesExactDatabase(t *testing.T) {
	fixture := newCLIDBFixture(t, "reconstruct database.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath
	execCLIDBSQL(t, dbPath, `DROP TRIGGER search_index_tasks_ai`)
	backupDir := filepath.Join(fixture.root, "state", "omakiten", "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatalf("create backup directory: %v", err)
	}
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("2026-07-13T10-00-%02d.000000000Z.db", i)
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("seed historical backup: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(backupDir, ".omakiten-backup.lock"), nil, 0o600); err != nil {
		t.Fatalf("seed lease file: %v", err)
	}
	before, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backups before reconstructive reindex: %v", err)
	}

	output := runCLI(t, dbPath, configPath, "db", "reindex", "--confirm")
	var envelope struct {
		Data struct {
			BackupRecommended bool   `json:"backup_recommended"`
			DatabasePath      string `json:"database_path"`
			BackupPath        string `json:"backup_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Data.BackupRecommended || envelope.Data.DatabasePath != dbPath || envelope.Data.BackupPath != "" {
		t.Fatalf("reconstructive success guidance = %s", output)
	}
	after, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backups after reconstructive reindex: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("backup directory changed without a recovery image: before=%v after=%v", before, after)
	}
}

func TestDBReindexConfirmedBackupFailurePreservesEvidence(t *testing.T) {
	fixture := newCLIDBFixture(t, "repair database.db")
	tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath
	execCLIDBSQL(t, dbPath, `INSERT INTO search_index(content, entity_type, entity_id, project_id) VALUES ('preserved after backup failure', 'task', CAST(999 AS TEXT), 1)`)
	stateBlocker := filepath.Join(tmp, "state-blocker")
	if err := os.WriteFile(stateBlocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write state blocker: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", stateBlocker)
	runCLIExpectError(t, dbPath, configPath, "internal_error", "db", "reindex", "--confirm")
	if got := queryCLIDBInt(t, dbPath, `SELECT COUNT(*) FROM search_index WHERE content = 'preserved after backup failure'`); got != 1 {
		t.Fatalf("destructive evidence after backup failure = %d, want 1", got)
	}
}

func execCLIDBSQL(t *testing.T, dbPath, statement string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("db.Exec(%q): %v", statement, err)
	}
}

func queryCLIDBInt(t *testing.T, dbPath, query string, args ...any) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	var value int64
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("db.QueryRow(%q): %v", query, err)
	}
	return value
}

func dbSchemaSnapshot(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT type, name, COALESCE(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatalf("schema query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var parts []string
	for rows.Next() {
		var objectType, name, definition string
		if err := rows.Scan(&objectType, &name, &definition); err != nil {
			t.Fatalf("schema scan: %v", err)
		}
		parts = append(parts, objectType+":"+name+":"+definition)
	}
	var journal string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	var migrations sql.NullString
	if err := db.QueryRow(`SELECT GROUP_CONCAT(version, ',') FROM (SELECT version FROM schema_migrations ORDER BY version)`).Scan(&migrations); err != nil {
		// A foreign database intentionally has no migration table.
		migrations.String = "<missing>"
	}
	return strings.Join(parts, "\n") + "\njournal=" + journal + "\nmigrations=" + migrations.String
}
