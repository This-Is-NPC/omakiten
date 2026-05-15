package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedPreset_LocalCreatesLibraryEntryAtRepoLocal(t *testing.T) {
	repoRoot := t.TempDir()
	res, err := SeedPreset(ScopeLocal, "omakase", false, SeedOptions{LocalRoot: repoRoot})
	if err != nil {
		t.Fatalf("SeedPreset: %v", err)
	}
	if res.NoOp {
		t.Fatalf("expected initial write, got NoOp")
	}
	wantPath := filepath.Join(repoRoot, RepoLocalDirName, "config", "omakase.yaml")
	if res.Path != wantPath {
		t.Fatalf("path = %q, want %q", res.Path, wantPath)
	}
	active, err := os.ReadFile(filepath.Join(repoRoot, RepoLocalDirName, "config", ".active"))
	if err != nil {
		t.Fatalf("read .active: %v", err)
	}
	if got := strings.TrimSpace(string(active)); got != "omakase.yaml" {
		t.Fatalf(".active = %q, want omakase.yaml", got)
	}
}

func TestSeedPreset_GlobalCreatesAtGlobalRoot(t *testing.T) {
	root := t.TempDir()
	res, err := SeedPreset(ScopeGlobal, "kaiseki", false, SeedOptions{GlobalRoot: root})
	if err != nil {
		t.Fatalf("SeedPreset: %v", err)
	}
	wantPath := filepath.Join(root, "config", "kaiseki.yaml")
	if res.Path != wantPath {
		t.Fatalf("path = %q, want %q", res.Path, wantPath)
	}
	active, err := os.ReadFile(filepath.Join(root, "config", ".active"))
	if err != nil {
		t.Fatalf("read .active: %v", err)
	}
	if got := strings.TrimSpace(string(active)); got != "kaiseki.yaml" {
		t.Fatalf(".active = %q, want kaiseki.yaml", got)
	}
}

func TestSeedPreset_NoOpWhenContentMatches(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedPreset(ScopeGlobal, "omakase", false, SeedOptions{GlobalRoot: root}); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	res, err := SeedPreset(ScopeGlobal, "omakase", false, SeedOptions{GlobalRoot: root})
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if !res.NoOp {
		t.Fatalf("expected NoOp on identical content, got %+v", res)
	}
}

func TestSeedPreset_ErrorWhenDivergesWithoutForce(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedPreset(ScopeGlobal, "omakase", false, SeedOptions{GlobalRoot: root}); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	p := filepath.Join(root, "config", "omakase.yaml")
	if err := os.WriteFile(p, []byte("version: 1\n# tampered by test\n"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	_, err := SeedPreset(ScopeGlobal, "omakase", false, SeedOptions{GlobalRoot: root})
	if !errors.Is(err, ErrPresetTargetExists) {
		t.Fatalf("err = %v, want ErrPresetTargetExists", err)
	}
}

func TestSeedPreset_ForceOverwritesDivergent(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedPreset(ScopeGlobal, "omakase", false, SeedOptions{GlobalRoot: root}); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	p := filepath.Join(root, "config", "omakase.yaml")
	if err := os.WriteFile(p, []byte("tampered\n"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	res, err := SeedPreset(ScopeGlobal, "omakase", true, SeedOptions{GlobalRoot: root})
	if err != nil {
		t.Fatalf("force seed: %v", err)
	}
	if res.NoOp {
		t.Fatalf("force overwrite should not report NoOp")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) == "tampered\n" {
		t.Fatalf("file not overwritten")
	}
}

func TestSeedPreset_RejectsUnknownScope(t *testing.T) {
	_, err := SeedPreset("weird", "omakase", false, SeedOptions{GlobalRoot: t.TempDir()})
	if err == nil {
		t.Fatalf("expected error for unknown scope")
	}
}

func TestSeedPreset_RequiresMatchingRoot(t *testing.T) {
	if _, err := SeedPreset(ScopeGlobal, "omakase", false, SeedOptions{LocalRoot: t.TempDir()}); err == nil {
		t.Fatalf("expected error: GlobalRoot missing for global scope")
	}
	if _, err := SeedPreset(ScopeLocal, "omakase", false, SeedOptions{GlobalRoot: t.TempDir()}); err == nil {
		t.Fatalf("expected error: LocalRoot missing for local scope")
	}
}

func TestSeedPreset_RejectsUnknownName(t *testing.T) {
	_, err := SeedPreset(ScopeLocal, "no-such-preset", false, SeedOptions{LocalRoot: t.TempDir()})
	if !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("err = %v, want ErrPresetNotFound", err)
	}
}
