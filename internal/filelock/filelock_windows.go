//go:build windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes an exclusive LockFileEx lock on the first byte of file.
// locked is false when another holder owns the lock.
func TryLock(file *os.File) (unlock func() error, locked bool, err error) {
	handle := windows.Handle(file.Fd())
	overlapped := new(windows.Overlapped)
	err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	switch {
	case err == nil:
		return func() error { return windows.UnlockFileEx(handle, 0, 1, 0, overlapped) }, true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return nil, false, nil
	default:
		return nil, false, err
	}
}
