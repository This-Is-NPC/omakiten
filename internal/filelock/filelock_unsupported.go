//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package filelock

import (
	"errors"
	"os"
)

// TryLock reports that advisory locking is unavailable on this platform.
func TryLock(*os.File) (unlock func() error, locked bool, err error) {
	return nil, false, errors.New("file locking is unavailable on this platform")
}
