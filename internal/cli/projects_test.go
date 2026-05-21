package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectsDeleteCommand_WithYesSkipsPromptAndWritesBackup wires
// init + a tiny task seed, then runs `projects delete <slug> --yes`
// and asserts (a) the JSON envelope reports the new counters, (b)
// the backup file lands under XDG_STATE_HOME/omakiten/backups/, and
// (c) the project row is gone from the DB.
func TestProjectsDeleteCommand_WithYesSkipsPromptAndWritesBackup(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))

	runCLI(t, dbPath, configPath, "init", "--name", "Doomed", "--slug", "doomed")
	runCLI(t, dbPath, configPath, "add", "-t", "Stay alive")

	output := runCLI(t, dbPath, configPath, "projects", "delete", "doomed", "--yes")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v (%s)", err, output)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.data missing: %v", envelope)
	}
	backupPath, ok := data["backup_path"].(string)
	if !ok || backupPath == "" {
		t.Fatalf("envelope.data.backup_path missing: %v", data)
	}
	if !strings.Contains(backupPath, "backups") {
		t.Fatalf("backup_path = %q, want backups subdir", backupPath)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file missing on disk: %v", err)
	}
	counters, ok := data["counters"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.data.counters missing: %v", data)
	}
	if counters["tasks"].(float64) < 1 {
		t.Fatalf("counters.tasks = %v, want >= 1 (seeded one task)", counters["tasks"])
	}

	// Project gone — re-running the delete must fail.
	runCLIExpectError(t, dbPath, configPath, "project_not_found", "projects", "delete", "doomed", "--yes")
}

func TestProjectsDeleteCommand_PipedWithoutYesErrors(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))

	runCLI(t, dbPath, configPath, "init", "--name", "Pipe", "--slug", "pipe")

	// runCLI feeds no stdin and the test harness has no TTY → the
	// command must surface validation_error rather than blocking on a
	// read.
	envelope := runCLIExpectError(t, dbPath, configPath, "validation_error", "projects", "delete", "pipe")
	if msg, ok := envelope["msg"].(string); !ok || !strings.Contains(msg, "interactive confirmation") {
		t.Fatalf("validation_error msg = %v, want \"interactive confirmation\" hint", envelope["msg"])
	}
}

func TestProjectsDeleteCommand_NotFound(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))

	runCLI(t, dbPath, configPath, "init", "--name", "Only", "--slug", "only")
	runCLIExpectError(t, dbPath, configPath, "project_not_found", "projects", "delete", "nonexistent", "--yes")
}
