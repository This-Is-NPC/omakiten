//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUnsupportedPlatformSnapshotSourceFailsClosed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.db")
	destinationPath := filepath.Join(dir, "snapshot.db")
	if _, _, _, _, _, err := pinSnapshotSource(sourcePath); err == nil {
		t.Fatal("pinSnapshotSource succeeded on unsupported platform")
	}
	if err := SnapshotDatabase(ctx, sourcePath, destinationPath); err == nil {
		t.Fatal("SnapshotDatabase succeeded on unsupported platform")
	}
}
