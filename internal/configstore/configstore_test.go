package configstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/testfixtures"
)

// TestAdapterBundleRoundTrip writes a minimal bundle with the adapter's
// SaveBundle, reads it back via LoadBundle, and asserts the wiring round-
// trips. Catches any signature drift between adapter and the underlying
// config helpers (which the wrapper otherwise hides).
func TestAdapterBundleRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() = %v", err)
	}
	configPath := filepath.Join(configDir, "omakiten.yaml")

	// Materialize an entity file so LoadBundle has refs to resolve.
	skillsDir := filepath.Join(tmp, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(skills) = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "go.md"), []byte("---\nname: Go\ndescription: Go skill.\n---\n\nbody"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill) = %v", err)
	}

	adapter := New()
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	// Skills is yaml:"-" so the YAML cannot supply it. Wire the in-memory
	// value here so SaveBundle has the same payload the legacy inline
	// bundle had — adapter.SaveBundle ignores it for the YAML write but
	// downstream callers may inspect the in-memory shape.
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go", Description: "Go skill."}}
	if err := adapter.SaveBundle(configPath, bundle); err != nil {
		t.Fatalf("SaveBundle = %v", err)
	}

	loaded, err := adapter.LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle = %v", err)
	}
	if loaded.Kit.Key != "default" {
		t.Fatalf("loaded.Kit.Key = %q, want %q", loaded.Kit.Key, "default")
	}
	if len(loaded.Skills) != 1 || loaded.Skills[0].Slug != "go" {
		t.Fatalf("loaded.Skills = %+v, want one go skill", loaded.Skills)
	}
}

// TestAdapterWriteAtomic verifies that the adapter's WriteAtomic publishes
// the file via rename — concurrent readers should never observe a half-
// written file. We can't easily test the atomicity itself in unit form, but
// we can confirm the function writes the correct payload and creates parent
// directories.
func TestAdapterWriteAtomic(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "deeper", "leaf", "file.txt")
	payload := []byte("hello world")

	adapter := New()
	if err := adapter.WriteAtomic(target, payload); err != nil {
		t.Fatalf("WriteAtomic = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile = %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file content = %q, want %q", got, payload)
	}
}

// TestAdapterHashFileChangesWithContent verifies the hash function returns
// distinct hex digests for two different file contents and matches itself
// for the same input.
func TestAdapterHashFileChangesWithContent(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a")
	b := filepath.Join(tmp, "b")
	c := filepath.Join(tmp, "c")
	for path, payload := range map[string]string{a: "alpha", b: "alpha", c: "delta"} {
		if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) = %v", path, err)
		}
	}

	adapter := New()
	hashA, err := adapter.HashFile(a)
	if err != nil {
		t.Fatalf("HashFile(a) = %v", err)
	}
	hashB, err := adapter.HashFile(b)
	if err != nil {
		t.Fatalf("HashFile(b) = %v", err)
	}
	hashC, err := adapter.HashFile(c)
	if err != nil {
		t.Fatalf("HashFile(c) = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("HashFile of identical content differs: %s vs %s", hashA, hashB)
	}
	if hashA == hashC {
		t.Fatalf("HashFile of different content matches: %s", hashA)
	}
	if len(hashA) != 64 {
		t.Fatalf("HashFile length = %d, want 64 (sha256 hex)", len(hashA))
	}
}

// TestAdapterEntityFilePathLayout cross-checks the canonical-vs-custom path
// helpers against the layout convention they encode (kind folder + custom/
// subtree for user-owned files).
func TestAdapterEntityFilePathLayout(t *testing.T) {
	adapter := New()
	root := "/tmp/omakiten"

	defaultPath := adapter.EntityFilePath(root, config.EntityKindLaw, "scope")
	if !strings.Contains(defaultPath, "/laws/scope.md") || strings.Contains(defaultPath, "/custom/") {
		t.Fatalf("EntityFilePath = %q, want laws/scope.md (no custom/)", defaultPath)
	}
	customPath := adapter.CustomEntityFilePath(root, config.EntityKindLaw, "scope")
	if !strings.Contains(customPath, "/laws/custom/scope.md") {
		t.Fatalf("CustomEntityFilePath = %q, want laws/custom/scope.md", customPath)
	}
}

// TestAdapterSlugify exercises the kebab-case normalizer through the port
// to keep the contract explicit (the adapter is the production seam).
func TestAdapterSlugify(t *testing.T) {
	adapter := New()
	cases := map[string]string{
		"Hello World":  "hello-world",
		"foo_bar.baz":  "foo-bar-baz",
		"  trim me  ":  "trim-me",
		"already-kebab": "already-kebab",
	}
	for in, want := range cases {
		if got := adapter.Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAdapterEntityFileBytes round-trips a Law through LawFileBytes by
// asserting both the frontmatter and the body land in the rendered output.
// Symmetric tests cover skill + persona variants.
func TestAdapterEntityFileBytes(t *testing.T) {
	adapter := New()

	lawBytes, err := adapter.LawFileBytes(config.Law{Slug: "x", Name: "X law", Severity: "warning", Body: "law body"})
	if err != nil {
		t.Fatalf("LawFileBytes = %v", err)
	}
	if !strings.Contains(string(lawBytes), "law body") || !strings.Contains(string(lawBytes), "warning") {
		t.Fatalf("LawFileBytes payload missing body or severity: %q", lawBytes)
	}

	skillBytes, err := adapter.SkillFileBytes(config.Skill{Slug: "y", Name: "Y skill", Description: "desc", Body: "skill body"})
	if err != nil {
		t.Fatalf("SkillFileBytes = %v", err)
	}
	if !strings.Contains(string(skillBytes), "skill body") || !strings.Contains(string(skillBytes), "desc") {
		t.Fatalf("SkillFileBytes payload missing body or description: %q", skillBytes)
	}

	personaBytes, err := adapter.PersonaFileBytes(config.Persona{Slug: "z", Name: "Z persona", Description: "desc", Body: "persona body"})
	if err != nil {
		t.Fatalf("PersonaFileBytes = %v", err)
	}
	if !strings.Contains(string(personaBytes), "persona body") {
		t.Fatalf("PersonaFileBytes payload missing body: %q", personaBytes)
	}
}
