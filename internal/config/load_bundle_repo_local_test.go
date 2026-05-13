package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBundleWithRepoLocal_EmptyDirIsNoOp(t *testing.T) {
	root := t.TempDir()
	writeNewLayoutFixture(t, root)

	// Same fixture run through both entry points must produce equivalent
	// bundle metadata for the entity sets they share.
	want, err := LoadBundle(filepath.Join(root, "config", "omakase.yaml"))
	if err != nil {
		t.Fatalf("LoadBundle err: %v", err)
	}
	got, err := LoadBundleWithRepoLocal(filepath.Join(root, "config", "omakase.yaml"), "")
	if err != nil {
		t.Fatalf("LoadBundleWithRepoLocal err: %v", err)
	}
	if len(want.Skills) != len(got.Skills) {
		t.Fatalf("skills len diverged: want %d got %d", len(want.Skills), len(got.Skills))
	}
	if len(want.Laws) != len(got.Laws) {
		t.Fatalf("laws len diverged: want %d got %d", len(want.Laws), len(got.Laws))
	}
}

func TestLoadBundleWithRepoLocal_EntityFolderOverridesDefault(t *testing.T) {
	root := t.TempDir()
	writeNewLayoutFixture(t, root)
	repo := filepath.Join(root, ".omakiten")
	writeFile(t, filepath.Join(repo, "skills", "go.md"), "---\nname: Go (repo)\n---\nrepo body\n")

	bundle, err := LoadBundleWithRepoLocal(filepath.Join(root, "config", "omakase.yaml"), repo)
	if err != nil {
		t.Fatalf("LoadBundleWithRepoLocal err: %v", err)
	}
	bySlug := map[string]Skill{}
	for _, s := range bundle.Skills {
		bySlug[s.Slug] = s
	}
	got, ok := bySlug["go"]
	if !ok {
		t.Fatalf("go skill missing: %+v", bundle.Skills)
	}
	if got.Name != "Go (repo)" {
		t.Fatalf("override not applied: name = %q", got.Name)
	}
	if !got.IsRepoLocal {
		t.Fatalf("IsRepoLocal = false, want true")
	}
}

func TestLoadBundleWithRepoLocal_NewEntityAdded(t *testing.T) {
	root := t.TempDir()
	writeNewLayoutFixture(t, root)
	repo := filepath.Join(root, ".omakiten")
	writeFile(t, filepath.Join(repo, "skills", "rust.md"), "---\nname: Rust\n---\nrust body\n")

	bundle, err := LoadBundleWithRepoLocal(filepath.Join(root, "config", "omakase.yaml"), repo)
	if err != nil {
		t.Fatalf("LoadBundleWithRepoLocal err: %v", err)
	}
	var rust *Skill
	for i := range bundle.Skills {
		if bundle.Skills[i].Slug == "rust" {
			rust = &bundle.Skills[i]
			break
		}
	}
	if rust == nil {
		t.Fatalf("rust skill not added; got %+v", bundle.Skills)
	}
	if !rust.IsRepoLocal {
		t.Fatalf("rust.IsRepoLocal = false")
	}
}

func TestLoadBundleWithRepoLocal_WiringEntryMergeAddsPersona(t *testing.T) {
	root := t.TempDir()
	writeNewLayoutFixture(t, root)
	repo := filepath.Join(root, ".omakiten")
	// Repo-local persona file plus a wiring entry referencing it.
	writeFile(t, filepath.Join(repo, "personas", "reviewer.md"),
		"---\nname: Reviewer\n---\nrepo persona body\n")
	writeFile(t, filepath.Join(repo, "omakiten.yaml"),
		"personas:\n  - slug: reviewer\n    skills: []\n    laws: []\n")

	bundle, err := LoadBundleWithRepoLocal(filepath.Join(root, "config", "omakase.yaml"), repo)
	if err != nil {
		t.Fatalf("LoadBundleWithRepoLocal err: %v", err)
	}
	var reviewer *Persona
	for i := range bundle.Personas {
		if bundle.Personas[i].Slug == "reviewer" {
			reviewer = &bundle.Personas[i]
			break
		}
	}
	if reviewer == nil {
		t.Fatalf("reviewer persona not wired; got %+v", bundle.Personas)
	}
	if !reviewer.IsRepoLocal {
		t.Fatalf("reviewer.IsRepoLocal = false")
	}
}

func TestLoadBundleWithRepoLocal_PersonasDisabledRemoves(t *testing.T) {
	root := t.TempDir()
	writeNewLayoutFixture(t, root)
	// Default fixture wires `agent` via the omakase yaml. Override with a
	// repo-local that disables it through the new removal list.
	repo := filepath.Join(root, ".omakiten")
	writeFile(t, filepath.Join(repo, "omakiten.yaml"),
		"personas_disabled: [agent]\n")

	// Re-run; agent should no longer appear in the wired personas slice.
	// The on-disk personas/agent.md is still loaded but the wiring no
	// longer references it so pickPersonas drops it.
	bundle, err := LoadBundleWithRepoLocal(filepath.Join(root, "config", "omakase.yaml"), repo)
	if err != nil {
		t.Fatalf("LoadBundleWithRepoLocal err: %v", err)
	}
	for _, p := range bundle.Personas {
		if p.Slug == "agent" {
			t.Fatalf("agent persona should be disabled, got %+v", bundle.Personas)
		}
	}
}

func TestLoadBundleWithRepoLocal_WalkUpDiscoveryFromNested(t *testing.T) {
	// Verifies the integration of FindRepoLocal + LoadBundleWithRepoLocal.
	root := t.TempDir()
	t.Setenv("HOME", root)
	writeNewLayoutFixture(t, root)
	repo := filepath.Join(root, ".omakiten")
	writeFile(t, filepath.Join(repo, "skills", "go.md"), "---\nname: Go (repo)\n---\nrepo body\n")

	nested := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	discovered, ok, err := FindRepoLocal(nested)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if !ok || discovered != repo {
		t.Fatalf("FindRepoLocal: got (%s, %v), want (%s, true)", discovered, ok, repo)
	}

	bundle, err := LoadBundleWithRepoLocal(filepath.Join(root, "config", "omakase.yaml"), discovered)
	if err != nil {
		t.Fatalf("LoadBundleWithRepoLocal err: %v", err)
	}
	bySlug := map[string]Skill{}
	for _, s := range bundle.Skills {
		bySlug[s.Slug] = s
	}
	if got, ok := bySlug["go"]; !ok || !got.IsRepoLocal || !strings.Contains(got.Body, "repo body") {
		t.Fatalf("repo-local body did not propagate: %+v", got)
	}
}

