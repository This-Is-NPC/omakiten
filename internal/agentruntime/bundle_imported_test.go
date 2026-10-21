package agentruntime

import (
	"context"
	"path/filepath"
	"testing"

	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// TestBundleImportedOnlyWhenTheBundleChanges verifies a process that loads
// the bundle its project last imported records no bundle.imported, and one
// that loads changed content does.
func TestBundleImportedOnlyWhenTheBundleChanges(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv(paths.HomeEnv, tmp)
	opts := Options{DBPath: filepath.Join(tmp, "omakiten.db"), ConfigPath: filepath.Join(tmp, "config", "omakase.yaml"), CWD: tmp}
	open := func() int {
		t.Helper()
		rt, err := Open(ctx, opts)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		events, err := rt.Store().ListRecentEvents(ctx, domain.EventTypeBundleImported, 100)
		if err != nil {
			t.Fatalf("ListRecentEvents: %v", err)
		}
		if err := rt.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		return len(events)
	}
	first := open()
	if first == 0 {
		t.Fatal("the first load recorded no bundle.imported")
	}
	if again := open(); again != first {
		t.Fatalf("an unchanged reload recorded %d bundle.imported, want %d", again, first)
	}
	appendRuntimeTopLevelYAML(t, opts.ConfigPath, "# changed\n")
	if changed := open(); changed != first+1 {
		t.Fatalf("a changed bundle recorded %d bundle.imported, want %d", changed, first+1)
	}
}
