//go:build windows

package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"omakiten/internal/filelock"
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
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		unlock, locked, err := filelock.TryLock(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if locked {
			return func() error {
				return errors.Join(unlock(), file.Close())
			}, nil
		}
		if err := waitForBackupLockRetry(ctx); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
}
