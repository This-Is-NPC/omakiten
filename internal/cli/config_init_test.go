package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigInitLocalCreatesAtCWD(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	cwd := filepath.Join(tmp, "repo", "src", "pkg")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(cwd) error = %v", err)
	}
	t.Chdir(cwd)

	out := runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "kaiseki")
	wantPath := filepath.Join(cwd, ".omakiten", "config", "kaiseki.yaml")
	if !strings.Contains(out, `"scope":"local"`) || !strings.Contains(out, wantPath) {
		t.Fatalf("output = %s, want scope=local path=%s", out, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", wantPath, err)
	}
	if !strings.Contains(string(data), "key: kaiseki") {
		t.Fatalf("preset content = %s, want kaiseki", string(data))
	}

	active, err := os.ReadFile(filepath.Join(cwd, ".omakiten", "config", ".active"))
	if err != nil {
		t.Fatalf("ReadFile(.active) error = %v", err)
	}
	if got := strings.TrimSpace(string(active)); got != "kaiseki.yaml" {
		t.Fatalf(".active = %q, want kaiseki.yaml", got)
	}
}

func TestCLIConfigInitLocalNoWalkUp(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	// Plant a .omakiten/ at an ancestor so any accidental walk-up would
	// find it. The CWD-literal contract means the new library entry must
	// still land under the CWD, not the ancestor.
	ancestor := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(ancestor, ".omakiten", "config"), 0o755); err != nil {
		t.Fatalf("MkdirAll(ancestor .omakiten) error = %v", err)
	}
	cwd := filepath.Join(ancestor, "sub")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(cwd) error = %v", err)
	}
	t.Chdir(cwd)

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	cwdPath := filepath.Join(cwd, ".omakiten", "config", "izakaya.yaml")
	ancestorPath := filepath.Join(ancestor, ".omakiten", "config", "izakaya.yaml")
	if _, err := os.Stat(cwdPath); err != nil {
		t.Fatalf("expected file at CWD %s, error = %v", cwdPath, err)
	}
	if _, err := os.Stat(ancestorPath); err == nil {
		t.Fatalf("ancestor .omakiten/config/izakaya.yaml was written; CWD-literal contract broken")
	}
}

func TestCLIConfigInitGlobalCreatesAtConfigRoot(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	t.Chdir(t.TempDir())

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "shokunin")

	want := filepath.Join(tmp, "global", "config", "shokunin.yaml")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", want, err)
	}
	if !strings.Contains(string(data), "key: shokunin") {
		t.Fatalf("global preset content = %s, want shokunin", string(data))
	}
}

func TestCLIConfigInitNoOpOnIdenticalRerun(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	cwd := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(cwd) error = %v", err)
	}
	t.Chdir(cwd)

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")
	rerun := runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")
	if !strings.Contains(rerun, `"no_op":true`) {
		t.Fatalf("rerun output = %s, want no_op:true", rerun)
	}
}

func TestCLIConfigInitErrorsOnTamperedWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	cwd := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(cwd) error = %v", err)
	}
	t.Chdir(cwd)

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")

	yamlPath := filepath.Join(cwd, ".omakiten", "config", "omakase.yaml")
	if err := os.WriteFile(yamlPath, []byte("# tampered\n"), 0o644); err != nil {
		t.Fatalf("tamper write error = %v", err)
	}
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "init", "--scope", "local", "--preset", "omakase")
}

func TestCLIConfigInitForceOverwritesTampered(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	cwd := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll(cwd) error = %v", err)
	}
	t.Chdir(cwd)

	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	yamlPath := filepath.Join(cwd, ".omakiten", "config", "omakase.yaml")
	if err := os.WriteFile(yamlPath, []byte("# tampered\n"), 0o644); err != nil {
		t.Fatalf("tamper write error = %v", err)
	}

	out := runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase", "--force")
	if !strings.Contains(out, `"overwritten":true`) {
		t.Fatalf("force output = %s, want overwritten:true", out)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if !strings.Contains(string(data), "key: omakase") {
		t.Fatalf("file content = %s, want omakase preset", string(data))
	}
}

func TestCLIConfigInitRejectsInvalidScope(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "init", "--scope", "weird", "--preset", "omakase")
}

func TestCLIConfigInitRejectsUnknownPreset(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "init", "--scope", "local", "--preset", "no-such-preset")
}
