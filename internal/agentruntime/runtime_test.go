package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenMaterializesRuntime verifies that Open boots a runtime end-to-end:
// it creates the data dir, writes a default config, opens the sqlite store
// and stitches together the agent.Service. The previous home of this test
// (internal/agent/service_test.go) moved here when the bootstrap was
// extracted out of internal/agent — keeping the assertions gives the
// composition root unit-level coverage of its own seam.
func TestOpenMaterializesRuntime(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")

	rt, err := Open(ctx, Options{DBPath: dbPath, ConfigPath: configPath, CWD: tmp})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rt.Close() }()

	if rt.DBPath() != dbPath {
		t.Fatalf("Runtime.DBPath() = %q, want %q", rt.DBPath(), dbPath)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("default config was not materialized: %v", err)
	}
	if rt.Service() == nil {
		t.Fatal("Runtime.Service() = nil")
	}
	if rt.Store() == nil {
		t.Fatal("Runtime.Store() = nil")
	}
}
