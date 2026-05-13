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

	t.Run("law CRUD", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "law", "add", "-k", "no-secrets", "-s", "warning", "-b", "Never persist secrets", "--no-edit")
		slug := extractSlug(t, out, "law")
		if slug != "no-secrets" {
			t.Fatalf("law add slug = %q, want no-secrets", slug)
		}
		out = runCLI(t, dbPath, configPath, "law", "edit", slug, "-s", "error", "--no-edit")
		// CLI emits the raw severity id; the canonical kit maps "error" to id 3.
		if !strings.Contains(out, `"severity":3`) {
			t.Fatalf("law edit out = %s, want severity=3 (error)", out)
		}
		out = runCLI(t, dbPath, configPath, "law", "list")
		if !strings.Contains(out, "no-secrets") {
			t.Fatalf("law list missing key: %s", out)
		}
		out = runCLI(t, dbPath, configPath, "law", "show", slug)
		if !strings.Contains(out, `"key":"no-secrets"`) || !strings.Contains(out, `"body":`) {
			t.Fatalf("law show envelope missing fields: %s", out)
		}
		runCLI(t, dbPath, configPath, "law", "remove", slug)
		out = runCLI(t, dbPath, configPath, "law", "list")
		if strings.Contains(out, "no-secrets") {
			t.Fatalf("law list still has removed key: %s", out)
		}
	})

	t.Run("skill CRUD", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "skill", "add", "-k", "tui", "-n", "TUI", "--no-edit")
		slug := extractSlug(t, out, "skill")
		if slug != "tui" {
			t.Fatalf("skill add slug = %q, want tui", slug)
		}
		runCLI(t, dbPath, configPath, "skill", "edit", slug, "-n", "Terminal UI", "--no-edit")
		out = runCLI(t, dbPath, configPath, "skill", "list")
		if !strings.Contains(out, "Terminal UI") {
			t.Fatalf("skill list missing rename: %s", out)
		}
		runCLI(t, dbPath, configPath, "skill", "remove", slug)
	})

	t.Run("persona CRUD", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "persona", "add", "-k", "frontend", "-n", "Frontend Agent", "--skill-slug", "implementation", "--no-edit")
		slug := extractSlug(t, out, "persona")
		if slug != "frontend" {
			t.Fatalf("persona add slug = %q, want frontend", slug)
		}
		if !strings.Contains(out, `"skill_keys":["implementation"]`) {
			t.Fatalf("persona add out missing skill_keys: %s", out)
		}
		runCLI(t, dbPath, configPath, "persona", "edit", slug, "-n", "Frontend v2", "--no-edit")
		out = runCLI(t, dbPath, configPath, "persona", "list")
		if !strings.Contains(out, "Frontend v2") {
			t.Fatalf("persona list missing rename: %s", out)
		}
		runCLI(t, dbPath, configPath, "persona", "remove", slug)
	})
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

	cmd := NewRootCommand("test")
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
// skill silently rewrites persona wiring rather than blocking on usage.
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
	// `implementation` skill is referenced by the default engineer persona.
	runCLI(t, dbPath, configPath, "skill", "remove", "implementation")

	out := runCLI(t, dbPath, configPath, "persona", "show", "engineer")
	if strings.Contains(out, `"implementation"`) {
		t.Fatalf("persona still references removed skill: %s", out)
	}
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
	if err := os.WriteFile(payload, []byte("---\nname: Stubbed\ndescription: Written by stub editor\n---\nstub body content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(payload) error = %v", err)
	}
	if err := os.WriteFile(stubPath, []byte("#!/bin/sh\ncp \""+payload+"\" \"$1\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(stub) error = %v", err)
	}
	t.Setenv("EDITOR", stubPath)
	t.Setenv("VISUAL", "")

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	out := runCLI(t, dbPath, configPath, "skill", "add", "-k", "edited", "-n", "Edited skill")
	if !strings.Contains(out, `"name":"Stubbed"`) {
		t.Fatalf("skill add did not pick up stub editor output: %s", out)
	}
	if !strings.Contains(out, `"body":"stub body content"`) {
		t.Fatalf("skill add did not pick up stub body: %s", out)
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
