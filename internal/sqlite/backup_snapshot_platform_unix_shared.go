//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package sqlite

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func renameSnapshotLink(_ *os.Root, directory *os.File, oldName, newName string) error {
	return unix.Renameat(int(directory.Fd()), oldName, int(directory.Fd()), newName)
}

func removeSnapshotLink(_ *os.Root, directory *os.File, name string) error {
	return unix.Unlinkat(int(directory.Fd()), name, 0)
}

func syncPublishedSnapshotDirectory(directory *os.File) error {
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync snapshot directory: %w", err)
	}
	return nil
}
