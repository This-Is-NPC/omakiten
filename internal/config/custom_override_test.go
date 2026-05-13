package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// New layout fixtures: yaml at <root>/config/, entities at <root>/<kind>/ with
// optional <root>/<kind>/custom/ overrides.
func writeNewLayoutFixture(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "config", "omakase.yaml"), baseConfigHeader)
	writeFile(t, filepath.Join(root, "skills", "go.md"), "---\nname: Go\n---\ndefault body\n")
	writeFile(t, filepath.Join(root, "laws", "scope.md"), "---\nseverity: error\n---\nstay in scope\n")
	writeFile(t, filepath.Join(root, "personas", "agent.md"), "---\nname: Agent\n---\ndefault persona\n")
	writeFile(t, filepath.Join(root, "templates", "user-story.md"), "---\nname: Default Template\n---\nbody\n")
}

func TestCustomFileOverridesDefaultBySlug(t *testing.T) {
	root := t.TempDir()
	writeNewLayoutFixture(t, root)
	// Same slug `go` in custom/ — user override.
	writeFile(t, filepath.Join(root, "skills", "custom", "go.md"),
		"---\nname: Go (custom)\n---\noverride body\n")

	bundle, err := LoadBundle(filepath.Join(root, "config", "omakase.yaml"))
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}

	if len(bundle.Skills) != 1 {
		t.Fatalf("Skills len = %d, want 1 (custom override should not duplicate)", len(bundle.Skills))
	}
	got := bundle.Skills[0]
	if got.Slug != "go" || got.Name != "Go (custom)" {
		t.Fatalf("override did not apply: %+v", got)
	}
	if !got.IsCustom {
		t.Fatalf("custom skill IsCustom = false, want true")
	}
	if !strings.Contains(got.Body, "override body") {
		t.Fatalf("custom body not loaded: %q", got.Body)
	}
}

func TestCustomOnlyEntityIsLoaded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config", "omakase.yaml"), baseConfigHeader)
	// No defaults at root, only customs.
	writeFile(t, filepath.Join(root, "skills", "custom", "mine.md"),
		"---\nname: Mine\n---\nmy body\n")
	writeFile(t, filepath.Join(root, "laws", "custom", "mine.md"),
		"---\nseverity: warning\n---\nmy law body\n")

	bundle, err := LoadBundle(filepath.Join(root, "config", "omakase.yaml"))
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}

	if len(bundle.Skills) != 1 || !bundle.Skills[0].IsCustom {
		t.Fatalf("Skills = %+v, want [custom-only mine]", bundle.Skills)
	}
	if len(bundle.Laws) != 1 || !bundle.Laws[0].IsCustom {
		t.Fatalf("Laws = %+v, want [custom-only mine]", bundle.Laws)
	}
}

func TestDefaultIsCustomFalseLoadsAtRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "config", "omakase.yaml"), baseConfigHeader)
	writeFile(t, filepath.Join(root, "skills", "go.md"),
		"---\nname: Go\n---\ndefault\n")

	bundle, err := LoadBundle(filepath.Join(root, "config", "omakase.yaml"))
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	if len(bundle.Skills) != 1 || bundle.Skills[0].IsCustom {
		t.Fatalf("default skill IsCustom should be false, got %+v", bundle.Skills[0])
	}
}
