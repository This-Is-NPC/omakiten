//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package sqlite

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func bindSnapshotStage(staged, _ *os.File, _ string) (string, func() error, func() error, error) {
	for _, root := range []string{"/dev/fd", "/proc/self/fd"} {
		path := fmt.Sprintf("%s/%d", root, staged.Fd())
		if info, err := os.Stat(path); err == nil {
			stagedInfo, statErr := staged.Stat()
			if statErr == nil && os.SameFile(stagedInfo, info) {
				return path, func() error { return nil }, func() error { return nil }, nil
			}
		}
	}
	return "", nil, nil, errors.New("descriptor-bound SQLite staging path is unavailable")
}

func validateSnapshotStageForPublication(root *os.Root, name string, expected os.FileInfo) error {
	current, err := root.Lstat(name)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		return errors.New("verified snapshot staging identity changed before publication")
	}
	return nil
}

func linkSnapshotFile(_ *os.File, sourceDirectory *os.File, sourceName string, destinationDirectory *os.File, destinationName string) error {
	return unix.Linkat(int(sourceDirectory.Fd()), sourceName, int(destinationDirectory.Fd()), destinationName, 0)
}
