package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"omakiten/defaults"
)

func TestSyncDefaultsReplacesImportedConfigFragments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "config", "modules", "base-config.yaml"), "activity_log:\n  max_rows: 500\n")
	writeConfigTestFile(t, filepath.Join(root, "config", "modules", "stale.yaml"), "stale\n")
	writeConfigTestFile(t, filepath.Join(root, "config", "modules", "surfaces.yaml", "child"), "stale directory\n")
	writeConfigTestFile(t, filepath.Join(root, "config", "themes"), "stale file\n")
	writeConfigTestFile(t, filepath.Join(root, "config", "custom", "user.yaml"), "user config\n")
	writeConfigTestFile(t, filepath.Join(root, "config", ".active"), "kaiseki.yaml\n")
	writeConfigTestFile(t, filepath.Join(root, "personas", "stale", "old.md"), "stale persona\n")
	writeConfigTestFile(t, filepath.Join(root, "personas", "custom", "user.md"), "user persona\n")

	script := filepath.Join("..", "..", "scripts", "sync-defaults.sh")
	cmd := exec.Command("bash", script, root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sync-defaults.sh: %v\n%s", err, output)
	}

	wantBase, err := defaults.FS.ReadFile("config/modules/base-config.yaml")
	if err != nil {
		t.Fatalf("read embedded base config: %v", err)
	}
	gotBase, err := os.ReadFile(filepath.Join(root, "config", "modules", "base-config.yaml"))
	if err != nil {
		t.Fatalf("read synced base config: %v", err)
	}
	if !bytes.Equal(gotBase, wantBase) {
		t.Fatal("sync-defaults.sh left a stale imported base config")
	}
	if _, err := os.Stat(filepath.Join(root, "config", "modules", "stale.yaml")); !os.IsNotExist(err) {
		t.Fatalf("stale managed module survived sync: %v", err)
	}
	if info, err := os.Stat(filepath.Join(root, "config", "modules", "surfaces.yaml")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("managed directory-to-file transition = %v, %v; want regular file", info, err)
	}
	if info, err := os.Stat(filepath.Join(root, "config", "themes")); err != nil || !info.IsDir() {
		t.Fatalf("managed file-to-directory transition = %v, %v; want directory", info, err)
	}
	if _, err := os.Stat(filepath.Join(root, "personas", "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale managed entity directory survived sync: %v", err)
	}
	assertSyncedFileBody(t, filepath.Join(root, "config", "custom", "user.yaml"), "user config\n")
	assertSyncedFileBody(t, filepath.Join(root, "config", ".active"), "kaiseki.yaml\n")
	assertSyncedFileBody(t, filepath.Join(root, "personas", "custom", "user.md"), "user persona\n")
}

func assertSyncedFileBody(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved file %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("preserved file %s = %q, want %q", path, got, want)
	}
}
