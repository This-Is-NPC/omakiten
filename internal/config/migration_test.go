package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLayoutMovesYAMLIntoConfigSubdir(t *testing.T) {
	tmp := t.TempDir()
	yaml := filepath.Join(tmp, "omakiten.yaml")
	writeFile(t, yaml, baseConfigHeader)

	if err := MigrateLayout(tmp); err != nil {
		t.Fatalf("MigrateLayout() error = %v", err)
	}

	if _, err := os.Stat(yaml); !os.IsNotExist(err) {
		t.Fatalf("legacy omakiten.yaml still exists: err=%v", err)
	}
	moved := filepath.Join(tmp, "config", "omakiten.yaml")
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("config/omakiten.yaml missing after migration: %v", err)
	}
}

func TestMigrateLayoutMovesEntityFoldersOutOfConfig(t *testing.T) {
	tmp := t.TempDir()
	// v1 layout: yaml + entity dirs all under <root>/config/.
	writeFile(t, filepath.Join(tmp, "config", "omakiten.yaml"), baseConfigHeader)
	writeFile(t, filepath.Join(tmp, "config", "skills", "implementation.md"), "---\nname: Implementation\n---\nbody\n")
	writeFile(t, filepath.Join(tmp, "config", "skills", "user-skill.md"), "---\nname: Mine\n---\nbody\n")

	if err := MigrateLayout(tmp); err != nil {
		t.Fatalf("MigrateLayout() error = %v", err)
	}

	// Entities moved to <root>/skills/.
	if _, err := os.Stat(filepath.Join(tmp, "skills", "implementation.md")); err != nil {
		t.Fatalf("skills/go.md not at new location: %v", err)
	}
	// Legacy nested folder is gone.
	if _, err := os.Stat(filepath.Join(tmp, "config", "skills")); !os.IsNotExist(err) {
		t.Fatalf("legacy config/skills/ still exists: %v", err)
	}
}

func TestMigrateLayoutSegregatesUserCustoms(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "config", "omakiten.yaml"), baseConfigHeader)
	// `implementation.md` ships in the embedded defaults, so it stays at the root.
	writeFile(t, filepath.Join(tmp, "skills", "implementation.md"), "---\nname: Implementation\n---\nbody\n")
	// `user-skill.md` is not in the embed → user-created → must move to custom/.
	writeFile(t, filepath.Join(tmp, "skills", "user-skill.md"), "---\nname: Mine\n---\nbody\n")

	if err := MigrateLayout(tmp); err != nil {
		t.Fatalf("MigrateLayout() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "skills", "implementation.md")); err != nil {
		t.Fatalf("skills/go.md must stay at root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "skills", "custom", "user-skill.md")); err != nil {
		t.Fatalf("user-skill.md should have moved to custom/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "skills", "user-skill.md")); !os.IsNotExist(err) {
		t.Fatalf("user-skill.md still at root after migration: %v", err)
	}
}

func TestMigrateLayoutMovesUserConfigProfilesToCustom(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "config", "omakiten.yaml"), baseConfigHeader)
	// User-authored profile that lived flat under config/.
	writeFile(t, filepath.Join(tmp, "config", "config-experiment.yaml"), "# user profile\n")

	if err := MigrateLayout(tmp); err != nil {
		t.Fatalf("MigrateLayout() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "config", "omakiten.yaml")); err != nil {
		t.Fatalf("default omakiten.yaml must stay at config/ root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "config-experiment.yaml")); !os.IsNotExist(err) {
		t.Fatalf("user profile still at config/ root after migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "custom", "config-experiment.yaml")); err != nil {
		t.Fatalf("user profile not relocated to custom/: %v", err)
	}
}

func TestMigrateLayoutCreatesEmptyConfigCustomDir(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "config", "omakiten.yaml"), baseConfigHeader)

	if err := MigrateLayout(tmp); err != nil {
		t.Fatalf("MigrateLayout() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "custom")); err != nil {
		t.Fatalf("config/custom dir missing: %v", err)
	}
}

func TestMigrateLayoutIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "config", "omakiten.yaml"), baseConfigHeader)
	writeFile(t, filepath.Join(tmp, "skills", "implementation.md"), "---\nname: Implementation\n---\nbody\n")

	for i := 0; i < 3; i++ {
		if err := MigrateLayout(tmp); err != nil {
			t.Fatalf("MigrateLayout() iteration %d error = %v", i, err)
		}
	}

	// File is still at the root and a custom dir exists.
	if _, err := os.Stat(filepath.Join(tmp, "skills", "implementation.md")); err != nil {
		t.Fatalf("skills/go.md disappeared after repeated migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "skills", "custom")); err != nil {
		t.Fatalf("skills/custom missing after repeated migration: %v", err)
	}
}
