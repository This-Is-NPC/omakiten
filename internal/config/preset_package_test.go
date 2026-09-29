package config_test

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
)

func TestPresetPublicationRejectsInvalidCandidates(t *testing.T) {
	t.Parallel()
	p := presetFixture(t)
	root := t.TempDir()
	installed, err := config.InstallPreset(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.ActivatePreset(root, installed.ID); err != nil {
		t.Fatal(err)
	}
	selection := filepath.Join(root, config.PresetSelectionFile)
	before, err := os.ReadFile(selection)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*config.PresetPackage){
		"unknown schema": func(p *config.PresetPackage) { p.Manifest.SchemaVersion++ },
		"missing config": func(p *config.PresetPackage) { delete(p.Files, p.Manifest.Config) },
		"outside path":   func(p *config.PresetPackage) { p.Files["../outside"] = config.PresetFile{} },
		"aliased path":   func(p *config.PresetPackage) { p.Files["scripts/../outside"] = config.PresetFile{} },
		"dangling reference": func(p *config.PresetPackage) {
			p.Files[p.Manifest.Config] = config.PresetFile{Content: "config: {theme: {active: absent}}", Mode: 0o644}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := p
			candidate.Files = maps.Clone(p.Files)
			mutate(&candidate)
			if _, err := config.InstallPreset(root, candidate); err == nil {
				t.Fatal("invalid candidate was installed")
			}
			assertPresetSelection(t, selection, before)
		})
	}
	if err := config.EditBundle(selection, func(path string) error { return os.WriteFile(path, []byte("unsupported: true\n"), 0o644) }); err == nil {
		t.Fatal("invalid edit was published")
	}
	assertPresetSelection(t, selection, before)
	if _, err := config.ActivatePreset(root, "missing"); err == nil {
		t.Fatal("unknown preset was activated")
	}
}

func TestPresetSharedEditorProtectsSnapshotsAndStalePlans(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	p := presetFixture(t)
	settings := p.Files["config/settings.yaml"]
	settings.Content = "# Project settings\n" + settings.Content
	p.Files["config/settings.yaml"] = settings
	installed, err := config.InstallPreset(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.ActivatePreset(root, installed.ID); err != nil {
		t.Fatal(err)
	}
	selection := filepath.Join(root, config.PresetSelectionFile)
	editor := app.NewBundleEditor(configstore.New(), selection)
	bundle, _, hashes, err := editor.LoadPlan()
	if err != nil {
		t.Fatal(err)
	}
	physical := filepath.Join(installed.Path, filepath.FromSlash(installed.Manifest.Config))
	if err := config.SaveBundle(physical, bundle); err == nil || !strings.Contains(err.Error(), selection) {
		t.Fatalf("direct snapshot write: %v", err)
	}
	if _, err := editor.Apply(context.Background(), bundle, hashes, func(candidate *config.Bundle) error {
		candidate.Config.Languages.AgentOutput = "Portuguese"
		candidate.Config.Hooks = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := editor.Apply(context.Background(), bundle, hashes, nil); err == nil {
		t.Fatal("stale plan overwrote the active package")
	}
	resolved, err := editor.Load()
	if err != nil || resolved.Config.Languages.AgentOutput != "Portuguese" || len(resolved.Config.Hooks) != 0 {
		t.Fatalf("published settings were lost: %v", err)
	}
	assertPresetModuleEdit(t, selection, p)
	if _, err := config.ActivatePreset(root, installed.ID); err != nil {
		t.Fatal(err)
	}
	base, err := editor.Load()
	if err != nil || base.Config.Languages.AgentOutput == "Portuguese" {
		t.Fatalf("original snapshot changed: %v", err)
	}
}

func assertPresetModuleEdit(t *testing.T, selection string, p config.PresetPackage) {
	t.Helper()
	activePath, _, err := config.ResolvePresetSelection(selection)
	if err != nil {
		t.Fatal(err)
	}
	modified, err := config.ReadPresetDirectory(config.ConfigRootFromYAMLPath(activePath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(modified.Files["config/settings.yaml"].Content, "# Project settings\n") {
		t.Fatal("settings comment was lost")
	}
	if !strings.Contains(modified.Files["config/settings.yaml"].Content, "hooks: {from: ./hooks.yaml}") || modified.Files["config/hooks.yaml"].Content != "[]\n" {
		t.Fatal("clearing hooks did not retain its imported module")
	}
	for name, original := range p.Files {
		if name != "config/settings.yaml" && name != "config/hooks.yaml" && modified.Files[name] != original {
			t.Fatalf("unrelated source changed: %s", name)
		}
	}
}

func TestPresetTransportKeepsOpaqueScriptBytes(t *testing.T) {
	t.Parallel()
	p := presetFixture(t)
	script := config.PresetFile{Content: "#!/bin/sh\n# opaque payload\nprintf '\xff'\n", Mode: 0o751}
	p.Files["scripts/hook.sh"] = script
	raw, err := config.EncodePreset(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := config.DecodePreset(raw)
	if err != nil || decoded.Files["scripts/hook.sh"] != script {
		t.Fatalf("opaque asset changed: %v", err)
	}
	root := t.TempDir()
	first, err := config.InstallPreset(root, decoded)
	if err != nil {
		t.Fatal(err)
	}
	second, err := config.InstallPreset(root, decoded)
	if err != nil || first.ID != second.ID {
		t.Fatalf("repeated import was not idempotent: %v", err)
	}
	changed := decoded
	changed.Files = maps.Clone(decoded.Files)
	changed.Files["scripts/hook.sh"] = config.PresetFile{Content: strings.ReplaceAll(script.Content, "\xff", "\xfe"), Mode: script.Mode}
	different, err := config.InstallPreset(root, changed)
	if err != nil || different.ID == first.ID {
		t.Fatalf("opaque assets collided: %v", err)
	}

	info, err := os.Stat(filepath.Join(first.Path, "scripts/hook.sh"))
	if err != nil || uint32(info.Mode().Perm()) != script.Mode {
		t.Fatalf("executable permission changed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first.Path, "scripts/hook.sh"), []byte("changed"), 0o751); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ActivatePreset(root, first.ID); err == nil {
		t.Fatal("modified snapshot passed integrity validation")
	}
}

func presetFixture(t *testing.T) config.PresetPackage {
	t.Helper()
	seed, err := config.SeedInstall(t.TempDir(), "omakase", false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.SnapshotPreset(seed.Path, "omakase", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func assertPresetSelection(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("preset selection changed after rejection: %v", err)
	}
}
