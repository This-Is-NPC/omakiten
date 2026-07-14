//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package app

import (
	"context"
	"errors"
	"os"
)

func validateBackupFileSecurity(os.FileInfo, string) error { return nil }

func lockBackupDirectory(context.Context, string, os.FileInfo, os.FileInfo) (func() error, error) {
	return nil, errors.New("backup directory locking is unavailable on this platform")
}
