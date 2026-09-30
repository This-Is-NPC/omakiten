package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCLIRejectsExtraArgumentsAcrossCommandTree(t *testing.T) {
	var paths [][]string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() {
			paths = append(paths, strings.Fields(cmd.CommandPath())[1:])
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(NewRootCommand("test", Runners{}))
	for _, path := range paths {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			args := append(append([]string{}, path...), strings.Fields("unexpected unexpected unexpected unexpected unexpected unexpected")...)
			assertCLIRejectsBeforeWriting(t, args)
		})
	}
}

func TestCLIRejectsInvalidFlagsAndIDsBeforeWriting(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	for _, args := range [][]string{
		{"--project", "one", "--project-id", "1", "list"},
		{"--project-id", "0", "list"},
		{"list", "--limit", "1"},
		{"list", "--parent", "-1"},
		{"logs", "--limit", "-1"},
		{"solution", "confirm", "--solution-id", "0", "--success"},
		{"progress", "record", "--task-id", "-1"},
		{"task", "import", "--file", ""},
		{"task", "create", "--file", ""},
		{"task", "export", "0"},
		{"plan", "export", "plan", "--output", ""},
		{"list", "--db", ""},
		{"config", "validate", "--config", ""},
		{"task", "continue", "0"},
		{"task", "activity", "-1"},
		{"assign", "-1"},
		{"comment", "delete", "0"},
		{"plan", "wave-remove", "0"},
		{"plan", "wave-reorder", "1", "0"},
		{"plan", "assign", "plan", "0", "1"},
		{"setup", "--skip-harnesses", "--harnesses", "agents"},
		{"skill", "add", "--name", "Scaffold"},
		{"persona", "edit", "existing"},
		{"law", "add", "--key", "scaffold"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { assertCLIRejectsBeforeWriting(t, args) })
	}
}

func assertCLIRejectsBeforeWriting(t *testing.T, args []string) {
	t.Helper()
	root := t.TempDir()
	db := filepath.Join(root, "test.db")
	config := filepath.Join(root, "config", "omakase.yaml")
	cmd := NewRootCommand("test", Runners{})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"--db", db, "--config", config}, args...))
	if args[0] == "tui" {
		if Execute(cmd) != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "okt tui --help") {
			t.Fatalf("missing TUI guidance: stdout=%s stderr=%s", &stdout, &stderr)
		}
		return
	}
	if code := Execute(cmd); code != 1 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	envelope := decodeEnvelope(t, stdout.String())
	details, ok := envelope["details"].(map[string]any)
	if envelope["code"] != "validation_error" || !ok || details["help_command"] == "" || details["usage"] == "" {
		t.Fatalf("missing input guidance: %s", &stdout)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("invalid input wrote state: %v, %v", entries, err)
	}
}

func TestCLINotFoundGuidanceKeepsScope(t *testing.T) {
	fixture := newCLIDBFixture(t, "database with spaces.db")
	for _, args := range [][]string{
		{"--project", "absent", "list"},
		{"task", "continue", "999999"},
		{"law", "show", "absent"},
		{"skill", "show", "absent"},
		{"persona", "show", "absent"},
		{"plan", "show", "absent"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := NewRootCommand("test", Runners{})
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetArgs(append([]string{"--db", fixture.dbPath, "--config", fixture.configPath}, args...))
			if Execute(cmd) != 1 {
				t.Fatal("missing entity accepted")
			}
			details := decodeEnvelope(t, stdout.String())["details"].(map[string]any)
			retry, _ := details["suggested_command"].(string)
			if !strings.Contains(retry, "--db "+shellQuoteArg(fixture.dbPath)) || !strings.Contains(retry, "--config "+fixture.configPath) {
				t.Fatalf("recovery lost scope: %s", &stdout)
			}
			if args[0] == "--project" && strings.Contains(retry, "--project") {
				t.Fatalf("recovery repeats missing project: %s", retry)
			}
		})
	}
}

func TestCLILawScopeDoesNotShadowProjectSelection(t *testing.T) {
	fixture := newCLIDBFixture(t, "test.db")
	writeFile(t, fixture.configPath, readFile(t, fixture.configPath)+"\nprojects:\n  - slug: law-target\n    name: Law target\n")
	runCLI(t, fixture.dbPath, fixture.configPath, "--project", "project", "law", "add", "--key", "scoped", "--scope", "project", "--scope-project", "law-target", "--no-edit")
	out := runCLI(t, fixture.dbPath, fixture.configPath, "--project", "project", "law", "show", "scoped")
	if !strings.Contains(out, `"project":"law-target"`) {
		t.Fatalf("law lost scope: %s", out)
	}
}
