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
	fixture := newCLIDBFixture(t, "omakiten.db")
	dbPath, configPath := fixture.dbPath, fixture.configPath

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
	fixture := newCLIDBFixture(t, "omakiten.db")
	tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath

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

func TestDBBackupOutRejectsSymlinkedParentAndForceDestinationSymlink(t *testing.T) {
	fixture := newCLIDBFixture(t, "omakiten.db")
	tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath

	realParent := filepath.Join(tmp, "real-parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir real parent: %v", err)
	}
	linkedParent := filepath.Join(tmp, "lexically-safe-parent")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatalf("Symlink parent: %v", err)
	}
	runCLIExpectError(t, dbPath, configPath, "internal_error", "db", "backup", "--out", filepath.Join(linkedParent, "snapshot.db"))
	if _, err := os.Stat(filepath.Join(realParent, "snapshot.db")); !os.IsNotExist(err) {
		t.Fatalf("symlink-parent backup was published: %v", err)
	}

	victim := filepath.Join(tmp, "victim.db")
	if err := os.WriteFile(victim, []byte("victim"), 0o600); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	destinationLink := filepath.Join(tmp, "destination-link.db")
	if err := os.Symlink(victim, destinationLink); err != nil {
		t.Fatalf("Symlink destination: %v", err)
	}
	runCLIExpectError(t, dbPath, configPath, "internal_error", "db", "backup", "--out", destinationLink, "--force")
	if body, err := os.ReadFile(victim); err != nil || string(body) != "victim" {
		t.Fatalf("force destination symlink victim changed = %q, %v", body, err)
	}
}

func TestDBBackupOutForceReplacesOnlyRegularDestination(t *testing.T) {
	fixture := newCLIDBFixture(t, "omakiten.db")
	tmp, dbPath, configPath := fixture.root, fixture.dbPath, fixture.configPath
	destination := filepath.Join(tmp, "manual", "snapshot.db")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatalf("MkdirAll destination: %v", err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}
	runCLI(t, dbPath, configPath, "db", "backup", "--out", destination, "--force")
	if body, err := os.ReadFile(destination); err != nil || string(body) == "old" {
		t.Fatalf("force regular destination was not replaced: body=%q error=%v", body, err)
	}
}
