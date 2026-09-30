//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes an exclusive flock on file. locked is false when another
// holder owns the lock.
func TryLock(file *os.File) (unlock func() error, locked bool, err error) {
	fd := int(file.Fd())
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return func() error { return unix.Flock(fd, unix.LOCK_UN) }, true, nil
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
			return nil, false, nil
		default:
			return nil, false, err
		}
	}
}
