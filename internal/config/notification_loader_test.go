package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validNotificationYAML = `
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
animation:
  - frame: 0
    value: "X"
bubble:
  tail_side: bottom
position: center
typing_ms_per_char: 0
message_field: hint
dismiss:
  mode: key
  keys: [esc]
`

func writeNotificationFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadNotification_happyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeNotificationFile(t, dir, "kit.yaml", validNotificationYAML)
	notification, err := LoadNotification(path)
	if err != nil {
		t.Fatalf("LoadNotification: %v", err)
	}
	if notification.Name != "testkit" {
		t.Fatalf("name = %q", notification.Name)
	}
	if notification.SourcePath != path {
		t.Fatalf("SourcePath drifted: %q", notification.SourcePath)
	}
}

func TestLoadNotification_unknownField(t *testing.T) {
	dir := t.TempDir()
	body := validNotificationYAML + "\nrandom_field: 1\n"
	path := writeNotificationFile(t, dir, "kit.yaml", body)
	if _, err := LoadNotification(path); err == nil {
		t.Fatalf("unknown field should fail")
	}
}

func TestLoadNotification_validationFails(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(validNotificationYAML, "tail_side: bottom", "tail_side: elsewhere")
	path := writeNotificationFile(t, dir, "kit.yaml", body)
	if _, err := LoadNotification(path); err == nil {
		t.Fatalf("invalid tail_side should fail")
	}
}

func TestLoadNotifications_discoversDefaultsAndCustoms(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeNotificationFile(t, root, "alpha.yaml", strings.Replace(validNotificationYAML, "name: testkit", "name: alpha", 1))
	writeNotificationFile(t, filepath.Join(root, "custom"), "beta.yaml", strings.Replace(validNotificationYAML, "name: testkit", "name: beta", 1))

	notifications, _, err := LoadNotifications(root)
	if err != nil {
		t.Fatalf("LoadNotifications: %v", err)
	}
	if _, ok := notifications["alpha"]; !ok {
		t.Errorf("alpha not loaded")
	}
	if b, ok := notifications["beta"]; !ok {
		t.Errorf("beta not loaded")
	} else if !b.IsCustom {
		t.Errorf("beta should be marked custom")
	}
}

func TestLoadNotifications_customOverridesDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeNotificationFile(t, root, "shared.yaml", strings.Replace(validNotificationYAML, "name: testkit", "name: shared", 1))
	customBody := strings.Replace(validNotificationYAML, "name: testkit", "name: shared", 1)
	customBody = strings.Replace(customBody, "description: tester", "description: overridden", 1)
	writeNotificationFile(t, filepath.Join(root, "custom"), "shared.yaml", customBody)

	notifications, _, err := LoadNotifications(root)
	if err != nil {
		t.Fatalf("LoadNotifications: %v", err)
	}
	got, ok := notifications["shared"]
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

func TestLoadNotifications_missingDirOK(t *testing.T) {
	notifications, _, err := LoadNotifications(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing dir should be fine, got %v", err)
	}
	if len(notifications) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(notifications))
	}
}

// TestLoadNotifications_invalidCustomFileSurfaceWarning pins the soft-fail
// contract for custom notifications: a custom file that decodes against an
// older schema (or fails validation) is reported via SourceWarning
// and skipped instead of poisoning the entire bundle. Default-scope
// notifications still hard-fail.
func TestLoadNotifications_invalidCustomFileSurfacesWarning(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeNotificationFile(t, root, "good.yaml", strings.Replace(validNotificationYAML, "name: testkit", "name: good", 1))
	writeNotificationFile(t, filepath.Join(root, "custom"), "stale.yaml", "name: stale\nanimations:\n  idle:\n    - frame: 0\n      value: x\n")

	notifications, warnings, err := LoadNotifications(root)
	if err != nil {
		t.Fatalf("invalid custom should not fail load: %v", err)
	}
	if _, ok := notifications["good"]; !ok {
		t.Fatalf("good notification was not loaded")
	}
	if len(warnings) == 0 {
		t.Fatalf("expected a warning for the stale custom file")
	}
	if !strings.Contains(warnings[0].Path, "stale.yaml") {
		t.Fatalf("warning should cite the offending path; got %+v", warnings[0])
	}
	if !strings.Contains(warnings[0].Message, "incompatible") {
		t.Fatalf("warning message should flag schema incompatibility; got %q", warnings[0].Message)
	}
}

// TestLoadNotifications_invalidDefaultFileFails confirms default-scope
// notifications still hard-fail — defaults are kit-controlled and any
// breakage there is a bug, not user drift.
func TestLoadNotifications_invalidDefaultFileFails(t *testing.T) {
	root := t.TempDir()
	writeNotificationFile(t, root, "broken.yaml", "name: broken\nanimations:\n  idle:\n    - frame: 0\n      value: x\n")
	_, _, err := LoadNotifications(root)
	if err == nil {
		t.Fatalf("expected default-scope failure to be fatal")
	}
}

func TestLoadNotifications_duplicateAtSameScopeFails(t *testing.T) {
	root := t.TempDir()
	writeNotificationFile(t, root, "a.yaml", strings.Replace(validNotificationYAML, "name: testkit", "name: dup", 1))
	writeNotificationFile(t, root, "b.yaml", strings.Replace(validNotificationYAML, "name: testkit", "name: dup", 1))
	_, _, err := LoadNotifications(root)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}
