package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIEntityCommands exercises the slug-based skill/law/persona CRUD path,
// including the --no-edit fast path so the tests don't need a real $EDITOR.
func TestCLIEntityCommands(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	t.Run("law CRUD", func(t *testing.T) { testCLILawCRUD(t, dbPath, configPath) })
	t.Run("skill CRUD", func(t *testing.T) { testCLISkillCRUD(t, dbPath, configPath) })
	t.Run("persona CRUD", func(t *testing.T) { testCLIPersonaCRUD(t, dbPath, configPath) })
}

func testCLILawCRUD(t *testing.T, dbPath, configPath string) {
	t.Helper()
	out := runCLI(t, dbPath, configPath, "law", "add", "-k", "no-secrets", "-s", "warning", "-b", "Never persist secrets", "--no-edit")
	slug := extractSlug(t, out, "law")
	if slug != "no-secrets" {
		t.Fatalf("law add slug = %q, want no-secrets", slug)
	}
	out = runCLI(t, dbPath, configPath, "law", "edit", slug, "-s", "error", "--name", "Protect secrets", "--body", "Keep credentials outside the board", "--no-edit")
	if !strings.Contains(out, `"severity":3`) {
		t.Fatalf("law edit out = %s, want severity=3 (error)", out)
	}
	out = runCLI(t, dbPath, configPath, "law", "list")
	if !strings.Contains(out, "no-secrets") {
		t.Fatalf("law list missing key: %s", out)
	}
	out = runCLI(t, dbPath, configPath, "law", "show", slug)
	if !strings.Contains(out, `"slug":"no-secrets"`) || !strings.Contains(out, `"body":`) {
		t.Fatalf("law show envelope missing fields: %s", out)
	}
	runCLI(t, dbPath, configPath, "law", "remove", slug)
	out = runCLI(t, dbPath, configPath, "law", "list")
	if strings.Contains(out, "no-secrets") {
		t.Fatalf("law list still has removed key: %s", out)
	}
}

func testCLISkillCRUD(t *testing.T, dbPath, configPath string) {
	t.Helper()
	out := runCLI(t, dbPath, configPath, "skill", "add", "-k", "tui", "-n", "TUI", "--no-edit")
	slug := extractSlug(t, out, "skill")
	if slug != "tui" {
		t.Fatalf("skill add slug = %q, want tui", slug)
	}
	runCLI(t, dbPath, configPath, "skill", "edit", slug, "-n", "Terminal UI", "--description", "Build terminal interfaces", "--no-edit")
	out = runCLI(t, dbPath, configPath, "skill", "list")
	if !strings.Contains(out, "Terminal UI") {
		t.Fatalf("skill list missing rename: %s", out)
	}
	runCLI(t, dbPath, configPath, "skill", "remove", slug)
}

func testCLIPersonaCRUD(t *testing.T, dbPath, configPath string) {
	t.Helper()
	out := runCLI(t, dbPath, configPath, "persona", "add", "-k", "frontend", "-n", "Frontend Agent", "--skill-slug", "implementation", "--no-edit")
	slug := extractSlug(t, out, "persona")
	if slug != "frontend" {
		t.Fatalf("persona add slug = %q, want frontend", slug)
	}
	if !strings.Contains(out, `"skill_keys":["implementation"]`) {
		t.Fatalf("persona add out missing skill_keys: %s", out)
	}
	runCLI(t, dbPath, configPath, "persona", "edit", slug, "-n", "Frontend v2", "--description", "Maintain interfaces", "--skill-slug", "implementation", "--no-edit")
	out = runCLI(t, dbPath, configPath, "persona", "list")
	if !strings.Contains(out, "Frontend v2") {
		t.Fatalf("persona list missing rename: %s", out)
	}
	runCLI(t, dbPath, configPath, "persona", "remove", slug)
}

func TestCLILawAddRejectsInvalidSeverity(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	cmd := NewRootCommand("test", Runners{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--db", dbPath, "--config", configPath, "law", "add", "-k", "x", "-s", "fatal", "-b", "anything", "--no-edit"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want validation failure")
	}
	var envelope map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &envelope); err != nil {
		t.Fatalf("Unmarshal() error = %v, out = %s", err, out.String())
	}
	if envelope["code"] != "validation_error" {
		t.Fatalf("code = %v, want validation_error (%s)", envelope["code"], out.String())
	}
}

// TestCLISkillRemovePrunesPersonaRefs covers the requirement that removing a
// skill does not error when personas or commands still reference it.
func TestCLISkillRemovePrunesPersonaRefs(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	// `implementation` skill is in the default Builder persona's repertoire.
	runCLI(t, dbPath, configPath, "skill", "remove", "implementation")

	out := runCLI(t, dbPath, configPath, "skill", "list")
	if strings.Contains(out, `"slug":"implementation"`) {
		t.Fatalf("skill list still has removed skill: %s", out)
	}
	runCLIExpectError(t, dbPath, configPath, "skill_not_found", "skill", "show", "implementation")
}

// TestCLIEditorShellOut spins up a stub editor (a tiny sh script) that writes
// a known body into the entity file. After the CLI returns, the bundle must
// reflect the stub-written content.
func TestCLIEditorShellOut(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("/bin/sh unavailable: %v", err)
	}
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	t.Chdir(projectRoot)
	stubPath := filepath.Join(tmp, "editor.sh")
	payload := filepath.Join(tmp, "payload.md")
	if err := os.WriteFile(stubPath, []byte("#!/bin/sh\ncp \""+payload+"\" \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(stub) error = %v", err)
	}
	t.Setenv("EDITOR", stubPath)
	t.Setenv("VISUAL", "")

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	for _, entity := range []string{"skill", "law", "persona"} {
		t.Run(entity, func(t *testing.T) {
			checkEntityEditorRoundTrip(t, dbPath, configPath, payload, entity)
		})
	}
}

func extractSlug(t *testing.T, payload string, key string) string {
	t.Helper()
	var envelope struct {
		Data map[string]struct {
			Slug string `json:"slug"`
			Key  string `json:"key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", payload, err)
	}
	entry, ok := envelope.Data[key]
	if !ok {
		t.Fatalf("payload missing %q: %s", key, payload)
	}
	if entry.Key != "" {
		return entry.Key
	}
	return entry.Slug
}

func checkEntityEditorRoundTrip(t *testing.T, dbPath, configPath, payload, entity string) {
	t.Helper()
	metadata := "name: Stubbed\ndescription: Written by stub editor\nschema_version: 2\n"
	if entity == "law" {
		metadata = "name: Stubbed\nseverity: error\n"
	}
	writeFile(t, payload, "---\n"+metadata+"---\nstub body content\n")
	args := []string{entity, "add", "-k", "edited", "-n", "Edited entity"}
	if entity == "law" {
		args = append(args, "--body", "Initial law body")
	}
	out := runCLI(t, dbPath, configPath, args...)
	if !strings.Contains(out, `"name":"Stubbed"`) || !strings.Contains(out, `"body":"stub body content"`) {
		t.Fatalf("editor output not imported: %s", out)
	}
	writeFile(t, payload, "---\n"+strings.Replace(metadata, "Stubbed", "Revised", 1)+"---\nrevised body content\n")
	out = runCLI(t, dbPath, configPath, entity, "edit", "edited")
	if !strings.Contains(out, `"name":"Revised"`) || !strings.Contains(out, `"body":"revised body content"`) {
		t.Fatalf("editor changes not imported: %s", out)
	}
}
