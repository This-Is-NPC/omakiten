//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package sqlite

import (
	"os"
)

func pinSnapshotSource(string) (string, os.FileInfo, string, func() error, func(string) error, error) {
	return "", nil, "", nil, nil, snapshotSourceValidationError("secure snapshot source opening is unsupported on this platform")
}
