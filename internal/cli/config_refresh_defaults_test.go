package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRefreshDefaultsCommandUsesDirectRefresh(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	cfgPath := filepath.Join(root, "config", "omakase.yaml")

	writeFile(t, filepath.Join(root, "config", ".active"), "kaiseki.yaml\n")
	writeFile(t, filepath.Join(root, "config", "custom", "user.yaml"), "user config\n")
	writeFile(t, filepath.Join(root, "skills", "custom", "user.md"), "user skill\n")
	writeFile(t, filepath.Join(root, "config", "omakase.yaml"), "version: 1\n# flattened stale copy\n")
	writeFile(t, filepath.Join(root, "templates", "stale.md"), "stale template\n")

	out := runCLI(t, dbPath, cfgPath, "config", "refresh-defaults")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["root"] != root || data["refreshed"] != true {
		t.Fatalf("refresh payload = %v, want root=%s refreshed=true", data, root)
	}

	readBackEquals(t, filepath.Join(root, "config", ".active"), "kaiseki.yaml\n")
	readBackEquals(t, filepath.Join(root, "config", "custom", "user.yaml"), "user config\n")
	readBackEquals(t, filepath.Join(root, "skills", "custom", "user.md"), "user skill\n")
	if got := readFile(t, cfgPath); !strings.Contains(got, "merge_from: ./modules/base-config.yaml") {
		t.Fatalf("refresh command flattened or failed to restore shipped config imports:\n%s", got[:min(len(got), 400)])
	}
	if _, err := os.Stat(filepath.Join(root, "templates", "stale.md")); !os.IsNotExist(err) {
		t.Fatalf("stale managed template survived refresh; err=%v", err)
	}
}

func TestConfigRefreshDefaultsRejectsUnsafeConfigDerivedRoot(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "omakiten.db")
	cfgPath := filepath.Join(root, "config", "omakase.yaml")
	writeFile(t, cfgPath, "version: 1\n")
	stale := filepath.Join(root, "templates", "stale.md")
	writeFile(t, stale, "must survive\n")

	envelope := runCLIExpectError(t, dbPath, cfgPath, "validation_error", "config", "refresh-defaults")
	msg, _ := envelope["msg"].(string)
	if !strings.Contains(msg, "refusing to refresh defaults") {
		t.Fatalf("error message = %q, want refusing to refresh defaults", msg)
	}
	readBackEquals(t, stale, "must survive\n")
}
