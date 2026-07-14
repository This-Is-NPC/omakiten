//go:build windows

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Windows POSIX mode bits do not describe ACL confidentiality. This layer does
// not install or validate private DACLs; native deployment policy must provide
// private inheritance. Root and lock identity checks protect integrity only.
func validateBackupFileSecurity(os.FileInfo, string) error { return nil }

func lockBackupDirectory(ctx context.Context, dirPath string, _, expected os.FileInfo) (func() error, error) {
	path, err := windows.UTF16PtrFromString(filepath.Join(dirPath, backupLeaseFilename))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), filepath.Join(dirPath, backupLeaseFilename))
	current, err := file.Stat()
	if err != nil || !os.SameFile(expected, current) {
		_ = file.Close()
		return nil, errors.New("backup lease file changed while opening anti-rename handle")
	}
	overlapped := new(windows.Overlapped)
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err := windows.LockFileEx(
			handle,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1,
			0,
			overlapped,
		)
		if err == nil {
			return func() error {
				return errors.Join(windows.UnlockFileEx(handle, 0, 1, 0, overlapped), file.Close())
			}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, err
		}
		if err := waitForBackupLockRetry(ctx); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
}
