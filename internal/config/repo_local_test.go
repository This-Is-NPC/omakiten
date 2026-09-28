package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRepoLocal_HitAtStartDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, RepoLocalDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, ok, err := FindRepoLocal(root)
	if err != nil {
		t.Fatalf("FindRepoLocal: %v", err)
	}
	if !ok {
		t.Fatalf("expected hit at start dir, got miss")
	}
	if want := filepath.Join(root, RepoLocalDirName); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindRepoLocal_WalksUpFromDeepSubdir(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "src", "internal", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir deep: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, RepoLocalDirName), 0o755); err != nil {
		t.Fatalf("mkdir .omakiten: %v", err)
	}
	got, ok, err := FindRepoLocal(deep)
	if err != nil {
		t.Fatalf("FindRepoLocal: %v", err)
	}
	if !ok {
		t.Fatalf("expected walk-up hit, got miss")
	}
	if want := filepath.Join(root, RepoLocalDirName); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindRepoLocal_EmptyStartIsNoOp(t *testing.T) {
	got, ok, err := FindRepoLocal("")
	if err != nil {
		t.Fatalf("FindRepoLocal(\"\"): %v", err)
	}
	if ok || got != "" {
		t.Fatalf("expected no-op, got (%q, %v)", got, ok)
	}
}

func TestFindRepoLocal_NoHitReturnsFalse(t *testing.T) {
	root := t.TempDir()
	got, ok, err := FindRepoLocal(root)
	if err != nil {
		t.Fatalf("FindRepoLocal: %v", err)
	}
	if ok || got != "" {
		t.Fatalf("expected miss, got (%q, %v)", got, ok)
	}
}

func TestFindRepoLocal_FileNotDirIsMiss(t *testing.T) {
	root := t.TempDir()
	// Plant a file (not a directory) with the magic name; walker must skip.
	if err := os.WriteFile(filepath.Join(root, RepoLocalDirName), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, ok, err := FindRepoLocal(root)
	if err != nil {
		t.Fatalf("FindRepoLocal: %v", err)
	}
	if ok {
		t.Fatalf("expected miss when name is a file, got hit")
	}
}

func TestFindRepoLocal_RejectsSymlinkedRoot(t *testing.T) {
	parent := t.TempDir()
	repoA := filepath.Join(parent, "repo-a")
	repoB := filepath.Join(parent, "repo-b")
	if err := os.MkdirAll(filepath.Join(repoA, "src"), 0o755); err != nil {
		t.Fatalf("mkdir repo A: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoB, RepoLocalDirName), 0o755); err != nil {
		t.Fatalf("mkdir repo B: %v", err)
	}
	if err := os.Symlink(filepath.Join(repoB, RepoLocalDirName), filepath.Join(repoA, RepoLocalDirName)); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	_, ok, err := FindRepoLocal(filepath.Join(repoA, "src"))
	if err == nil || ok {
		t.Fatalf("FindRepoLocal through symlinked root = (%v, %v), want rejection", ok, err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("FindRepoLocal error = %q, want symlink detail", err)
	}
}

func TestFindRepoLocal_RejectsSymlinkedIntermediateComponent(t *testing.T) {
	parent := t.TempDir()
	realRepo := filepath.Join(parent, "real-repo")
	alias := filepath.Join(parent, "alias")
	if err := os.MkdirAll(filepath.Join(realRepo, "src", "pkg", RepoLocalDirName), 0o755); err != nil {
		t.Fatalf("mkdir real repo: %v", err)
	}
	if err := os.Symlink(realRepo, alias); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	_, ok, err := FindRepoLocal(filepath.Join(alias, "src", "pkg"))
	if err == nil || ok {
		t.Fatalf("FindRepoLocal through intermediate symlink = (%v, %v), want rejection", ok, err)
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("FindRepoLocal error = %q, want symlink detail", err)
	}
}
