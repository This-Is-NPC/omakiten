package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigShowGlobalReturnsRawYaml(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	t.Chdir(t.TempDir())
	// Seed the global config so the active file exists with known content.
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "show", "--scope", "global")
	if !strings.Contains(out, `"scope":"global"`) || !strings.Contains(out, "key: omakase") {
		t.Fatalf("show output = %s, want scope=global containing key: omakase", out)
	}
}

func TestCLIConfigShowLocalDiscoversViaWalkUp(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "kaiseki")

	deep := filepath.Join(repo, "src", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll(deep) error = %v", err)
	}
	t.Chdir(deep)

	out := runCLI(t, dbPath, globalConfig, "config", "show", "--scope", "local")
	if !strings.Contains(out, `"scope":"local"`) || !strings.Contains(out, "key: kaiseki") {
		t.Fatalf("local show output = %s, want kaiseki content via walk-up", out)
	}
}

func TestCLIConfigShowLocalErrorsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	bare := filepath.Join(tmp, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	// Plant a sibling .omakiten/ at a SIBLING of bare to prove walk-up
	// stops at filesystem boundaries; the CLI must still report not-found.
	t.Chdir(bare)
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "show", "--scope", "local")
}

func TestCLIConfigPathGlobalReturnsConfigDir(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig, "config", "path", "--scope", "global")
	wantDir := filepath.Join(tmp, "global", "config")
	if !strings.Contains(out, `"scope":"global"`) || !strings.Contains(out, wantDir) {
		t.Fatalf("path output = %s, want %s", out, wantDir)
	}
}

func TestCLIConfigPathLocalReturnsDiscoveredDir(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "shokunin")

	deep := filepath.Join(repo, "deep", "nest")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll(deep) error = %v", err)
	}
	t.Chdir(deep)

	out := runCLI(t, dbPath, globalConfig, "config", "path", "--scope", "local")
	wantDir := filepath.Join(repo, ".omakiten")
	if !strings.Contains(out, `"scope":"local"`) || !strings.Contains(out, wantDir) {
		t.Fatalf("path output = %s, want %s", out, wantDir)
	}
}

func TestCLIConfigPathLocalErrorsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	bare := filepath.Join(tmp, "bare")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	t.Chdir(bare)
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "path", "--scope", "local")
}
