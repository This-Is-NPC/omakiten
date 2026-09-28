//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type snapshotSourcePin struct {
	fd int
}

func pinSnapshotSource(path string) (string, os.FileInfo, string, func() error, func(string) error, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, "", nil, nil, snapshotSourceValidationError("snapshot source path is invalid")
	}
	absolutePath = filepath.Clean(absolutePath)
	pin, err := pinSnapshotSourceParent(absolutePath)
	if err != nil {
		return "", nil, "", nil, nil, err
	}
	_, identity, err := pin.openTarget(filepath.Base(absolutePath))
	if err != nil {
		_ = pin.close()
		return "", nil, "", nil, nil, err
	}
	sqliteOpenPath, err := pin.sqlitePath(filepath.Base(absolutePath))
	if err != nil {
		_ = pin.close()
		return "", nil, "", nil, nil, err
	}
	release := func() error { return pin.close() }
	verifyPragma := func(selectedPath string) error {
		return pin.verifyPragmaIdentity(selectedPath, identity)
	}
	return absolutePath, identity, sqliteOpenPath, release, verifyPragma, nil
}

func pinSnapshotSourceParent(path string) (*snapshotSourcePin, error) {
	parent := filepath.Dir(path)
	fd, err := openSnapshotSourceDirectory(parent)
	if err != nil {
		return nil, snapshotSourceValidationError("snapshot source parent cannot be securely opened")
	}
	return &snapshotSourcePin{fd: fd}, nil
}

func openSnapshotSourceDirectory(path string) (int, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	closeFD := func() {
		_ = unix.Close(fd)
	}
	relative := strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	if relative == "" {
		return fd, nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		next, openErr := openSnapshotSourceDirectoryComponent(fd, component)
		if openErr != nil {
			closeFD()
			return -1, openErr
		}
		if err := unix.Close(fd); err != nil {
			_ = unix.Close(next)
			return -1, err
		}
		fd = next
	}
	return fd, nil
}

func openSnapshotSourceDirectoryComponent(parentFD int, component string) (int, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	return unix.Openat(parentFD, component, flags, 0)
}

func (pin *snapshotSourcePin) openTarget(name string) (bool, os.FileInfo, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	fd, err := unix.Openat(pin.fd, name, flags, 0)
	if err != nil {
		return false, nil, snapshotSourceValidationError("snapshot source cannot be securely opened")
	}
	file := os.NewFile(uintptr(fd), name)
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || !info.Mode().IsRegular() {
		return false, nil, snapshotSourceValidationError("snapshot source must be a regular file")
	}
	return false, info, nil
}

func (pin *snapshotSourcePin) sqlitePath(name string) (string, error) {
	for _, root := range []string{"/proc/self/fd", "/dev/fd"} {
		if _, err := os.Stat(root); err == nil {
			return fmt.Sprintf("%s/%d/%s", root, pin.fd, name), nil
		}
	}
	return "", snapshotSourceValidationError("descriptor-relative SQLite path is unavailable")
}

func (pin *snapshotSourcePin) verifyPragmaIdentity(selectedPath string, expected os.FileInfo) error {
	selected, err := snapshotSourceIdentityFromPragmaPath(selectedPath)
	if err != nil {
		return err
	}
	if !os.SameFile(expected, selected) {
		return errors.New("opened snapshot source identity does not match requested file")
	}
	return nil
}

func snapshotSourceIdentityFromPragmaPath(path string) (os.FileInfo, error) {
	if strings.HasPrefix(path, "/proc/self/fd/") || strings.HasPrefix(path, "/dev/fd/") {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat snapshot source pragma path: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("snapshot source pragma path is not a regular file")
		}
		return info, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat snapshot source pragma path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("snapshot source pragma path is not a regular file")
	}
	return info, nil
}

func (pin *snapshotSourcePin) close() error {
	if pin == nil || pin.fd < 0 {
		return nil
	}
	err := unix.Close(pin.fd)
	pin.fd = -1
	return err
}
