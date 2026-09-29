//go:build windows

package config

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func createWindowsJunction(t *testing.T, junction, target string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction %s -> %s: %v (%s)", junction, target, err, output)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
}

func swapWindowsDirectoryForJunction(t *testing.T, dir, target string) {
	t.Helper()
	old := dir + "-old"
	if err := os.Rename(dir, old); err != nil {
		t.Fatalf("rename %s aside: %v", dir, err)
	}
	createWindowsJunction(t, dir, target)
}

func TestWindowsNTStatusClassificationUsesWin32Errno(t *testing.T) {
	cases := map[string]struct {
		status windows.NTStatus
		errno  syscall.Errno
	}{
		"missing directory": {windows.STATUS_OBJECT_PATH_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND},
		"missing target":    {windows.STATUS_OBJECT_NAME_NOT_FOUND, windows.ERROR_FILE_NOT_FOUND},
		"existing target":   {windows.STATUS_OBJECT_NAME_COLLISION, windows.ERROR_ALREADY_EXISTS},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !windowsErrorIs(tc.status, tc.errno) {
				t.Fatalf("windowsErrorIs(%#x, %d) = false", tc.status, tc.errno)
			}
		})
	}
	if err := noFollowOpenError("missing", windows.STATUS_OBJECT_NAME_NOT_FOUND); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("translated missing status = %v, want os.ErrNotExist", err)
	}
}

func TestWindowsWriteAtomicCreatesNewTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "config.yaml")
	want := []byte("new config\n")
	if err := WriteAtomic(target, want); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read atomic target: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("atomic target = %q, want %q", got, want)
	}
}

func TestWindowsReadFileAtPinsDirectoryAcrossPathSwap(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	dir := filepath.Join(root, "entities")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir entities: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("owned\n"), 0o600); err != nil {
		t.Fatalf("write owned file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "skill.md"), []byte("escaped\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	dirHandle, _, err := openDirNoFollow(dir, false)
	if err != nil {
		t.Fatalf("open entity directory: %v", err)
	}
	dirFile := os.NewFile(uintptr(dirHandle), dir)
	defer func() { _ = dirFile.Close() }()
	swapWindowsDirectoryForJunction(t, dir, outside)

	got, err := readFileAt(dirHandle, "skill.md", filepath.Join(dir, "skill.md"), MaxEntityFileBytes)
	if err != nil {
		t.Fatalf("read pinned entity: %v", err)
	}
	if !bytes.Equal(got, []byte("owned\n")) {
		t.Fatalf("pinned entity = %q, want owned content", got)
	}
}

func TestWindowsRenameAndRemovePinDirectoryHandlesAcrossPathSwap(t *testing.T) {
	t.Run("rename", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		sourceDir := filepath.Join(root, "source")
		destinationDir := filepath.Join(root, "destination")
		if err := os.MkdirAll(sourceDir, 0o700); err != nil {
			t.Fatalf("mkdir source: %v", err)
		}
		if err := os.MkdirAll(destinationDir, 0o700); err != nil {
			t.Fatalf("mkdir destination: %v", err)
		}
		if err := os.Mkdir(filepath.Join(outside, "source"), 0o700); err != nil {
			t.Fatalf("mkdir outside source: %v", err)
		}
		if err := os.Mkdir(filepath.Join(outside, "destination"), 0o700); err != nil {
			t.Fatalf("mkdir outside destination: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "source.yaml"), []byte("owned\n"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}

		sourceHandle, _, err := openDirNoFollow(sourceDir, false)
		if err != nil {
			t.Fatalf("open source directory: %v", err)
		}
		destinationHandle, _, err := openDirNoFollowAccess(destinationDir, false, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE)
		if err != nil {
			_ = windows.CloseHandle(sourceHandle)
			t.Fatalf("open destination directory: %v", err)
		}
		defer func() {
			_ = windows.CloseHandle(sourceHandle)
			_ = windows.CloseHandle(destinationHandle)
		}()
		swapWindowsDirectoryForJunction(t, sourceDir, filepath.Join(outside, "source"))
		swapWindowsDirectoryForJunction(t, destinationDir, filepath.Join(outside, "destination"))
		fileHandle, err := openRelative(sourceHandle, "source.yaml", windows.FILE_GENERIC_READ|windows.DELETE, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT)
		if err != nil {
			t.Fatalf("open source handle: %v", err)
		}
		defer func() { _ = windows.CloseHandle(fileHandle) }()

		if err := renameHandle(fileHandle, destinationHandle, "moved.yaml"); err != nil {
			t.Fatalf("rename pinned source: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(destinationDir+"-old", "moved.yaml"))
		if err != nil {
			t.Fatalf("read pinned destination: %v", err)
		}
		if !bytes.Equal(got, []byte("owned\n")) {
			t.Fatalf("pinned destination = %q, want owned content", got)
		}
		if _, err := os.Stat(filepath.Join(outside, "destination", "moved.yaml")); !os.IsNotExist(err) {
			t.Fatalf("rename escaped to junction target: %v", err)
		}
	})

	t.Run("remove", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		dir := filepath.Join(root, "remove")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir remove directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "victim"), []byte("owned\n"), 0o600); err != nil {
			t.Fatalf("write victim: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outside, "victim"), []byte("outside\n"), 0o600); err != nil {
			t.Fatalf("write outside victim: %v", err)
		}
		dirHandle, _, err := openDirNoFollowAccess(dir, false, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.DELETE)
		if err != nil {
			t.Fatalf("open remove directory: %v", err)
		}
		defer func() { _ = windows.CloseHandle(dirHandle) }()
		swapWindowsDirectoryForJunction(t, dir, outside)

		if err := removeAt(dirHandle, "victim", false); err != nil {
			t.Fatalf("remove pinned victim: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir+"-old", "victim")); !os.IsNotExist(err) {
			t.Fatalf("pinned victim remains: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(outside, "victim"))
		if err != nil {
			t.Fatalf("read outside victim: %v", err)
		}
		if !bytes.Equal(got, []byte("outside\n")) {
			t.Fatalf("remove escaped to junction target: %q", got)
		}
	})
}
