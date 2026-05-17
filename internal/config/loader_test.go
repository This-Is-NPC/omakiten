package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	hash, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile() error = %v", err)
	}
	if hash == "" {
		t.Fatal("HashFile() returned empty hash")
	}

	_, err = HashFile(filepath.Join(tmp, "missing"))
	if err == nil {
		t.Fatal("HashFile(missing) error = nil")
	}
}

func TestWriteAtomic(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "dir", "file.txt")

	if err := WriteAtomic(path, []byte("atomic content")); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "atomic content" {
		t.Fatalf("ReadFile() = %q, want %q", string(data), "atomic content")
	}
}

func TestEnsureDefaultFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}

	// New layout: yaml lives under config/, entity dirs are siblings.
	if _, err := os.Stat(filepath.Join(tmp, "config", "omakase.yaml")); err != nil {
		t.Fatalf("config/omakiten.yaml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "custom")); err != nil {
		t.Fatalf("config/custom dir missing: %v", err)
	}
	for _, name := range []string{"omakase.yaml", "izakaya.yaml", "kaiseki.yaml", "shokunin.yaml"} {
		if _, err := os.Stat(filepath.Join(tmp, "config", name)); err != nil {
			t.Fatalf("default config profile %s missing: %v", name, err)
		}
	}

	for _, dir := range []string{"skills", "laws", "personas", "templates", "themes", "languages"} {
		if _, err := os.Stat(filepath.Join(tmp, dir)); err != nil {
			t.Fatalf("%s dir missing: %v", dir, err)
		}
		// Each entity dir must have a custom/ subtree the user can write into.
		if _, err := os.Stat(filepath.Join(tmp, dir, "custom")); err != nil {
			t.Fatalf("%s/custom dir missing: %v", dir, err)
		}
	}

	// The bundled English language pack must materialize so the catalog has
	// a baseline to fall back to even when the user has not chosen a language.
	if _, err := os.Stat(filepath.Join(tmp, "languages", "en.yaml")); err != nil {
		t.Fatalf("default language en.yaml missing: %v", err)
	}

	// The default kit must ship the task and PR templates; otherwise the embed.FS
	// pattern silently dropped the templates/ subtree.
	for _, name := range []string{"user-story.md", "pull-request.md"} {
		if _, err := os.Stat(filepath.Join(tmp, "templates", name)); err != nil {
			t.Fatalf("default template %s missing: %v", name, err)
		}
	}

	// Second call should not error and should not overwrite
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() second call error = %v", err)
	}
}

func TestRefreshDefaultFilesOverwrites(t *testing.T) {
	tmp := t.TempDir()
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}

	// Mutate a default file to simulate user customization at the root.
	defaultPath := filepath.Join(tmp, "skills", "implementation.md")
	if err := os.WriteFile(defaultPath, []byte("# user edited\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	customPath := filepath.Join(tmp, "skills", "custom", "mine.md")
	if err := os.WriteFile(customPath, []byte("---\nname: Mine\n---\nmine\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := RefreshDefaultFiles(tmp); err != nil {
		t.Fatalf("RefreshDefaultFiles() error = %v", err)
	}

	got, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) == "# user edited\n" {
		t.Fatalf("RefreshDefaultFiles did not overwrite the default file")
	}

	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("RefreshDefaultFiles touched custom/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "omakase.yaml")); err != nil {
		t.Fatalf("RefreshDefaultFiles did not keep default config profiles: %v", err)
	}
}

func TestLoadThemeMissing(t *testing.T) {
	_, err := LoadTheme(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadTheme(missing) error = nil")
	}
}
