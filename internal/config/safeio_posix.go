//go:build linux || aix || darwin || dragonfly || freebsd || illumos || netbsd || openbsd || solaris

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openFileNoFollow(path string) (*os.File, error) {
	dirFD, absolute, err := openDirNoFollow(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	_ = unix.Close(dirFD)
	if err != nil {
		return nil, noFollowOpenError(filepath.Join(absolute, name), err)
	}
	return os.NewFile(uintptr(fd), filepath.Join(absolute, name)), nil
}

func readFileAtExpected(dirFD int, name, path string, max int64, expected *unixFileIdentity) ([]byte, error) {
	if expected == nil {
		var err error
		expected, err = unixFileIdentityAt(dirFD, name)
		if err != nil {
			return nil, noFollowOpenError(path, err)
		}
	}
	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, noFollowOpenError(path, err)
	}
	opened, identityErr := unixFileIdentityFD(fd)
	if identityErr != nil || !sameUnixFileIdentity(opened, expected) {
		_ = unix.Close(fd)
		if identityErr != nil {
			return nil, identityErr
		}
		return nil, fmt.Errorf("source %s changed identity before read", path)
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	return readBounded(file, path, max)
}

// RemoveFile validates the current leaf and unlinks only a regular file.
func RemoveFile(path string) error {
	dirFD, absolute, err := openDirNoFollow(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(dirFD) }()
	name := filepath.Base(path)
	var info unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if err == unix.ENOENT {
			return nil
		}
		return noFollowOpenError(filepath.Join(absolute, name), err)
	}
	mode := info.Mode & unix.S_IFMT
	if mode == unix.S_IFLNK {
		return fmt.Errorf("refusing remove target %s: symlink", filepath.Join(absolute, name))
	}
	if mode != unix.S_IFREG {
		return fmt.Errorf("refusing remove target %s: not a regular file", filepath.Join(absolute, name))
	}
	if err := unix.Unlinkat(dirFD, name, 0); err != nil {
		return err
	}
	if err := syncDirectory(dirFD); err != nil {
		return &AmbiguousPublicationError{Path: filepath.Join(absolute, name), Operation: "delete", Err: err}
	}
	return nil
}

func rejectAtomicTarget(dirFD int, absolute, name string) error {
	var info unix.Stat_t
	err := unix.Fstatat(dirFD, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if err == unix.ENOENT {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect atomic target %s: %w", filepath.Join(absolute, name), err)
	}
	mode := info.Mode & unix.S_IFMT
	if mode == unix.S_IFLNK {
		return fmt.Errorf("refusing atomic target %s: symlink", filepath.Join(absolute, name))
	}
	if mode != unix.S_IFREG {
		return fmt.Errorf("refusing atomic target %s: not a regular file", filepath.Join(absolute, name))
	}
	return nil
}
