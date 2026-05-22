package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDBBackupCommand_DefaultPathAndOutOverride(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	// Init seeds the DB + config so the snapshot has a real source to
	// copy from and the bundle exists for retention resolution.
	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	stateRoot := filepath.Join(tmp, "state")
	t.Setenv("XDG_STATE_HOME", stateRoot)

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
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "no-such.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))

	// init writes the config under configPath but uses dbPath as the
	// requested DB — the DB file is created by init, so remove it
	// afterward to simulate a missing source for the backup attempt.
	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove db: %v", err)
	}
	runCLIExpectError(t, dbPath, configPath, "internal_error", "db", "backup")
}
