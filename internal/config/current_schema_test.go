package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleRejectsLegacyLayoutWithoutMovingInput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "omakase.yaml")
	want := []byte("version: 1\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write legacy profile: %v", err)
	}

	_, err := LoadBundle(path)
	if err == nil || !strings.Contains(err.Error(), "outside the current config") {
		t.Fatalf("LoadBundle() error = %v, want current-layout rejection", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read legacy profile: %v", readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy profile changed: want %q, got %q", want, got)
	}
}

func TestLoadBundleRejectsLegacyEntityDirectoryWithoutMovingInput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "config", "omakase.yaml")
	legacyPath := filepath.Join(root, "config", "skills", "go.md")
	want := []byte("---\nname: Go\n---\nlegacy\n")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy entity directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatalf("write current profile: %v", err)
	}
	if err := os.WriteFile(legacyPath, want, 0o600); err != nil {
		t.Fatalf("write legacy entity: %v", err)
	}

	_, err := LoadBundle(path)
	if err == nil || !strings.Contains(err.Error(), "legacy entity directory") {
		t.Fatalf("LoadBundle() error = %v, want nested-layout rejection", err)
	}
	got, readErr := os.ReadFile(legacyPath)
	if readErr != nil {
		t.Fatalf("read legacy entity: %v", readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy entity changed: want %q, got %q", want, got)
	}
}

func TestLoadSkillsRejectsLegacySchemaWithoutRewriting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "old.md")
	want := []byte("---\nname: Old\n---\nlegacy\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write legacy skill: %v", err)
	}

	_, _, err := LoadSkills(dir)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("LoadSkills() error = %v, want schema rejection", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read legacy skill: %v", readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy skill changed: want %q, got %q", want, got)
	}
}

func TestLoadPersonasRejectsLegacySchemaWithoutRewriting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "old.md")
	want := []byte("---\nname: Old\nschema_version: 1\n---\nlegacy\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write legacy persona: %v", err)
	}

	_, _, err := LoadPersonas(dir)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("LoadPersonas() error = %v, want schema rejection", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read legacy persona: %v", readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy persona changed: want %q, got %q", want, got)
	}
}

func TestLoadBundleRejectsUnsupportedPersonaWiringSchemaWithoutRewriting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "config", "omakase.yaml")
	want := []byte("version: 1\npersonas:\n  - slug: old\n    schema_version: 1\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("write legacy wiring: %v", err)
	}

	_, err := LoadBundle(path)
	if err == nil || !strings.Contains(err.Error(), "personas.old schema_version") {
		t.Fatalf("LoadBundle() error = %v, want wiring schema rejection", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read legacy wiring: %v", readErr)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("legacy wiring changed: want %q, got %q", want, got)
	}
}
