package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
)

// TestPresetRequiresOkt verifies a release executable older than a preset's
// requires.okt refuses to install, activate, or load it, while development
// builds and newer releases accept it.
func TestPresetRequiresOkt(t *testing.T) {
	t.Cleanup(func() { config.SetRunningVersion("") })
	p := presetFixture(t)
	p.Manifest.Requires.Okt = "0.37.0"
	root := t.TempDir()

	config.SetRunningVersion("v0.36.0")
	_, err := config.InstallPreset(root, p)
	if err == nil || !strings.Contains(err.Error(), `requires okt >= 0.37.0 (running v0.36.0)`) {
		t.Fatalf("InstallPreset on an older release error = %v, want requirement failure", err)
	}

	config.SetRunningVersion("v0.36.0-4-gabc1234-dirty")
	installed, err := config.InstallPreset(root, p)
	if err != nil {
		t.Fatalf("InstallPreset on a development build: %v", err)
	}
	if _, err := config.ActivatePreset(root, installed.ID); err != nil {
		t.Fatalf("ActivatePreset on a development build: %v", err)
	}
	selection := filepath.Join(root, config.PresetSelectionFile)

	config.SetRunningVersion("0.36.9")
	if _, err := config.LoadBundle(selection); err == nil || !strings.Contains(err.Error(), "requires okt >= 0.37.0") {
		t.Fatalf("LoadBundle on an older release error = %v, want requirement failure", err)
	}
	if _, err := config.ActivatePreset(root, installed.ID); err == nil {
		t.Fatal("ActivatePreset on an older release succeeded")
	}

	config.SetRunningVersion("0.37.0")
	if _, err := config.LoadBundle(selection); err != nil {
		t.Fatalf("LoadBundle on the required release: %v", err)
	}

	p.Manifest.Requires.Okt = ">=0.37"
	if _, err := config.InstallPreset(t.TempDir(), p); err == nil || !strings.Contains(err.Error(), "must be a version X.Y.Z") {
		t.Fatalf("InstallPreset with a malformed requirement error = %v", err)
	}
}
