package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIConfigWhyResolverPicksLocalOverGlobal(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.workflow.active")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "local" {
		t.Fatalf("source = %v, want local (standalone discovery should win)", data["source"])
	}
	if data["value"] != "izakaya" {
		t.Fatalf("value = %v, want izakaya", data["value"])
	}
}

func TestCLIConfigWhyLayerFlagFiltersToGlobal(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.workflow.active", "--layer", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "global" || data["value"] != "omakase" {
		t.Fatalf("--layer global = %+v, want source=global value=omakase", data)
	}
}

func TestCLIConfigWhyMissingKeyIsNotSet(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "no.such.key")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "not_set" {
		t.Fatalf("source = %v, want not_set", data["source"])
	}
}

func TestCLIConfigWhyLayerLocalWithoutInstallIsNotSet(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.workflow.active", "--layer", "local")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "not_set" {
		t.Fatalf("source = %v, want not_set (no .omakiten/ above CWD)", data["source"])
	}
}

func TestCLIConfigWhyRejectsBadLayer(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "why", "key", "--layer", "weird")
}

func TestCLIConfigDiffReportsChanges(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "izakaya")

	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	entries := data["diff"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected non-empty diff between izakaya and omakase")
	}
}

func TestCLIConfigDiffIdenticalFilesIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	t.Chdir(t.TempDir())
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")

	out := runCLI(t, dbPath, globalConfig, "config", "diff", globalConfig, globalConfig)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if entries := data["diff"].([]any); len(entries) != 0 {
		t.Fatalf("expected empty diff for identical files, got %v", entries)
	}
}

func TestCLIConfigDiffLocalPathSpec(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config", "omakase.yaml")
	repoA := filepath.Join(tmp, "a")
	repoB := filepath.Join(tmp, "b")
	for _, p := range []string{repoA, repoB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", p, err)
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
	if len(data["diff"].([]any)) == 0 {
		t.Fatalf("expected diff between izakaya@a and kaiseki@b")
	}
}
