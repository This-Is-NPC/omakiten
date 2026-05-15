package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigShowGlobalPrintsRawYaml(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	// Materialise the global install first so show has something to print.
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "show", "--scope", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["scope"] != "global" {
		t.Fatalf("scope = %v, want global", data["scope"])
	}
	if !strings.Contains(data["content"].(string), "key: omakase") {
		t.Fatalf("content missing kit body, got %q", data["content"])
	}
}

func TestCLIConfigShowLocalWalksUp(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	deep := filepath.Join(repo, "deep", "nest")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll(deep) = %v", err)
	}
	t.Chdir(deep)

	out := runCLI(t, dbPath, globalConfig, "config", "show", "--scope", "local")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["scope"] != "local" {
		t.Fatalf("scope = %v, want local", data["scope"])
	}
	if !strings.Contains(data["content"].(string), "key: izakaya") {
		t.Fatalf("local show should load izakaya overlay, got %q", data["content"])
	}
	wantPathPrefix := filepath.Join(repo, ".omakiten", "config")
	if !strings.HasPrefix(data["path"].(string), wantPathPrefix) {
		t.Fatalf("path = %v, want prefix %s", data["path"], wantPathPrefix)
	}
}

func TestCLIConfigShowLocalErrorsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	bare := filepath.Join(tmp, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(bare)
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "show", "--scope", "local")
}

func TestCLIConfigPathGlobalReturnsConfigRoot(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig, "config", "path", "--scope", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	wantRoot := filepath.Join(tmp, "global")
	if data["path"] != wantRoot {
		t.Fatalf("path = %v, want %s", data["path"], wantRoot)
	}
}

func TestCLIConfigPathLocalReturnsDiscoveredRoot(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "kaiseki")

	deep := filepath.Join(repo, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll(deep) = %v", err)
	}
	t.Chdir(deep)

	out := runCLI(t, dbPath, globalConfig, "config", "path", "--scope", "local")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	wantRoot := filepath.Join(repo, ".omakiten")
	if data["path"] != wantRoot {
		t.Fatalf("path = %v, want %s", data["path"], wantRoot)
	}
}

func TestCLIConfigPathLocalErrorsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	bare := filepath.Join(tmp, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(bare)
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "path", "--scope", "local")
}

func decodeEnvelope(t *testing.T, raw string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &env); err != nil {
		t.Fatalf("decodeEnvelope: %v (raw=%s)", err, raw)
	}
	return env
}
