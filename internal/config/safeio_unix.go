//go:build aix || darwin || dragonfly || freebsd || illumos || netbsd || openbsd || solaris

package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

var syncDirectory = unix.Fsync

// The BSD and System V targets use the same descriptor-relative primitives as
// Linux. Keeping this backend separate avoids the unsafe Lstat-then-open
// fallback that used to be shared by every non-Linux target.
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
		identity, identityErr := unixFileIdentityAt(dirFD, name)
		if identityErr != nil {
			return nil, identityErr
		}
		raw, err := readFileAtExpected(dirFD, name, path, max, identity)
		if err != nil {
			return nil, err
		}
		files = append(files, entityFile{Path: path, IsCustom: isCustom, Raw: raw})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func readFileAt(dirFD int, name, path string, max int64) ([]byte, error) {
	return readFileAtExpected(dirFD, name, path, max, nil)
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

func openDirAtRelative(rootFD int, relative string) (int, error) {
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, err
	}
	for _, component := range safePathComponents(relative) {
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			_ = unix.Close(current)
			return -1, openErr
		}
		_ = unix.Close(current)
		current = next
	}
	return current, nil
}

func readFileAtRelative(rootFD int, rootAbsolute, path string, max int64) ([]byte, error) {
	relative, err := filepath.Rel(rootAbsolute, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("pinned source %s escapes root %s", path, rootAbsolute)
	}
	dirFD, err := openDirAtRelative(rootFD, filepath.Dir(relative))
	if err != nil {
		return nil, noFollowOpenError(path, err)
	}
	defer func() { _ = unix.Close(dirFD) }()
	identity, err := unixFileIdentityAt(dirFD, filepath.Base(relative))
	if err != nil {
		return nil, noFollowOpenError(path, err)
	}
	return readFileAtExpected(dirFD, filepath.Base(relative), path, max, identity)
}

func listFilesAtRelative(rootFD int, rootAbsolute, dir string, exts []string, isCustom bool, max int64) ([]entityFile, error) {
	relative, err := filepath.Rel(rootAbsolute, dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("pinned source %s escapes root %s", dir, rootAbsolute)
	}
	dirFD, err := openDirAtRelative(rootFD, relative)
	if err != nil {
		if err == unix.ENOENT {
			return nil, nil
		}
		return nil, noFollowOpenError(dir, err)
	}
	defer func() { _ = unix.Close(dirFD) }()
	readDirFD, err := unix.Dup(dirFD)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(readDirFD), dir)
	defer func() { _ = file.Close() }()
	entries, err := file.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	files := make([]entityFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() || !hasAnySuffix(strings.ToLower(name), exts) {
			if entry.Mode()&os.ModeSymlink != 0 && hasAnySuffix(strings.ToLower(name), exts) {
				return nil, fmt.Errorf("refusing config path %s: symlink", filepath.Join(dir, name))
			}
			continue
		}
		path := filepath.Join(dir, name)
		identity, identityErr := unixFileIdentityAt(dirFD, name)
		if identityErr != nil {
			return nil, identityErr
		}
		raw, err := readFileAtExpected(dirFD, name, path, max, identity)
		if err != nil {
			return nil, err
		}
		files = append(files, entityFile{Path: path, IsCustom: isCustom, Raw: raw})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
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
		flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		nextFD, openErr := unix.Openat(currentFD, component, flags, 0)
		if openErr != nil && openErr == unix.ENOENT && create {
			if mkdirErr := unix.Mkdirat(currentFD, component, 0o700); mkdirErr != nil && mkdirErr != unix.EEXIST {
				_ = unix.Close(currentFD)
				return -1, "", fmt.Errorf("create directory %s: %w", filepath.Join(absolute, component), mkdirErr)
			}
			nextFD, openErr = unix.Openat(currentFD, component, flags, 0)
		}
		if openErr != nil {
			if openErr == unix.ENOTDIR && isSymlinkAt(currentFD, component) {
				_ = unix.Close(currentFD)
				return -1, "", fmt.Errorf("refusing config path %s: symlink", filepath.Join(absolute, component))
			}
			_ = unix.Close(currentFD)
			return -1, "", noFollowOpenError(absolute, openErr)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return currentFD, absolute, nil
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
	file := os.NewFile(uintptr(dirFD), absolute)
	defer func() { _ = file.Close() }()
	entries, err := file.Readdir(-1)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	for _, entry := range entries {
		if entry.Name() == name {
			return entry, nil
		}
	}
	return nil, os.ErrNotExist
}

func writeAtomicNoFollow(path string, data []byte) error {
	dirFD, absolute, err := openDirNoFollow(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(dirFD) }()
	target := filepath.Base(path)
	return writeAtomicAt(dirFD, absolute, target, data)
}

func writeAtomicAt(dirFD int, absolute, target string, data []byte) error {
	return writeAtomicAtChecked(dirFD, absolute, target, data)
}

func writeAtomicAtChecked(dirFD int, absolute, target string, data []byte) error {
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
