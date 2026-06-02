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

// TestEnsureDefaultFilesHardensFreshDirs pins the new Option-A contract for the
// active half of finding #1 + finding #2: a freshly created omakiten config
// subtree is owner-only (0o700) — the root and the per-kind custom/ folders the
// seed creates all end up 0o700 when the seed itself created them.
func TestEnsureDefaultFilesHardensFreshDirs(t *testing.T) {
	// Brand-new root the seed must create from scratch.
	root := filepath.Join(t.TempDir(), "omakiten-config")

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
			t.Fatalf("fresh owned dir %s mode = %o, want 0700", dir, got)
		}
	}
}

// TestEnsureDefaultFilesDoesNotClobberExistingRoot pins the other half of the
// Option-A contract: a pre-existing root keeps its current mode and is never
// clobbered, because --config may point at a yaml outside an omakiten layout
// whose parent (e.g. ~/.config) is a shared dir omakiten did not create. The
// custom/ subfolders the seed itself creates underneath still get 0o700.
func TestEnsureDefaultFilesDoesNotClobberExistingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "omakiten-config")
	// Simulate a pre-existing, possibly shared root at a looser mode.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("pre-create root: %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("chmod root: %v", err)
	}

	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles: %v", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat root: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("pre-existing root mode = %o, want unchanged 0755", got)
	}

	// Subfolders the seed freshly created underneath are still hardened.
	for _, sub := range []string{
		filepath.Join(root, "config", "custom"),
		filepath.Join(root, "skills", "custom"),
	} {
		subInfo, err := os.Stat(sub)
		if err != nil {
			t.Fatalf("stat %s: %v", sub, err)
		}
		if got := subInfo.Mode().Perm(); got != 0o700 {
			t.Fatalf("fresh owned subdir %s mode = %o, want 0700", sub, got)
		}
	}
}

// TestWriteAtomicLeavesNoTempResidue is the regression test for W1: after both a
// successful write and a write into a pre-existing dir, no orphan .*.tmp file is
// left behind in the target directory. The cleanup defer is now registered
// immediately after os.CreateTemp, so even an error path (e.g. a Chmod failure)
// cannot orphan the temp file.
func TestWriteAtomicLeavesNoTempResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := WriteAtomic(path, []byte("first")); err != nil {
		t.Fatalf("WriteAtomic first: %v", err)
	}
	// Second write into the now pre-existing dir (overwrite path).
	if err := WriteAtomic(path, []byte("second")); err != nil {
		t.Fatalf("WriteAtomic second: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("orphan temp file left behind: %s", e.Name())
		}
	}
}
