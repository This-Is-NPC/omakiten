//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || windows

package filelock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTryLockExcludesSecondHolderUntilUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first := openLockFile(t, path)
	second := openLockFile(t, path)

	unlock, locked, err := TryLock(first)
	if err != nil || !locked {
		t.Fatalf("first TryLock = locked %v, err %v; want the lock", locked, err)
	}
	if _, locked, err := TryLock(second); err != nil || locked {
		t.Fatalf("second TryLock = locked %v, err %v; want contention", locked, err)
	}
	if err := unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	unlockSecond, locked, err := TryLock(second)
	if err != nil || !locked {
		t.Fatalf("TryLock after unlock = locked %v, err %v; want the lock", locked, err)
	}
	if err := unlockSecond(); err != nil {
		t.Fatalf("unlock second: %v", err)
	}
}

func openLockFile(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
