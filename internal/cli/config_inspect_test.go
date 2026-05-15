package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigWhyGlobalReportsGlobalSource(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.workflow.active")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if got := data["source"]; got != "global" {
		t.Fatalf("source = %v, want global", got)
	}
	if got := data["value"]; got != "omakase" {
		t.Fatalf("value = %v, want omakase", got)
	}
}

func TestCLIConfigWhyLocalShadowsGlobal(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.workflow.active")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if got := data["source"]; got != "local" {
		t.Fatalf("source = %v, want local (overlay should shadow global)", got)
	}
	if got := data["value"]; got != "izakaya" {
		t.Fatalf("value = %v, want izakaya", got)
	}
}

func TestCLIConfigWhyMissingKeyReportsNotSet(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "no.such.key")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if got := data["source"]; got != "not_set" {
		t.Fatalf("source = %v, want not_set", got)
	}
}

func TestCLIConfigWhyLayerLocalNotSetWhenAbsent(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.workflow.active", "--layer", "local")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if got := data["source"]; got != "not_set" {
		t.Fatalf("source = %v, want not_set when --layer local and no .omakiten/", got)
	}
}

func TestCLIConfigWhyRejectsInvalidLayer(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "why", "config.workflow.active", "--layer", "weird")
}

func TestCLIConfigDiffReportsAddedRemovedChanged(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	entries, ok := data["diff"].([]any)
	if !ok {
		t.Fatalf("diff field missing: %v", data)
	}
	if len(entries) == 0 {
		t.Fatalf("expected diff entries between izakaya and omakase, got none")
	}
	hasOp := func(op string) bool {
		for _, e := range entries {
			if m, ok := e.(map[string]any); ok && m["op"] == op {
				return true
			}
		}
		return false
	}
	if !hasOp("changed") {
		t.Fatalf("expected at least one 'changed' entry between presets, got %v", entries)
	}
}

func TestCLIConfigDiffAcceptsRawFilePath(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	// diff omakase against itself via raw path → empty diff.
	out := runCLI(t, dbPath, globalConfig, "config", "diff", globalConfig, globalConfig)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if entries := data["diff"].([]any); len(entries) != 0 {
		t.Fatalf("expected empty diff for identical files, got %v", entries)
	}
}

func TestCLIConfigDiffAcceptsLocalPathSpec(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")

	repoA := filepath.Join(tmp, "a")
	repoB := filepath.Join(tmp, "b")
	for _, p := range []string{repoA, repoB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", p, err)
		}
	}
	t.Chdir(repoA)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")
	t.Chdir(repoB)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "kaiseki")

	t.Chdir(tmp)
	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local:"+repoA, "local:"+repoB)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	entries := data["diff"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected diff between izakaya@a and kaiseki@b, got none")
	}
}

func decodeEnvelope(t *testing.T, raw string) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &env); err != nil {
		t.Fatalf("decodeEnvelope: %v (raw=%s)", err, raw)
	}
	return env
}
