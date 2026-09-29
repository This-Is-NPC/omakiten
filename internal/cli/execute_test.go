package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
)

func TestExecuteConfigRecoveryFromAncestor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OMAKITEN_HOME", filepath.Join(home, "global"))
	root := filepath.Join(home, "Projects", ".omakiten")
	seed, err := config.SeedInstall(root, "omakase", true)
	if err != nil {
		t.Fatal(err)
	}
	stale := "version: 1\nmcp: {}\n"
	writeFile(t, seed.Path, stale)
	custom := filepath.Join(root, "templates", "custom", "mine.md")
	customBody := "---\nname: My template\n---\nUser template\n"
	writeFile(t, custom, customBody)
	repo := filepath.Join(home, "Projects", "nested", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	db := filepath.Join(home, "data", "test.db")
	var stdout, stderr bytes.Buffer
	called := false
	cmd := NewRootCommand("test", func(context.Context, agentruntime.Session) error {
		called = true
		return nil
	})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--db", db, "tui"})
	if code := Execute(cmd); code != 1 || called || stdout.Len() != 0 {
		t.Fatalf("broken config: exit=%d, runner=%v, stdout=%q", code, called, stdout.String())
	}
	repair := updateDefaultsManualCommandForConfig(seed.Path)
	for _, want := range []string{"unknown_schema_key", seed.Path, "field mcp", repair, "custom/", "okt tui --help"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q: %s", want, stderr.String())
		}
	}
	readBackEquals(t, seed.Path, stale)
	runCLI(t, db, seed.Path, "config", "refresh-defaults")
	runCLI(t, db, seed.Path, "config", "validate")
	readBackEquals(t, custom, customBody)
	readBackEquals(t, filepath.Join(root, "config", ".active"), "omakase.yaml\n")
	if err := cmd.Execute(); err != nil || !called {
		t.Fatalf("repaired TUI: runner=%v, error=%v", called, err)
	}
}

func TestExecuteCustomEntityRepair(t *testing.T) {
	fixture := newCLIDBFixture(t, "test.db")
	custom := filepath.Join(fixture.root, "templates", "custom", "mine.md")
	writeFile(t, custom, "---\nname: My template\nunsupported: true\n---\nUser template\n")
	var stderr bytes.Buffer
	called := false
	cmd := NewRootCommand("test", func(context.Context, agentruntime.Session) error {
		called = true
		return nil
	})
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--db", fixture.dbPath, "--config", fixture.configPath, "tui"})
	if code := Execute(cmd); code != 1 || called {
		t.Fatalf("invalid custom template: exit=%d, runner=%v", code, called)
	}
	if !strings.Contains(stderr.String(), "${EDITOR:-vi} "+custom) || strings.Contains(stderr.String(), "refresh-defaults") {
		t.Fatalf("custom error recommends the wrong repair: %s", &stderr)
	}
}

func TestExecutePreservesJSONAndShowsArgumentHelp(t *testing.T) {
	cases := map[string]struct {
		args []string
		code int
		want string
	}{
		"JSON failure":    {[]string{"config", "refresh-defaults"}, 1, `"help_command":`},
		"unknown flag":    {[]string{"config", "refresh-defaults", "--unknown"}, 1, `"help_command":`},
		"extra argument":  {[]string{"config", "refresh-defaults", "extra"}, 1, `"help_command":`},
		"unknown command": {[]string{"confgi"}, 1, `"help_command":`},
		"help":            {[]string{"config", "refresh-defaults", "--help"}, 0, "--config"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := NewRootCommand("test")
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"--config", filepath.Join(t.TempDir(), "config", "omakase.yaml")}, tc.args...))
			if got := Execute(cmd); got != tc.code || stderr.Len() != 0 {
				t.Fatalf("exit=%d, want %d: %s %s", got, tc.code, &stdout, &stderr)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("unusable output: %s", &stdout)
			}
			if tc.code != 0 && !json.Valid(stdout.Bytes()) {
				t.Fatalf("invalid failure JSON: %s", &stdout)
			}
		})
	}
}
