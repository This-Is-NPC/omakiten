package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"omakiten/internal/installer"
	"omakiten/internal/paths"
)

func TestBinaryPath_DefaultPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix default path")
	}
	t.Setenv(InstallDirEnv, "")
	got := BinaryPath("/home/u")
	want := "/home/u/.local/bin/okt"
	if got != want {
		t.Fatalf("BinaryPath posix: got %q want %q", got, want)
	}
}

func TestBinaryPath_InstallDirOverride(t *testing.T) {
	t.Setenv(InstallDirEnv, "/opt/okt")
	got := BinaryPath("/home/u")
	want := filepath.Join("/opt/okt", BinaryName())
	if got != want {
		t.Fatalf("BinaryPath override: got %q want %q", got, want)
	}
}

func TestBinaryPath_EmptyHome(t *testing.T) {
	t.Setenv(InstallDirEnv, "")
	if got := BinaryPath(""); got != "" {
		t.Fatalf("BinaryPath empty home: got %q want empty", got)
	}
}

func TestRemoveBinary_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	removed, err := RemoveBinary(bin)
	if err != nil {
		t.Fatalf("RemoveBinary: %v", err)
	}
	if !removed {
		t.Fatalf("RemoveBinary: removed=false on existing file")
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatalf("RemoveBinary: binary still present after remove: %v", err)
	}
}

func TestRemoveBinary_MissingFile(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ghost")
	removed, err := RemoveBinary(bin)
	if err != nil {
		t.Fatalf("RemoveBinary missing: %v", err)
	}
	if removed {
		t.Fatalf("RemoveBinary missing: removed=true on missing file")
	}
}

func TestRemoveBinary_EmptyPath(t *testing.T) {
	removed, err := RemoveBinary("")
	if err != nil || removed {
		t.Fatalf("RemoveBinary empty: removed=%v err=%v", removed, err)
	}
}

func TestRemoveAllWrappers_StripsBashAndZsh(t *testing.T) {
	home := t.TempDir()
	bashrc := filepath.Join(home, ".bashrc")
	zshrc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(bashrc, []byte("# user content\n"), 0o644); err != nil {
		t.Fatalf("seed bash: %v", err)
	}
	if err := os.WriteFile(zshrc, []byte("# user content\n"), 0o644); err != nil {
		t.Fatalf("seed zsh: %v", err)
	}
	if err := installer.InstallWrapper(bashrc); err != nil {
		t.Fatalf("install bash wrapper: %v", err)
	}
	if err := installer.InstallWrapper(zshrc); err != nil {
		t.Fatalf("install zsh wrapper: %v", err)
	}

	removed, err := RemoveAllWrappers(home)
	if err != nil {
		t.Fatalf("RemoveAllWrappers: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("RemoveAllWrappers: expected 2 removed, got %v", removed)
	}
	for _, rc := range []string{bashrc, zshrc} {
		body, err := os.ReadFile(rc)
		if err != nil {
			t.Fatalf("read %s: %v", rc, err)
		}
		if contains := string(body); strings.Contains(contains, installer.WrapperBegin) {
			t.Fatalf("wrapper still present in %s: %q", rc, contains)
		}
	}
}

func TestRemoveAllWrappers_NoOpOnMissingFiles(t *testing.T) {
	home := t.TempDir()
	removed, err := RemoveAllWrappers(home)
	if err != nil {
		t.Fatalf("RemoveAllWrappers missing: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("RemoveAllWrappers missing: expected empty, got %v", removed)
	}
}

func TestRemoveAllWrappers_NoOpOnFilesWithoutSentinel(t *testing.T) {
	home := t.TempDir()
	bashrc := filepath.Join(home, ".bashrc")
	body := "export PATH=/usr/local/bin:$PATH\nalias ll='ls -la'\n"
	if err := os.WriteFile(bashrc, []byte(body), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	removed, err := RemoveAllWrappers(home)
	if err != nil {
		t.Fatalf("RemoveAllWrappers: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("RemoveAllWrappers: expected empty (no sentinel), got %v", removed)
	}
	after, _ := os.ReadFile(bashrc)
	if string(after) != body {
		t.Fatalf("file mutated: got %q want %q", string(after), body)
	}
}

// TestRemoveAllWrappers_AbortsOnReadOnlyRC pins the surface contract:
// when an rc file carries the okt wrapper sentinel but its parent is
// read-only (so WriteFile cannot rewrite it), RemoveAllWrappers
// aborts and surfaces the error rather than silently skipping. This
// is the path that warns a user their .bashrc is locked instead of
// reporting a fake success.
func TestRemoveAllWrappers_AbortsOnReadOnlyRC(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based read-only directories don't gate root or windows ACLs")
	}
	home := t.TempDir()
	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# user content\n"), 0o644); err != nil {
		t.Fatalf("seed bash: %v", err)
	}
	if err := installer.InstallWrapper(bashrc); err != nil {
		t.Fatalf("install wrapper: %v", err)
	}

	// 0o400 on the file itself blocks the O_TRUNC|O_WRONLY open inside
	// os.WriteFile that installer.RemoveWrapper uses to rewrite the rc.
	// Directory perms stay normal so t.TempDir cleanup can still unlink
	// the file after chmod is restored.
	if err := os.Chmod(bashrc, 0o400); err != nil {
		t.Fatalf("chmod bashrc: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(bashrc, 0o644) })

	removed, err := RemoveAllWrappers(home)
	if err == nil {
		t.Fatalf("expected error on read-only home, got removed=%v", removed)
	}
	if len(removed) != 0 {
		t.Fatalf("removed slice should be empty on abort, got %v", removed)
	}
	if !strings.Contains(err.Error(), "write") && !strings.Contains(err.Error(), "permission") {
		t.Fatalf("error should mention write/permission failure: %v", err)
	}
}

func TestPurgeDataDir_RemovesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.HomeEnv, home)
	dataDir, err := paths.DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "omakiten.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("seed db: %v", err)
	}
	got, removed, err := PurgeDataDir()
	if err != nil {
		t.Fatalf("PurgeDataDir: %v", err)
	}
	if got != dataDir {
		t.Fatalf("path: got %q want %q", got, dataDir)
	}
	if !removed {
		t.Fatalf("removed=false on existing dir")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir still present after purge: %v", err)
	}
}

func TestPurgeDataDir_NoopOnMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.HomeEnv, home)
	_, removed, err := PurgeDataDir()
	if err != nil {
		t.Fatalf("PurgeDataDir missing: %v", err)
	}
	if removed {
		t.Fatalf("removed=true on missing dir")
	}
}

func TestPurgeConfigRoot_RemovesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.HomeEnv, home)
	root, err := paths.ConfigRoot()
	if err != nil {
		t.Fatalf("ConfigRoot: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "omakiten.yaml"), []byte("name: x"), 0o644); err != nil {
		t.Fatalf("seed yaml: %v", err)
	}
	got, removed, err := PurgeConfigRoot()
	if err != nil {
		t.Fatalf("PurgeConfigRoot: %v", err)
	}
	if got != root {
		t.Fatalf("path: got %q want %q", got, root)
	}
	if !removed {
		t.Fatalf("removed=false on existing dir")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("config root still present after purge: %v", err)
	}
}

func TestDirSize_SumsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("seed sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b"), make([]byte, 512), 0o644); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	got, err := DirSize(dir)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if got != 1024+512 {
		t.Fatalf("DirSize: got %d want %d", got, 1024+512)
	}
}

func TestDirSize_MissingReturnsZero(t *testing.T) {
	got, err := DirSize(filepath.Join(t.TempDir(), "ghost"))
	if err != nil {
		t.Fatalf("DirSize missing: %v", err)
	}
	if got != 0 {
		t.Fatalf("DirSize missing: got %d want 0", got)
	}
}

func TestFormatBytes_Buckets(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d): got %q want %q", c.in, got, c.want)
		}
	}
}

