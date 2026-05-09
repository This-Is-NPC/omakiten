package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validBuddyYAML = `
name: testkit
description: tester
size:
  width: 16
  height: 4
background: transparent
frame_interval_ms: 200
style: rounded
border:
  visible: true
  width: 1
  color: "#ffffff"
animations:
  idle:
    - frame: 0
      value: "X"
bubble:
  tail_side: bottom
`

func writeBuddyFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadBuddy_happyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeBuddyFile(t, dir, "kit.yaml", validBuddyYAML)
	buddy, err := LoadBuddy(path)
	if err != nil {
		t.Fatalf("LoadBuddy: %v", err)
	}
	if buddy.Name != "testkit" {
		t.Fatalf("name = %q", buddy.Name)
	}
	if buddy.SourcePath != path {
		t.Fatalf("SourcePath drifted: %q", buddy.SourcePath)
	}
}

func TestLoadBuddy_unknownField(t *testing.T) {
	dir := t.TempDir()
	body := validBuddyYAML + "\nrandom_field: 1\n"
	path := writeBuddyFile(t, dir, "kit.yaml", body)
	if _, err := LoadBuddy(path); err == nil {
		t.Fatalf("unknown field should fail")
	}
}

func TestLoadBuddy_validationFails(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(validBuddyYAML, "tail_side: bottom", "tail_side: elsewhere")
	path := writeBuddyFile(t, dir, "kit.yaml", body)
	if _, err := LoadBuddy(path); err == nil {
		t.Fatalf("invalid tail_side should fail")
	}
}

func TestLoadBuddies_discoversDefaultsAndCustoms(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBuddyFile(t, root, "alpha.yaml", strings.Replace(validBuddyYAML, "name: testkit", "name: alpha", 1))
	writeBuddyFile(t, filepath.Join(root, "custom"), "beta.yaml", strings.Replace(validBuddyYAML, "name: testkit", "name: beta", 1))

	buddies, err := LoadBuddies(root)
	if err != nil {
		t.Fatalf("LoadBuddies: %v", err)
	}
	if _, ok := buddies["alpha"]; !ok {
		t.Errorf("alpha not loaded")
	}
	if b, ok := buddies["beta"]; !ok {
		t.Errorf("beta not loaded")
	} else if !b.IsCustom {
		t.Errorf("beta should be marked custom")
	}
}

func TestLoadBuddies_customOverridesDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBuddyFile(t, root, "shared.yaml", strings.Replace(validBuddyYAML, "name: testkit", "name: shared", 1))
	customBody := strings.Replace(validBuddyYAML, "name: testkit", "name: shared", 1)
	customBody = strings.Replace(customBody, "description: tester", "description: overridden", 1)
	writeBuddyFile(t, filepath.Join(root, "custom"), "shared.yaml", customBody)

	buddies, err := LoadBuddies(root)
	if err != nil {
		t.Fatalf("LoadBuddies: %v", err)
	}
	got, ok := buddies["shared"]
	if !ok {
		t.Fatalf("shared not loaded")
	}
	if got.Description != "overridden" {
		t.Fatalf("custom did not override; got description=%q", got.Description)
	}
	if !got.IsCustom {
		t.Fatalf("expected IsCustom=true after override")
	}
}

func TestLoadBuddies_missingDirOK(t *testing.T) {
	buddies, err := LoadBuddies(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing dir should be fine, got %v", err)
	}
	if len(buddies) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(buddies))
	}
}

func TestLoadBuddies_duplicateAtSameScopeFails(t *testing.T) {
	root := t.TempDir()
	writeBuddyFile(t, root, "a.yaml", strings.Replace(validBuddyYAML, "name: testkit", "name: dup", 1))
	writeBuddyFile(t, root, "b.yaml", strings.Replace(validBuddyYAML, "name: testkit", "name: dup", 1))
	_, err := LoadBuddies(root)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
