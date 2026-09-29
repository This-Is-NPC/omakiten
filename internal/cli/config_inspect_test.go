package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIConfigWhyResolverPicksLocalOverGlobal(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLI(t, dbPath, filepath.Join(repo, ".omakiten", "config.yaml"), "config", "language", "set", "--agent", "Portuguese")

	out := runCLI(t, dbPath, "", "config", "why", "config.languages.agent_output")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "local" {
		t.Fatalf("source = %v, want local (standalone discovery should win)", data["source"])
	}
	if data["value"] != "Portuguese" {
		t.Fatalf("value = %v, want Portuguese", data["value"])
	}
}

func TestCLIConfigWhyLayerFlagFiltersToGlobal(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, dbPath, globalConfig, "config", "language", "set", "--agent", "English")
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLI(t, dbPath, filepath.Join(repo, ".omakiten", "config.yaml"), "config", "language", "set", "--agent", "Portuguese")

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.languages.agent_output", "--layer", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "global" || data["value"] != "English" {
		t.Fatalf("--layer global = %+v, want source=global value=English", data)
	}
	data = decodeEnvelope(t, runCLI(t, dbPath, globalConfig, "config", "why", "config.languages.agent_output"))["data"].(map[string]any)
	if data["source"] != "explicit" || data["value"] != "English" {
		t.Fatalf("explicit config = %+v", data)
	}
}

func TestCLIConfigInspectionExpandsImports(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "config", "left.yaml")
	right := filepath.Join(root, "config", "right.yaml")
	fragment := filepath.Join(root, "config", "shared.yaml")
	writeFile(t, left, "config:\n  from: ./shared.yaml\n")
	writeFile(t, fragment, "theme:\n  active: borrowed\n")
	writeFile(t, right, "config:\n  theme:\n    active: borrowed\n")
	db := filepath.Join(root, "state.db")
	data := decodeEnvelope(t, runCLI(t, db, left, "config", "why", "config.theme.active"))["data"].(map[string]any)
	if data["value"] != "borrowed" || data["source"] != "explicit" {
		t.Fatalf("import inspection = %+v", data)
	}
	diff := decodeEnvelope(t, runCLI(t, db, left, "config", "diff", left, right))["data"].(map[string]any)
	if len(diff["diff"].([]any)) != 0 {
		t.Fatalf("equivalent imports differ: %+v", diff)
	}
}

func TestCLIConfigWhyMissingKeyIsNotSet(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
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
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
	t.Chdir(t.TempDir())

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.languages.agent_output", "--layer", "local")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "not_set" {
		t.Fatalf("source = %v, want not_set (no .omakiten/ above CWD)", data["source"])
	}
}

func TestCLIConfigWhyRejectsBadLayer(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
	t.Chdir(t.TempDir())
	runCLIExpectError(t, dbPath, globalConfig, "validation_error", "config", "why", "key", "--layer", "weird")
}

func TestCLIConfigDiffReportsChanges(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll = %v", err)
	}
	t.Chdir(repo)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "global", "--preset", "omakase")
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLI(t, dbPath, filepath.Join(repo, ".omakiten", "config.yaml"), "config", "language", "set", "--agent", "Portuguese")

	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	entries := data["diff"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected non-empty diff for distinct language settings")
	}
}

func TestCLIConfigDiffIdenticalFilesIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
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
	globalConfig := filepath.Join(tmp, "global", "config.yaml")
	repoA := filepath.Join(tmp, "a")
	repoB := filepath.Join(tmp, "b")
	for _, p := range []string{repoA, repoB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", p, err)
		}
	}
	t.Chdir(repoA)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLI(t, dbPath, filepath.Join(repoA, ".omakiten", "config.yaml"), "config", "language", "set", "--agent", "Portuguese")
	t.Chdir(repoB)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	t.Chdir(tmp)

	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local:"+repoA, "local:"+repoB)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if len(data["diff"].([]any)) == 0 {
		t.Fatalf("expected diff between distinct project language settings")
	}
}
