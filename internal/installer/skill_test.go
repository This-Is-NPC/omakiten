package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillLifecycle(t *testing.T) {
	root := t.TempDir()
	results, err := InstallSkills(root, []string{"agents", "claude-code", "agents"}, false)
	if err != nil || len(results) != 2 {
		t.Fatalf("install: %v, %v", results, err)
	}
	for _, result := range results {
		if !result.Changed {
			t.Fatalf("first installation unchanged: %+v", result)
		}
		data, err := os.ReadFile(result.Path)
		if err != nil || !ownedSkill(data) {
			t.Fatalf("installed skill: %v", err)
		}
	}
	repeated, err := InstallSkills(root, SupportedHarnesses(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range repeated {
		if result.Changed {
			t.Fatalf("repeat rewrote %s", result.Path)
		}
	}
	verifySkillUpdate(t, root, results[0].Path)
	verifySkillRemoval(t, root, results[0].Path)
}

func TestSkillPreservesForeignDestination(t *testing.T) {
	root := t.TempDir()
	path, err := skillPath(root, "agents")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("---\nname: omakiten\n---\nUser skill\n")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkills(root, []string{"agents"}, true); err == nil {
		t.Fatal("foreign skill overwritten")
	}
	if removed, err := RemoveSkills(root); err != nil || len(removed) != 0 {
		t.Fatalf("foreign removal: %v, %v", removed, err)
	}
	if refreshed, err := RefreshSkills(root); err != nil || len(refreshed) != 0 {
		t.Fatalf("foreign refresh: %v, %v", refreshed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(foreign) {
		t.Fatalf("foreign content changed: %s, %v", data, err)
	}
}

func verifySkillUpdate(t *testing.T, root, path string) {
	t.Helper()
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(data, []byte("\nLocal edit\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	unchanged, err := InstallSkills(root, []string{"agents"}, false)
	if err != nil || unchanged[0].Changed {
		t.Fatalf("preserve: %v, %v", unchanged, err)
	}
	updated, err := RefreshSkills(root)
	if err != nil || !updated[0].Changed {
		t.Fatalf("update: %v, %v", updated, err)
	}
}

func verifySkillRemoval(t *testing.T, root, path string) {
	t.Helper()
	neighbor := filepath.Join(filepath.Dir(path), "notes.md")
	if err := os.WriteFile(neighbor, []byte("user content"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveSkills(root)
	if err != nil || len(removed) != 2 {
		t.Fatalf("remove: %v, %v", removed, err)
	}
	if _, err := os.Stat(neighbor); err != nil {
		t.Fatalf("neighbor removed: %v", err)
	}
	removed, err = RemoveSkills(root)
	if err != nil || len(removed) != 0 {
		t.Fatalf("repeat removal: %v, %v", removed, err)
	}
}
