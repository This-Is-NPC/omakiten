package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"omakiten/internal/sqlite"
)

func TestConfigValidateMigrateAcceptsExactV030DatabaseWithoutReadingConfig(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	wantConfig := []byte("this is intentionally invalid legacy config: [\n")
	if err := os.WriteFile(configPath, wantConfig, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	seedExactV030DatabaseForCLI(t, dbPath)

	beforeDB, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile(before db): %v", err)
	}
	beforeSidecars := cliDatabaseSidecarSnapshot(t, dbPath)

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--db", dbPath, "config", "validate", "--migrate", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, output = %s", err, out.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, output = %s", err, out.String())
	}
	if envelope["ok"] != true {
		t.Fatalf("ok = %v, output = %s", envelope["ok"], out.String())
	}
	if got, err := os.ReadFile(configPath); err != nil {
		t.Fatalf("ReadFile(config): %v", err)
	} else if !bytes.Equal(got, wantConfig) {
		t.Fatalf("transition changed invalid legacy config: want %q, got %q", wantConfig, got)
	}
	if got, err := os.ReadFile(dbPath); err != nil {
		t.Fatalf("ReadFile(after db): %v", err)
	} else if !bytes.Equal(got, beforeDB) {
		t.Fatal("successful transition validation changed the source database")
	}
	if got := cliDatabaseSidecarSnapshot(t, dbPath); got != beforeSidecars {
		t.Fatalf("successful transition validation changed database sidecars:\nbefore=%safter=%s", beforeSidecars, got)
	}
}

func TestConfigValidateMigrateSubprocessSuccessAndFailure(t *testing.T) {
	for _, outcome := range []struct {
		name       string
		configPath func(string) string
		wantOK     bool
	}{
		{name: "exact official path succeeds", configPath: func(root string) string {
			return filepath.Join(root, "config", "omakase.yaml")
		}, wantOK: true},
		{name: "custom path fails", configPath: func(root string) string {
			return filepath.Join(root, "custom.yaml")
		}, wantOK: false},
	} {
		outcome := outcome
		t.Run(outcome.name, func(t *testing.T) { runConfigValidateMigrateSubprocess(t, outcome.configPath, outcome.wantOK) })
	}
}

func runConfigValidateMigrateSubprocess(t *testing.T, configPath func(string) string, wantOK bool) {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "omakiten.db")
	resolvedConfigPath := configPath(root)
	if err := os.MkdirAll(filepath.Dir(resolvedConfigPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := os.WriteFile(resolvedConfigPath, []byte("invalid legacy config: [\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	seedExactV030DatabaseForCLI(t, dbPath)
	outputPath := filepath.Join(root, "child-output.json")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestConfigValidateMigrateSubprocessChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		"OKT_HISTORICAL_VALIDATE_CHILD=1",
		"OKT_HISTORICAL_DB="+dbPath,
		"OKT_HISTORICAL_CONFIG="+resolvedConfigPath,
		"OKT_HISTORICAL_OUTPUT="+outputPath,
	)
	err := cmd.Run()
	if wantOK && err != nil {
		t.Fatalf("historical validation subprocess: %v", err)
	}
	if !wantOK && err == nil {
		t.Fatal("historical validation subprocess accepted a custom path")
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(child output): %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output), &envelope); err != nil {
		t.Fatalf("child output is not JSON: %v, output=%s", err, output)
	}
	if envelope["ok"] != wantOK {
		t.Fatalf("child ok=%v, want=%t; output=%s", envelope["ok"], wantOK, output)
	}
}

func TestConfigValidateMigrateSubprocessChild(t *testing.T) {
	if os.Getenv("OKT_HISTORICAL_VALIDATE_CHILD") != "1" {
		return
	}
	outputPath := os.Getenv("OKT_HISTORICAL_OUTPUT")
	output, err := os.Create(outputPath)
	if err != nil {
		os.Exit(2)
	}
	defer func() { _ = output.Close() }()
	cmd := NewRootCommand("test")
	cmd.SetOut(output)
	cmd.SetArgs([]string{"--db", os.Getenv("OKT_HISTORICAL_DB"), "config", "validate", "--migrate", "--config", os.Getenv("OKT_HISTORICAL_CONFIG")})
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func seedExactV030DatabaseForCLI(t *testing.T, path string) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("sqlite.Open(): %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("sqlite.Close(): %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	for _, version := range []string{
		"001_initial.sql", "002_entities.sql", "003_activity_logs.sql", "004_tags.sql",
		"005_transition_guards.sql", "006_comment_tags.sql", "007_errors.sql", "008_solution_likes.sql",
		"009_events.sql", "010_agent_attribution.sql", "011_purge_tui_summary_pollution.sql", "012_task_state.sql",
		"013_bucket_permissions_operations.sql", "014_workflow_defaults.sql", "015_priority_id.sql", "016_severity_id.sql",
		"017_drop_priority_severity_defaults.sql", "018_drop_legacy_event_payloads.sql", "019_unify_tool_call_events.sql",
		"020_drop_config_tables.sql", "021_rebind_orphan_buckets.sql", "022_search_index.sql", "023_plans.sql",
		"024_search_index_plans.sql", "025_projects_cascade.sql", "026_tasks_parent_id.sql", "027_tasks_parent_project_fk.sql",
		"028_tasks_depth.sql", "029_repair_tasks_depth.sql", "030_rename_errors_researched.sql", "031_notes.sql",
		"032_events_comment_log.sql", "033_drop_context_entries.sql", "034_realtime_read_path_indexes.sql",
		"035_events_order_by_indexes.sql",
	} {
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, version); err != nil {
			t.Fatalf("migration %s: %v", version, err)
		}
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("checkpoint exact v0.30 fixture: %v", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = DELETE`); err != nil {
		t.Fatalf("restore exact v0.30 journal mode: %v", err)
	}
}

func cliDatabaseSidecarSnapshot(t *testing.T, path string) string {
	t.Helper()
	var snapshot bytes.Buffer
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				snapshot.WriteString(suffix + ":missing\n")
				continue
			}
			t.Fatalf("ReadFile(%s): %v", path+suffix, err)
		}
		_, _ = fmt.Fprintf(&snapshot, "%s:present:%x\n", suffix, sha256.Sum256(data))
	}
	return snapshot.String()
}
