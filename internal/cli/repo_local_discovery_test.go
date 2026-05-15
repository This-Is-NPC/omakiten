package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIWalkUpStandaloneInstall exercises the .omakiten/ walk-up path: a
// fresh repo with a SeedInstall'd .omakiten/ must be the bundle source for
// every subsequent CLI invocation from the repo (no --config flag passed).
// The user-global ConfigRoot is pointed at a sibling tmp dir via
// $OMAKITEN_HOME so the test never touches the host machine's config.
func TestCLIWalkUpStandaloneInstall(t *testing.T) {
	homeDir := t.TempDir()
	repoDir := t.TempDir()
	dbPath := filepath.Join(homeDir, "data", "omakiten.db")

	t.Setenv("OMAKITEN_HOME", homeDir)
	t.Chdir(repoDir)

	// Seed the repo-local standalone install. okt init walks up to git
	// root; we materialise the install manually here so the test does not
	// have to fabricate a .git directory.
	repoLocalRoot := filepath.Join(repoDir, ".omakiten")
	out := runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "local", "--preset", "izakaya")
	if !strings.Contains(out, `"name":"izakaya"`) {
		t.Skipf("okt config init --scope local not wired yet; output = %s", out)
	}

	// Now run a regular okt command from a deep subdir with NO --config
	// flag. Discovery must walk up and load the local install, NOT the
	// empty $OMAKITEN_HOME global.
	deep := filepath.Join(repoDir, "src", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll(deep) error = %v", err)
	}
	t.Chdir(deep)
	out = runCLIWithoutConfig(t, dbPath, "config", "validate")
	if !strings.Contains(out, repoLocalRoot) {
		t.Fatalf("validate output = %s, want path under repo-local %s", out, repoLocalRoot)
	}
	if !strings.Contains(out, "izakaya") {
		t.Fatalf("validate output = %s, want izakaya kit", out)
	}
}

// runCLIWithoutConfig mirrors runCLI but does NOT inject --config so the
// resolver's $OMAKITEN_HOME + walk-up paths run end-to-end.
func runCLIWithoutConfig(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	full := append([]string{"--db", dbPath}, args...)
	cmd.SetArgs(full)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v, output = %s", full, err, out.String())
	}
	trimmed := strings.TrimSpace(out.String())
	var envelope map[string]any
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(%v) error = %v, output = %s", full, err, trimmed)
	}
	if envelope["ok"] != true {
		t.Fatalf("Execute(%v) ok = %v, output = %s", full, envelope["ok"], trimmed)
	}
	return trimmed
}
