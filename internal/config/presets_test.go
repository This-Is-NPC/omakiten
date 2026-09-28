package config

import (
	"path/filepath"
	"testing"
)

func TestOfficialPresetsCopyAndValidate(t *testing.T) {
	for _, preset := range ListPresets() {
		t.Run(preset.Name, func(t *testing.T) {
			assertOfficialPreset(t, preset)
		})
	}
}

func assertOfficialPreset(t *testing.T, preset Preset) {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".omakiten")
	path := filepath.Join(root, "config", preset.Name+".yaml")
	if _, err := SeedInstall(root, preset.Name, false); err != nil {
		t.Fatalf("SeedManagedProfile: %v", err)
	}
	// Materialize the embedded entity defaults next to the preset.
	// omakase ships full commands + persona wiring (it doubles
	// as the canonical kit), so its refs need matching .md files
	// to resolve; the other presets work either way.
	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
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
}
