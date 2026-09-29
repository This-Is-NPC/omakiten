package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
)

func TestCLIApplicationLanguagePreferencesLeavePresetsIntact(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("OMAKITEN_HOME", filepath.Join(root, "application"))
	db, selection := filepath.Join(root, "state.db"), filepath.Join(root, ".omakiten", "config.yaml")
	runCLI(t, db, "", "config", "init", "--scope", "local", "--preset", "omakase")
	before := readFile(t, selection)
	out := runCLI(t, db, selection, "config", "language", "set", "--cli", "pt-br", "--agent", "Portuguese")
	path, err := config.PreferencesPath()
	if err != nil {
		t.Fatal(err)
	}
	if decodeEnvelope(t, out)["data"].(map[string]any)["path"] != path {
		t.Fatalf("preferences were routed through the preset: %s", out)
	}
	readBackEquals(t, selection, before)
	items, err := config.InstalledPresets(filepath.Dir(selection))
	if err != nil || len(items) != 1 || items[0].Dirty {
		t.Fatalf("language change created a modified preset: %+v, %v", items, err)
	}
	writeFile(t, selection, "unsupported: true\n")
	portuguese, err := config.LoadBundledLanguage("pt-br")
	if err != nil {
		t.Fatal(err)
	}
	if bootstrapCatalog().Get("cli.root.short") != portuguese.Keys["cli.root.short"] {
		t.Fatal("CLI help does not use application preferences independently of the preset")
	}
	out = runCLI(t, db, selection, "config", "language", "show")
	if !strings.Contains(out, "Portuguese") {
		t.Fatalf("preferences depend on a valid preset: %s", out)
	}
	writeFile(t, path, "languages: {cli: unknown}\n")
	english, err := config.LoadBundledLanguage("en")
	if err != nil {
		t.Fatal(err)
	}
	if bootstrapCatalog().Get("cli.root.short") != english.Keys["cli.root.short"] {
		t.Fatal("invalid preferences prevent English recovery help")
	}
	runCLIExpectError(t, db, selection, "config_invalid", "config", "language", "show")
	runCLI(t, db, selection, "config", "language", "reset")
	p, err := config.LoadPreferences()
	if err != nil || p.Languages.Effective().CLI != "en" {
		t.Fatalf("reset failed to repair preferences: %+v, %v", p, err)
	}
}

func TestCLIConfigInspectionReportsInvalidSources(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config.yaml")
	runCLI(t, db, cfg, "config", "init", "--scope", "global", "--preset", "omakase")
	broken, scalar, empty := filepath.Join(root, "broken.yaml"), filepath.Join(root, "scalar.yaml"), filepath.Join(root, "empty.yaml")
	writeFile(t, broken, "broken: [\n")
	writeFile(t, scalar, "plain scalar\n")
	writeFile(t, empty, "")
	value := decodeEnvelope(t, runCLI(t, db, cfg, "config", "why", "config.workflow.active"))["data"].(map[string]any)
	if value["source"] != "explicit" || value["value"] != "omakase" {
		t.Fatalf("global discovery lost the preset: %v", value)
	}
	value = decodeEnvelope(t, runCLI(t, db, cfg, "config", "why", "version.child"))["data"].(map[string]any)
	if value["source"] != "not_set" {
		t.Fatalf("scalar has a child key: %v", value)
	}
	runCLI(t, db, cfg, "config", "diff", "global", cfg)
	for name, args := range map[string][]string{
		"missing left file":          {"config", "diff", "missing.yaml", cfg},
		"missing right file":         {"config", "diff", cfg, "missing.yaml"},
		"missing local installation": {"config", "diff", "local", cfg},
		"empty local path":           {"config", "diff", "local:", cfg},
		"absent local directory":     {"config", "diff", "local:" + root, cfg},
		"invalid show scope":         {"config", "show", "--scope", "other"},
		"invalid path scope":         {"config", "path", "--scope", "other"},
		"invalid init scope":         {"config", "init", "--scope", "other", "--preset", "omakase"},
	} {
		t.Run(name, func(t *testing.T) { runCLIExpectError(t, db, cfg, "validation_error", args...) })
	}
	for name, args := range map[string][]string{
		"broken left":     {"config", "diff", broken, cfg},
		"broken right":    {"config", "diff", cfg, broken},
		"scalar document": {"config", "diff", scalar, cfg},
		"broken why":      {"--config", broken, "config", "why", "broken", "--layer", "global"},
	} {
		t.Run(name, func(t *testing.T) {
			envelope := runCLIExpectError(t, db, cfg, "config_invalid", args...)
			if !strings.Contains(envelope["details"].(map[string]any)["path"].(string), root) {
				t.Fatalf("source path absent: %v", envelope)
			}
		})
	}
	if diff := decodeEnvelope(t, runCLI(t, db, cfg, "config", "diff", empty, empty))["data"].(map[string]any)["diff"].([]any); len(diff) != 0 {
		t.Fatalf("empty documents differ: %v", diff)
	}
	writeFile(t, filepath.Join(root, ".omakiten", "config", ".active"), "missing.yaml\n")
	runCLIExpectError(t, db, cfg, "validation_error", "config", "diff", "local:"+root, cfg)
	if err := os.RemoveAll(filepath.Join(root, ".omakiten")); err != nil {
		t.Fatal(err)
	}
}

func TestCLIConfigProjectSelectorControlsDiscovery(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config.yaml")
	runCLI(t, db, cfg, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
	runCLI(t, db, cfg, "config", "init", "--scope", "local", "--preset", "omakase")
	t.Chdir(t.TempDir())
	for _, command := range []string{"path", "show"} {
		out := runCLI(t, db, cfg, "--project", "example", "config", command, "--scope", "local")
		if !strings.Contains(out, filepath.Join(root, ".omakiten")) {
			t.Fatalf("project discovery lost: %s", out)
		}
		runCLIExpectError(t, db, cfg, "project_not_found", "--project", "missing", "config", command, "--scope", "local")
	}
}
