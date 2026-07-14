//go:build linux

package sqlite

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func bindSnapshotStage(staged, _ *os.File, _ string) (string, func() error, func() error, error) {
	for _, root := range []string{"/proc/self/fd", "/dev/fd"} {
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

func validateSnapshotStageForPublication(_ *os.Root, _ string, _ os.FileInfo) error {
	return nil
}

func linkSnapshotFile(staged *os.File, _ *os.File, _ string, destinationDirectory *os.File, destinationName string) error {
	return unix.Linkat(int(staged.Fd()), "", int(destinationDirectory.Fd()), destinationName, unix.AT_EMPTY_PATH)
}
