package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialPresetsCopyAndValidate(t *testing.T) {
	for _, preset := range ListPresets() {
		t.Run(preset.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".omakiten")
			copied, path, err := CopyPreset(preset.Name, root, false)
			if err != nil {
				t.Fatalf("CopyPreset() error = %v", err)
			}
			if copied.Name != preset.Name {
				t.Fatalf("CopyPreset() preset = %q, want %q", copied.Name, preset.Name)
			}
			if filepath.Base(path) != "omakiten.yaml" || filepath.Base(filepath.Dir(path)) != "config" {
				t.Fatalf("CopyPreset() path = %q, want config/omakiten.yaml", path)
			}

			bundle, err := LoadBundle(path)
			if err != nil {
				t.Fatalf("LoadBundle(%s) error = %v", path, err)
			}
			if bundle.Kit.Key != preset.Name {
				t.Fatalf("bundle.Kit.Key = %q, want %q", bundle.Kit.Key, preset.Name)
			}
			if bundle.Config.Workflow.Active != preset.Name {
				t.Fatalf("active workflow = %q, want %q", bundle.Config.Workflow.Active, preset.Name)
			}
		})
	}
}

func TestCopyPresetRefusesOverwriteWithoutForce(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".omakiten")
	if _, _, err := CopyPreset("izakaya", root, false); err != nil {
		t.Fatalf("CopyPreset() initial error = %v", err)
	}
	if _, _, err := CopyPreset("omakase", root, false); !errors.Is(err, ErrPresetTargetExists) {
		t.Fatalf("CopyPreset() overwrite error = %v, want ErrPresetTargetExists", err)
	}
	if _, _, err := CopyPreset("omakase", root, true); err != nil {
		t.Fatalf("CopyPreset() forced overwrite error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "config", "omakiten.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); !containsAll(got, "key: omakase", "Omakase Workflow") {
		t.Fatalf("forced preset content = %q, want omakase config", got)
	}
}

func TestCopyPresetRejectsUnknownName(t *testing.T) {
	_, _, err := CopyPreset("unknown", filepath.Join(t.TempDir(), ".omakiten"), false)
	if !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("CopyPreset() error = %v, want ErrPresetNotFound", err)
	}
}

func containsAll(s string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
}
