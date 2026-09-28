//go:build linux

package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var syncDirectory = unix.Fsync

// readFileBounded uses descriptors for both the parent directory and the
// final entry. A path replacement after validation therefore cannot redirect
// the read through a symlink.
func readFileBounded(path string, max int64) ([]byte, error) {
	file, err := openFileNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readBounded(file, path, max)
}

func listFilesIn(dir string, exts []string, isCustom bool, max int64) ([]entityFile, error) {
	dirFD, absolute, err := openDirNoFollow(dir, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	dirFile := os.NewFile(uintptr(dirFD), absolute)
	defer func() { _ = dirFile.Close() }()
	entries, err := dirFile.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	files := make([]entityFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !hasAnySuffix(strings.ToLower(name), exts) {
			continue
		}
		path := filepath.Join(absolute, name)
		if entry.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing config path %s: symlink", path)
		}
		if !entry.Mode().IsRegular() {
			continue
		}
		raw, err := readFileAt(dirFD, name, path, max)
		if err != nil {
			return nil, err
		}
		files = append(files, entityFile{Path: path, IsCustom: isCustom, Raw: raw})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

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

func readFileAt(dirFD int, name, path string, max int64) ([]byte, error) {
	return readFileAtExpected(dirFD, name, path, max, nil)
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

type unixFileIdentity struct {
	dev uint64
	ino uint64
}

func unixFileIdentityAt(dirFD int, name string) (*unixFileIdentity, error) {
	var info unix.Stat_t
	if err := unix.Fstatat(dirFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, err
	}
	return &unixFileIdentity{dev: uint64(info.Dev), ino: uint64(info.Ino)}, nil
}

func unixFileIdentityFD(fd int) (*unixFileIdentity, error) {
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	return &unixFileIdentity{dev: uint64(info.Dev), ino: uint64(info.Ino)}, nil
}

func sameUnixFileIdentity(left, right *unixFileIdentity) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.dev == right.dev && left.ino == right.ino
}

func openDirNoFollow(path string, create bool) (int, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, "", err
	}
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open filesystem root: %w", err)
	}
	currentFD := rootFD
	for _, component := range safePathComponents(absolute) {
		nextFD, openErr := openDirComponent(currentFD, absolute, component, create)
		if openErr != nil {
			_ = unix.Close(currentFD)
			return -1, "", openErr
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return currentFD, absolute, nil
}

func openDirComponent(currentFD int, absolute, component string, create bool) (int, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	nextFD, openErr := unix.Openat(currentFD, component, flags, 0)
	if openErr == unix.ENOENT && create {
		if mkdirErr := unix.Mkdirat(currentFD, component, 0o700); mkdirErr != nil && mkdirErr != unix.EEXIST {
			return -1, fmt.Errorf("create directory %s: %w", filepath.Join(absolute, component), mkdirErr)
		}
		nextFD, openErr = unix.Openat(currentFD, component, flags, 0)
	}
	if openErr == unix.ENOTDIR && isSymlinkAt(currentFD, component) {
		return -1, fmt.Errorf("refusing config path %s: symlink", filepath.Join(absolute, component))
	}
	if openErr != nil {
		return -1, noFollowOpenError(absolute, openErr)
	}
	return nextFD, nil
}

func isSymlinkAt(dirFD int, name string) bool {
	var info unix.Stat_t
	return unix.Fstatat(dirFD, name, &info, unix.AT_SYMLINK_NOFOLLOW) == nil && info.Mode&unix.S_IFMT == unix.S_IFLNK
}

func noFollowOpenError(path string, err error) error {
	if err == unix.ELOOP {
		return fmt.Errorf("refusing config path %s: symlink", path)
	}
	return &os.PathError{Op: "open", Path: path, Err: err}
}

func safePathComponents(absolute string) []string {
	parts := strings.Split(filepath.ToSlash(strings.TrimPrefix(absolute, string(filepath.Separator))), "/")
	components := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." {
			components = append(components, part)
		}
	}
	return components
}

func hardenDirNoFollow(path string) error {
	fd, _, err := openDirNoFollow(path, true)
	if err != nil {
		return err
	}
	return unix.Close(fd)
}

func readDirNoFollow(path string) ([]os.FileInfo, error) {
	fd, absolute, err := openDirNoFollow(path, false)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), absolute)
	defer func() { _ = file.Close() }()
	return file.Readdir(-1)
}

func lstatNoFollow(path string) (os.FileInfo, error) {
	dirFD, absolute, err := openDirNoFollow(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dirFD) }()
	var info unix.Stat_t
	name := filepath.Base(path)
	if err := unix.Fstatat(dirFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if err == unix.ENOENT {
			return nil, os.ErrNotExist
		}
		return nil, noFollowOpenError(filepath.Join(absolute, name), err)
	}
	return fileInfoFromStat(filepath.Join(absolute, name), &info), nil
}

func fileInfoFromStat(name string, info *unix.Stat_t) os.FileInfo {
	mode := os.FileMode(info.Mode & 0o777)
	switch info.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= os.ModeDir
	case unix.S_IFLNK:
		mode |= os.ModeSymlink
	case unix.S_IFIFO:
		mode |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= os.ModeSocket
	}
	return syntheticFileInfo{name: name, mode: mode, size: int64(info.Size)}
}

type syntheticFileInfo struct {
	name string
	mode os.FileMode
	size int64
}

func (i syntheticFileInfo) Name() string       { return filepath.Base(i.name) }
func (i syntheticFileInfo) Size() int64        { return i.size }
func (i syntheticFileInfo) Mode() os.FileMode  { return i.mode }
func (i syntheticFileInfo) ModTime() time.Time { return time.Time{} }
func (i syntheticFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i syntheticFileInfo) Sys() any           { return nil }

func writeAtomicNoFollow(path string, data []byte) error {
	dirFD, absolute, err := openDirNoFollow(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(dirFD) }()
	target := filepath.Base(path)
	if err := rejectAtomicTarget(dirFD, absolute, target); err != nil {
		return err
	}
	tmpName, tmpFD, err := createAtomicTemp(dirFD)
	if err != nil {
		return err
	}
	tmp := os.NewFile(uintptr(tmpFD), filepath.Join(absolute, tmpName))
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(dirFD, tmpName, 0)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := rejectAtomicTarget(dirFD, absolute, target); err != nil {
		return err
	}
	if err := unix.Renameat(dirFD, tmpName, dirFD, target); err != nil {
		return err
	}
	cleanup = false
	if err := syncDirectory(dirFD); err != nil {
		return &AmbiguousPublicationError{Path: filepath.Join(absolute, target), Operation: "write", Err: err}
	}
	return nil
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

func createAtomicTemp(dirFD int) (string, int, error) {
	var random [12]byte
	for attempt := 0; attempt < 100; attempt++ {
		if _, err := rand.Read(random[:]); err != nil {
			return "", -1, err
		}
		name := fmt.Sprintf(".%s-%x.tmp", "omakiten", random)
		fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err == nil {
			return name, fd, nil
		}
		if err != unix.EEXIST {
			return "", -1, err
		}
	}
	return "", -1, fmt.Errorf("create atomic temp: too many collisions")
}
