//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package sqlite

import (
	"errors"
	"os"
)

func bindSnapshotStage(_, _ *os.File, _ string) (string, func() error, func() error, error) {
	return "", nil, nil, errors.New("secure SQLite staging is unsupported on this platform")
}

func validateSnapshotStageForPublication(_ *os.Root, _ string, _ os.FileInfo) error {
	return errors.New("secure snapshot publication is unsupported on this platform")
}

func linkSnapshotFile(_, _ *os.File, _ string, _ *os.File, _ string) error {
	return errors.New("secure snapshot publication is unsupported on this platform")
}

func renameSnapshotLink(_ *os.Root, _ *os.File, _, _ string) error {
	return errors.New("secure snapshot publication is unsupported on this platform")
}

func removeSnapshotLink(_ *os.Root, _ *os.File, _ string) error {
	return errors.New("secure snapshot publication rollback is unsupported on this platform")
}

func syncPublishedSnapshotDirectory(_ *os.File) error {
	return errors.New("secure snapshot directory sync is unsupported on this platform")
}
