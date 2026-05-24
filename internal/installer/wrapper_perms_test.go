package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInstallWrapperWritesTightModeBits pins the perm-hardening fix:
// the rc file lands at 0o600 and its parent dir (when InstallWrapper
// has to create it) at 0o700. Shell wiring + PATH overrides are
// per-user material; widening these to 0o644 / 0o755 leaks them to
// every account on a shared box.
func TestInstallWrapperWritesTightModeBits(t *testing.T) {
	home := t.TempDir()
	subdir := filepath.Join(home, "config", "shell")
	rc := filepath.Join(subdir, "rc")
	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("InstallWrapper: %v", err)
	}
	dirInfo, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("stat subdir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("subdir perm = %o, want 700", perm)
	}
	fileInfo, err := os.Stat(rc)
	if err != nil {
		t.Fatalf("stat rc: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("rc perm = %o, want 600", perm)
	}
}

// TestRemoveWrapperPreservesTightModeBits asserts the strip path
// (RemoveWrapper) rewrites the rc file at 0o600 instead of silently
// widening to 0o644 on round-trip.
func TestRemoveWrapperPreservesTightModeBits(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, "rc")
	if err := InstallWrapper(rc); err != nil {
		t.Fatalf("InstallWrapper: %v", err)
	}
	// Add a trailing line so RemoveWrapper has to rewrite the file
	// (RemoveWrapper short-circuits on a no-op strip).
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(rc, append(body, []byte("# extra\n")...), 0o600); err != nil {
		t.Fatalf("seed extra line: %v", err)
	}
	removed, err := RemoveWrapper(rc)
	if err != nil {
		t.Fatalf("RemoveWrapper: %v", err)
	}
	if !removed {
		t.Fatalf("RemoveWrapper returned removed=false; expected the wrapper block to be present")
	}
	info, err := os.Stat(rc)
	if err != nil {
		t.Fatalf("stat after remove: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("rc perm after remove = %o, want 600", perm)
	}
}
