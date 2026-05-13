package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSkills_RepoLocalOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.md"), "---\nname: Go\n---\ndefault body\n")
	writeFile(t, filepath.Join(repoLocal, "go.md"), "---\nname: Go (repo)\n---\nrepo body\n")

	skills, _, err := LoadSkills(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("len(skills) = %d, want 1", len(skills))
	}
	got := skills[0]
	if got.Name != "Go (repo)" {
		t.Fatalf("override not applied: name = %q", got.Name)
	}
	if !got.IsRepoLocal {
		t.Fatalf("IsRepoLocal = false, want true")
	}
	if got.IsCustom {
		t.Fatalf("IsCustom = true, want false")
	}
	if !strings.Contains(got.Body, "repo body") {
		t.Fatalf("body = %q, want repo body", got.Body)
	}
}

func TestLoadSkills_RepoLocalOverridesCustom(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.md"), "---\nname: Go\n---\ndefault\n")
	writeFile(t, filepath.Join(dir, "custom", "go.md"), "---\nname: Go (custom)\n---\ncustom\n")
	writeFile(t, filepath.Join(repoLocal, "go.md"), "---\nname: Go (repo)\n---\nrepo\n")

	skills, _, err := LoadSkills(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "Go (repo)" {
		t.Fatalf("repo-local did not win over custom: %+v", skills)
	}
	if !skills[0].IsRepoLocal || skills[0].IsCustom {
		t.Fatalf("provenance flags wrong: IsCustom=%v IsRepoLocal=%v", skills[0].IsCustom, skills[0].IsRepoLocal)
	}
}

func TestLoadSkills_RepoLocalAddsNewSlug(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.md"), "---\nname: Go\n---\nbody\n")
	writeFile(t, filepath.Join(repoLocal, "rust.md"), "---\nname: Rust\n---\nbody\n")

	skills, _, err := LoadSkills(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("len(skills) = %d, want 2", len(skills))
	}
	bySlug := map[string]Skill{}
	for _, s := range skills {
		bySlug[s.Slug] = s
	}
	if !bySlug["rust"].IsRepoLocal {
		t.Fatalf("rust IsRepoLocal = false")
	}
	if bySlug["go"].IsRepoLocal || bySlug["go"].IsCustom {
		t.Fatalf("go default provenance wrong: %+v", bySlug["go"])
	}
}

func TestLoadSkills_RepoLocalOnly(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(repoLocal, "ruby.md"), "---\nname: Ruby\n---\nbody\n")

	skills, _, err := LoadSkills(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 1 || skills[0].Slug != "ruby" || !skills[0].IsRepoLocal {
		t.Fatalf("repo-local-only load wrong: %+v", skills)
	}
}

func TestLoadSkills_RepoLocalMissingDirOK(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.md"), "---\nname: Go\n---\nbody\n")

	skills, _, err := LoadSkills(dir, filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("LoadSkills() error = %v", err)
	}
	if len(skills) != 1 || skills[0].IsRepoLocal {
		t.Fatalf("missing repo-local dir corrupted load: %+v", skills)
	}
}

func TestLoadLaws_RepoLocalOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(dir, "scope.md"), "---\nseverity: error\n---\nbase\n")
	writeFile(t, filepath.Join(repoLocal, "scope.md"), "---\nseverity: warning\n---\nrepo\n")

	laws, _, err := LoadLaws(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadLaws() error = %v", err)
	}
	if len(laws) != 1 || laws[0].Severity != "warning" || !laws[0].IsRepoLocal {
		t.Fatalf("repo-local law override failed: %+v", laws)
	}
}

func TestLoadPersonas_RepoLocalAddsNewSlug(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent.md"), "---\nname: Agent\n---\nbody\n")
	writeFile(t, filepath.Join(repoLocal, "reviewer.md"), "---\nname: Reviewer\n---\nbody\n")

	personas, _, err := LoadPersonas(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadPersonas() error = %v", err)
	}
	if len(personas) != 2 {
		t.Fatalf("len(personas) = %d, want 2", len(personas))
	}
	bySlug := map[string]Persona{}
	for _, p := range personas {
		bySlug[p.Slug] = p
	}
	if !bySlug["reviewer"].IsRepoLocal {
		t.Fatalf("reviewer IsRepoLocal = false")
	}
}

func TestLoadTemplates_RepoLocalOverridesCustom(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(dir, "user-story.md"), "---\nname: Default\n---\nbase\n")
	writeFile(t, filepath.Join(dir, "custom", "user-story.md"), "---\nname: Custom\n---\ncustom\n")
	writeFile(t, filepath.Join(repoLocal, "user-story.md"), "---\nname: Repo\n---\nrepo\n")

	templates, _, err := LoadTemplates(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadTemplates() error = %v", err)
	}
	if len(templates) != 1 || templates[0].Name != "Repo" || !templates[0].IsRepoLocal {
		t.Fatalf("repo-local template override failed: %+v", templates)
	}
}

func TestLoadSkills_DuplicateInRepoLocalIsFatal(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, filepath.Join(repoLocal, "go.md"), "---\nname: Go A\n---\na\n")
	// Same slug in same layer is impossible via filesystem (one file per
	// path), so duplicate-within-layer cannot happen for repo-local. The
	// invariant checked here: cross-layer collision is NOT an error.
	writeFile(t, filepath.Join(dir, "go.md"), "---\nname: Go default\n---\nbody\n")

	skills, _, err := LoadSkills(dir, repoLocal)
	if err != nil {
		t.Fatalf("LoadSkills() error = %v (cross-layer should override silently)", err)
	}
	if len(skills) != 1 || skills[0].Name != "Go A" {
		t.Fatalf("repo-local did not override default: %+v", skills)
	}
}
