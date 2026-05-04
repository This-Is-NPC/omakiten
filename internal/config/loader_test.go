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

	// Verify omakiten.yaml was created
	if _, err := os.Stat(filepath.Join(tmp, "omakiten.yaml")); err != nil {
		t.Fatalf("omakiten.yaml missing: %v", err)
	}

	// Verify skills, laws, personas, themes dirs were created
	for _, dir := range []string{"skills", "laws", "personas", "themes"} {
		if _, err := os.Stat(filepath.Join(tmp, dir)); err != nil {
			t.Fatalf("%s dir missing: %v", dir, err)
		}
	}

	// Second call should not error and should not overwrite
	if err := EnsureDefaultFiles(tmp); err != nil {
		t.Fatalf("EnsureDefaultFiles() second call error = %v", err)
	}
}

func TestLoadThemeMissing(t *testing.T) {
	_, err := LoadTheme(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadTheme(missing) error = nil")
	}
}
