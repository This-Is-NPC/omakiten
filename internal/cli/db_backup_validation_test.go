package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDBBackupOutSystemPathReturnsValidationError pins the W4 #246
// fix: system-root + overwrite errors on `db backup --out` must
// surface as code "validation_error" via domain.NewError, not the
// generic "internal_error" fallback writeError emits for non-coded
// errors. Agents that drive backup flows match on the typed code to
// distinguish a recoverable user-input rejection from a real system
// failure.
func TestDBBackupOutSystemPathReturnsValidationError(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	envelope := runCLIExpectError(t, dbPath, configPath, "validation_error",
		"db", "backup", "--out", "/etc/forbidden/snapshot.db")
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.details missing for system-path branch: %v", envelope)
	}
	if details["path"] == nil || details["root"] == nil {
		t.Fatalf("envelope.details must carry path + root for agent matching: %v", details)
	}
}

// TestDBBackupOutExistingFileReturnsValidationError covers the second
// guard branch: --out targeting an existing file without --force must
// also surface as validation_error so the agent can distinguish
// "user typed an in-use path" from "the backup pipeline blew up".
func TestDBBackupOutExistingFileReturnsValidationError(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	existing := filepath.Join(tmp, "manual", "snapshot.db")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatalf("MkdirAll(manual): %v", err)
	}
	if err := os.WriteFile(existing, []byte("preexisting"), 0o600); err != nil {
		t.Fatalf("WriteFile(existing): %v", err)
	}

	envelope := runCLIExpectError(t, dbPath, configPath, "validation_error",
		"db", "backup", "--out", existing)
	details, ok := envelope["details"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.details missing for exists branch: %v", envelope)
	}
	if details["path"] == nil {
		t.Fatalf("envelope.details.path missing for exists branch: %v", details)
	}
}
