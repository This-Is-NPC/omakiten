//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteAtomicCreatesFreshParentOwnerOnly verifies that when WriteAtomic has
// to create a brand-new parent directory it does so owner-only (0o700), so the
// presence of the 0o600 files inside is not leaked to other users.
func TestWriteAtomicCreatesFreshParentOwnerOnly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fresh-parent")
	path := filepath.Join(dir, "config.json")

	if err := WriteAtomic(path, []byte("{}")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("fresh parent dir mode = %o, want 0700", got)
	}
}

// TestWriteAtomicDoesNotClobberExistingParent pins the contract for finding #1:
// WriteAtomic must NOT chmod a pre-existing parent directory. ~/.claude/ is
// created by Claude Code at ~0o755 and shared with it; silently tightening it
// to 0o700 would be surprising and a source of errors. The leak being open for
// shared, pre-existing parents is by design.
func TestWriteAtomicDoesNotClobberExistingParent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod existing dir: %v", err)
	}
	path := filepath.Join(dir, ".mcp.json")

	if err := WriteAtomic(path, []byte("{}")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("pre-existing parent dir mode = %o, want unchanged 0755", got)
	}
}

// TestWriteAtomicWritesFileOwnerOnly enforces finding #3: the written file is
// 0o600 by explicit Chmod, not merely inherited from os.CreateTemp's default.
func TestWriteAtomicWritesFileOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := WriteAtomic(path, []byte("payload")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("written file mode = %o, want 0600", got)
	}
}

// TestEnsureDefaultFilesHardensOwnedDirs covers finding #2 + the active half of
// finding #1: every omakiten-owned config directory the seed creates ends up
// 0o700, including the root and the per-kind custom/ folders, even if the root
// already existed at a looser mode (older installs used 0o755).
func TestEnsureDefaultFilesHardensOwnedDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "omakiten-config")
	// Simulate an older install whose root was created world-listable.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("pre-create root: %v", err)
	}

	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}

	checkDirs := []string{
		root,
		filepath.Join(root, "config", "custom"),
		filepath.Join(root, "skills", "custom"),
		filepath.Join(root, "templates", "custom"),
	}
	for _, dir := range checkDirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("owned dir %s mode = %o, want 0700", dir, got)
		}
	}
}
