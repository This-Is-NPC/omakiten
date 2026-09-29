package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
)

func TestPresetRepositoryOfflineEditingAndTransport(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	seed, p, repo := createPresetRepository(t, root)
	db := filepath.Join(root, "state.db")
	runCLI(t, db, seed.Path, "preset", "add", repo)
	selection := filepath.Join(root, ".omakiten", "config.yaml")
	runCLI(t, db, selection, "preset", "use", "omakase")
	before := mustPresetFile(t, selection)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	runCLI(t, db, selection, "init", "--name", "Package", "--slug", "package", "--root", root)
	runCLI(t, db, "", "config", "validate")
	if resolved := runCLI(t, db, selection, "command", "resolve", "okt-shape"); !strings.Contains(resolved, "third-hokage") {
		t.Fatal("persona bindings were lost")
	}
	runCLI(t, db, selection, "task", "create", "--title", "Offline", "--confirm")
	runCLI(t, db, selection, "config", "language", "set", "--agent", "pt-BR")
	if bytes.Equal(before, mustPresetFile(t, selection)) {
		t.Fatal("edit did not select an independent package")
	}
	runCLI(t, db, selection, "skill", "add", "--name", "Local skill", "--key", "local-skill", "--no-edit")
	runCLI(t, db, selection, "skill", "edit", "local-skill", "--description", "Edited", "--no-edit")
	runCLI(t, db, selection, "skill", "remove", "local-skill")
	list := runCLI(t, db, selection, "preset", "list")
	if !strings.Contains(list, `"dirty":true`) || !strings.Contains(list, `"name":"omakase-local"`) {
		t.Fatalf("modified package missing: %s", list)
	}
	exported := filepath.Join(root, "preset.md")
	runCLI(t, db, selection, "preset", "export", "--output", exported, "--name", "shared", "--version", "2.0.0")
	doc, err := config.DecodePreset(mustPresetFile(t, exported))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Files["scripts/on-task.sh"] != p.Files["scripts/on-task.sh"] {
		t.Fatal("script contents or executable mode changed")
	}
	target := filepath.Join(root, "target", "config.yaml")
	runCLI(t, db, target, "preset", "import", "--file", exported, "--scope", "global")
	runCLI(t, db, target, "preset", "use", doc.Manifest.Name, "--scope", "global")
	runCLI(t, db, target, "config", "validate")
	bundle, err := config.LoadBundle(target)
	if err != nil || bundle.Config.Languages.AgentOutput != "pt-BR" {
		t.Fatalf("round trip: language=%q error=%v", bundle.Config.Languages.AgentOutput, err)
	}
	assertPresetOriginalUnchanged(t, root, p)
}

func assertPresetOriginalUnchanged(t *testing.T, root string, original config.PresetPackage) {
	t.Helper()
	items, err := config.InstalledPresets(filepath.Join(root, ".omakiten"))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Manifest.Name != "omakase" {
			continue
		}
		base, err := config.ReadPresetDirectory(item.Path)
		if err != nil || base.Files[original.Manifest.Config] != original.Files[original.Manifest.Config] || item.Dirty {
			t.Fatalf("source package was changed: %+v, %v", item, err)
		}
		return
	}
	t.Fatal("source package was lost")
}

func mustPresetFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func createPresetRepository(t *testing.T, root string) (config.SeedResult, config.PresetPackage, string) {
	t.Helper()
	seed, err := config.SeedInstall(filepath.Join(root, "seed"), "omakase", false)
	if err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(root, "seed.md")
	runCLI(t, "", seed.Path, "preset", "export", "--output", document)
	p, err := config.DecodePreset(mustPresetFile(t, document))
	if err != nil {
		t.Fatal(err)
	}
	if _, duplicate := p.Files["themes/naruto.yaml"]; duplicate {
		t.Fatal("persona module was exported as a color theme")
	}
	if _, color := p.Files["themes/omakiten.yaml"]; !color {
		t.Fatal("active color theme was lost")
	}
	p.Files["scripts/on-task.sh"] = config.PresetFile{Content: "#!/bin/sh\nprintf 'user hook'\n", Mode: 0o755}
	repo := filepath.Join(root, "repo")
	if err := config.WritePresetDirectory(repo, p); err != nil {
		t.Fatal(err)
	}
	return seed, p, repo
}
