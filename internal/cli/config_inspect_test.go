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
	setPresetCommentLimit(t, filepath.Join(repo, ".omakiten", "config.yaml"), 321)

	out := runCLI(t, dbPath, "", "config", "why", "config.agent.max_comment_chars")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "local" {
		t.Fatalf("source = %v, want local (standalone discovery should win)", data["source"])
	}
	if data["value"] != float64(321) {
		t.Fatalf("value = %v, want 321", data["value"])
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
	setPresetCommentLimit(t, globalConfig, 123)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	setPresetCommentLimit(t, filepath.Join(repo, ".omakiten", "config.yaml"), 321)

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.agent.max_comment_chars", "--layer", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if data["source"] != "global" || data["value"] != float64(123) {
		t.Fatalf("--layer global = %+v, want source=global value=123", data)
	}
	data = decodeEnvelope(t, runCLI(t, dbPath, globalConfig, "config", "why", "config.agent.max_comment_chars"))["data"].(map[string]any)
	if data["source"] != "explicit" || data["value"] != float64(123) {
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

	out := runCLI(t, dbPath, globalConfig, "config", "why", "config.agent.max_comment_chars", "--layer", "local")
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
	setPresetCommentLimit(t, filepath.Join(repo, ".omakiten", "config.yaml"), 321)

	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local", "global")
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	entries := data["diff"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected non-empty diff for distinct comment limits")
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
	setPresetCommentLimit(t, filepath.Join(repoA, ".omakiten", "config.yaml"), 321)
	t.Chdir(repoB)
	runCLI(t, dbPath, globalConfig, "config", "init", "--scope", "local", "--preset", "omakase")
	t.Chdir(tmp)

	out := runCLI(t, dbPath, globalConfig, "config", "diff", "local:"+repoA, "local:"+repoB)
	envelope := decodeEnvelope(t, out)
	data := envelope["data"].(map[string]any)
	if len(data["diff"].([]any)) == 0 {
		t.Fatalf("expected diff between distinct project comment limits")
	}
}
