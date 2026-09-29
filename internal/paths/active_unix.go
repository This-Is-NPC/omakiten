//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package paths

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const activeMarkerMode = 0o644

// activeMarkerValidationHook is a no-op in production. Unix adversarial tests
// replace it to pause the real marker operation immediately before its
// descriptor-relative identity recheck.
var activeMarkerValidationHook = func() {}

func setActiveConfigFile(dir, filename string) error {
	dirFD, absoluteDir, err := openConfigDir(dir, true)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(dirFD) }()

	mode, identity, err := activeMarkerState(dirFD, absoluteDir)
	if err != nil {
		return err
	}
	return writeActiveMarkerAt(dirFD, absoluteDir, []byte(filename+"\n"), mode, identity)
}

func readActiveConfigFile(dir string) ([]byte, error) {
	dirFD, absoluteDir, err := openConfigDir(dir, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dirFD) }()

	fd, err := unix.Openat(dirFD, ActiveConfigStateFile, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if err == unix.ELOOP {
			return nil, fmt.Errorf("refusing active config marker %s: symlink", filepath.Join(absoluteDir, ActiveConfigStateFile))
		}
		return nil, fmt.Errorf("open active config marker %s: %w", filepath.Join(absoluteDir, ActiveConfigStateFile), err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(absoluteDir, ActiveConfigStateFile))
	defer func() { _ = file.Close() }()
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("refusing active config marker %s: not a regular file", filepath.Join(absoluteDir, ActiveConfigStateFile))
	}
	return io.ReadAll(file)
}

func writeActiveMarkerAt(dirFD int, absoluteDir string, data []byte, mode os.FileMode, identity *activeMarkerIdentity) error {
	tempName, tempFD, err := createActiveTemp(dirFD)
	if err != nil {
		return err
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = unix.Unlinkat(dirFD, tempName, 0)
		}
	}()

	tempPath := filepath.Join(absoluteDir, tempName)
	tempFile := os.NewFile(uintptr(tempFD), tempPath)
	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("set active marker mode: %w", err)
	}
	if _, err := io.Copy(tempFile, bytes.NewReader(data)); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write active marker: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync active marker: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close active marker: %w", err)
	}
	activeMarkerValidationHook()
	if err := validateActiveMarkerBeforeRename(dirFD, absoluteDir, identity); err != nil {
		return err
	}
	if err := unix.Renameat(dirFD, tempName, dirFD, ActiveConfigStateFile); err != nil {
		return fmt.Errorf("replace active marker %s: %w", filepath.Join(absoluteDir, ActiveConfigStateFile), err)
	}
	removeTemp = false
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync active marker directory: %w", err)
	}
	return nil
}

type activeMarkerIdentity struct {
	dev uint64
	ino uint64
}

func activeMarkerState(dirFD int, absoluteDir string) (os.FileMode, *activeMarkerIdentity, error) {
	var info unix.Stat_t
	err := unix.Fstatat(dirFD, ActiveConfigStateFile, &info, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		if err == unix.ENOENT {
			return activeMarkerMode, nil, nil
		}
		return 0, nil, fmt.Errorf("inspect active marker %s: %w", filepath.Join(absoluteDir, ActiveConfigStateFile), err)
	}
	if info.Mode&unix.S_IFMT == unix.S_IFLNK {
		return 0, nil, fmt.Errorf("refusing active config marker %s: symlink", filepath.Join(absoluteDir, ActiveConfigStateFile))
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return 0, nil, fmt.Errorf("refusing active config marker %s: not a regular file", filepath.Join(absoluteDir, ActiveConfigStateFile))
	}
	return os.FileMode(info.Mode & 0o7777), &activeMarkerIdentity{dev: uint64(info.Dev), ino: uint64(info.Ino)}, nil
}

func validateActiveMarkerBeforeRename(dirFD int, absoluteDir string, expected *activeMarkerIdentity) error {
	var info unix.Stat_t
	err := unix.Fstatat(dirFD, ActiveConfigStateFile, &info, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		if err == unix.ENOENT && expected == nil {
			return nil
		}
		if err == unix.ENOENT {
			return fmt.Errorf("active config marker %s changed during write", filepath.Join(absoluteDir, ActiveConfigStateFile))
		}
		return fmt.Errorf("recheck active marker %s: %w", filepath.Join(absoluteDir, ActiveConfigStateFile), err)
	}
	if info.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("refusing active config marker %s: symlink appeared during write", filepath.Join(absoluteDir, ActiveConfigStateFile))
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("refusing active config marker %s: non-regular file appeared during write", filepath.Join(absoluteDir, ActiveConfigStateFile))
	}
	if expected == nil || uint64(info.Dev) != expected.dev || uint64(info.Ino) != expected.ino {
		return fmt.Errorf("active config marker %s changed during write", filepath.Join(absoluteDir, ActiveConfigStateFile))
	}
	return nil
}

func createActiveTemp(dirFD int) (string, int, error) {
	var random [12]byte
	for attempt := 0; attempt < 100; attempt++ {
		if _, err := rand.Read(random[:]); err != nil {
			return "", -1, fmt.Errorf("generate active marker temp name: %w", err)
		}
		name := fmt.Sprintf(".%s-%x.tmp", ActiveConfigStateFile, random)
		fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, activeMarkerMode)
		if err == nil {
			return name, fd, nil
		}
		if err != unix.EEXIST {
			return "", -1, fmt.Errorf("create active marker temp: %w", err)
		}
	}
	return "", -1, fmt.Errorf("create active marker temp: too many collisions")
}

func openConfigDir(dir string, create bool) (int, string, error) {
	absoluteDir, err := AbsolutePath(dir)
	if err != nil {
		return -1, "", err
	}
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open config filesystem root: %w", err)
	}
	currentFD := rootFD
	relative := absoluteDir[len(string(filepath.Separator)):]
	for _, component := range splitPathComponents(relative) {
		flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		nextFD, openErr := unix.Openat(currentFD, component, flags, 0)
		if openErr != nil && openErr == unix.ENOENT && create {
			if mkdirErr := unix.Mkdirat(currentFD, component, 0o755); mkdirErr != nil && mkdirErr != unix.EEXIST {
				_ = unix.Close(currentFD)
				return -1, "", fmt.Errorf("create config directory %s: %w", filepath.Join(absoluteDir, component), mkdirErr)
			}
			nextFD, openErr = unix.Openat(currentFD, component, flags, 0)
		}
		if openErr != nil {
			_ = unix.Close(currentFD)
			return -1, "", fmt.Errorf("open config directory %s without following symlinks: %w", filepath.Join(absoluteDir, component), openErr)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	var info unix.Stat_t
	if err := unix.Fstat(currentFD, &info); err != nil {
		_ = unix.Close(currentFD)
		return -1, "", fmt.Errorf("validate config directory %s: %w", absoluteDir, err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(currentFD)
		return -1, "", fmt.Errorf("config path %s is not a directory", absoluteDir)
	}
	return currentFD, absoluteDir, nil
}

func splitPathComponents(path string) []string {
	var components []string
	for _, component := range bytes.Split([]byte(path), []byte(string(filepath.Separator))) {
		if len(component) != 0 && string(component) != "." {
			components = append(components, string(component))
		}
	}
	return components
}
