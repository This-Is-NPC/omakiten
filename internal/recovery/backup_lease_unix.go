//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package recovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"omakiten/internal/filelock"
)

func validateBackupFileSecurity(info os.FileInfo, kind string) error {
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s has unsafe group/other-write permissions", kind)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s ownership is unavailable", kind)
	}
	if int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is not owned by the current user", kind)
	}
	return nil
}

func lockBackupDirectory(ctx context.Context, path string, expected, _ os.FileInfo) (func() error, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	current, err := directory.Stat()
	if err != nil || !os.SameFile(expected, current) {
		_ = directory.Close()
		return nil, errors.New("backup directory changed while opening lease capability")
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = directory.Close()
			return nil, err
		}
		unlock, locked, err := filelock.TryLock(directory)
		if err != nil {
			_ = directory.Close()
			return nil, err
		}
		if locked {
			return func() error {
				return errors.Join(unlock(), directory.Close())
			}, nil
		}
		if err := waitForBackupLockRetry(ctx); err != nil {
			_ = directory.Close()
			return nil, err
		}
	}
}
