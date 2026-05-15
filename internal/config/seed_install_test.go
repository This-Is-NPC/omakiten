package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedInstall_MaterializesFullInstall(t *testing.T) {
	root := t.TempDir()
	res, err := SeedInstall(root, "omakase", false)
	if err != nil {
		t.Fatalf("SeedInstall: %v", err)
	}
	if res.NoOp {
		t.Fatalf("expected fresh install, got NoOp")
	}
	wantYaml := filepath.Join(root, "config", "omakase.yaml")
	if res.Path != wantYaml {
		t.Fatalf("path = %q, want %q", res.Path, wantYaml)
	}
	if _, err := os.Stat(wantYaml); err != nil {
		t.Fatalf("preset yaml not materialized: %v", err)
	}
	// Entity folders + their custom/ subdirs must exist so the install is
	// a complete config root.
	for _, sub := range []string{"skills", "laws", "personas", "templates", "themes", "notifications"} {
		if _, err := os.Stat(filepath.Join(root, sub)); err != nil {
			t.Fatalf("missing entity folder %s: %v", sub, err)
		}
		if _, err := os.Stat(filepath.Join(root, sub, "custom")); err != nil {
			t.Fatalf("missing %s/custom: %v", sub, err)
		}
	}
	// Other preset profiles also ship so the user can flip .active later.
	for _, alt := range []string{"izakaya", "kaiseki", "shokunin"} {
		if _, err := os.Stat(filepath.Join(root, "config", alt+".yaml")); err != nil {
			t.Fatalf("expected alt preset %s materialised: %v", alt, err)
		}
	}
	active, err := os.ReadFile(filepath.Join(root, "config", ".active"))
	if err != nil {
		t.Fatalf("read .active: %v", err)
	}
	if got := strings.TrimSpace(string(active)); got != "omakase.yaml" {
		t.Fatalf(".active = %q, want omakase.yaml", got)
	}
}

func TestSeedInstall_LoadableViaLoadBundle(t *testing.T) {
	root := t.TempDir()
	res, err := SeedInstall(root, "izakaya", false)
	if err != nil {
		t.Fatalf("SeedInstall: %v", err)
	}
	// The materialised install is a valid input to LoadBundle — proves the
	// .omakiten/ tree is self-contained without any merge step.
	bundle, err := LoadBundle(res.Path)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if bundle.Kit.Key != "izakaya" {
		t.Fatalf("bundle.Kit.Key = %q, want izakaya", bundle.Kit.Key)
	}
}

func TestSeedInstall_NoOpWhenAlreadyActive(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedInstall(root, "kaiseki", false); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	res, err := SeedInstall(root, "kaiseki", false)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if !res.NoOp {
		t.Fatalf("expected NoOp on rerun, got %+v", res)
	}
}

func TestSeedInstall_SwitchesActiveOnDifferentPreset(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedInstall(root, "omakase", false); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if _, err := SeedInstall(root, "izakaya", false); err != nil {
		t.Fatalf("switch: %v", err)
	}
	active, err := os.ReadFile(filepath.Join(root, "config", ".active"))
	if err != nil {
		t.Fatalf("read .active: %v", err)
	}
	if got := strings.TrimSpace(string(active)); got != "izakaya.yaml" {
		t.Fatalf(".active = %q, want izakaya.yaml after switch", got)
	}
}

func TestSeedInstall_ForceRefreshesShippedFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := SeedInstall(root, "omakase", false); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	// Tamper a shipped file; without --force the next seed leaves it alone,
	// with --force the embedded version is restored.
	path := filepath.Join(root, "skills", "markdown.md")
	if err := os.WriteFile(path, []byte("# tampered\n"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := SeedInstall(root, "omakase", false); err != nil {
		t.Fatalf("idempotent seed: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "# tampered\n" {
		t.Fatalf("non-force seed should preserve tampered file; got = %q", got)
	}
	if _, err := SeedInstall(root, "omakase", true); err != nil {
		t.Fatalf("forced seed: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after force: %v", err)
	}
	if string(got) == "# tampered\n" {
		t.Fatalf("forced seed should restore embedded file, still tampered")
	}
}

func TestSeedInstall_RejectsUnknownPreset(t *testing.T) {
	_, err := SeedInstall(t.TempDir(), "no-such-preset", false)
	if !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("err = %v, want ErrPresetNotFound", err)
	}
}
