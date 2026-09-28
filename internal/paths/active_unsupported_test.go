//go:build plan9 || windows

package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveMarkerFailsClosedWithoutNoFollowBackend(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, ActiveConfigStateFile)
	if err := os.WriteFile(marker, []byte("omakase.yaml\n"), 0o600); err != nil {
		t.Fatalf("seed marker: %v", err)
	}

	if _, err := ActiveConfigFileInDir(dir); !errors.Is(err, errUnsupportedActiveMarker) {
		t.Fatalf("ActiveConfigFileInDir error = %v, want unsupported marker backend", err)
	}
	if err := SetActiveConfigInDir(dir, "other.yaml"); !errors.Is(err, errUnsupportedActiveMarker) {
		t.Fatalf("SetActiveConfigInDir error = %v, want unsupported marker backend", err)
	}
}
