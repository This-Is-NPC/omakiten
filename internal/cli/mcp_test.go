package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// TestCLIPromptsListSurfacesTiers asserts the `okt mcp prompts --list`
// command-surface listing (#379 AC#1): the CLI exposes the full v2 command
// surface grouped by routing tier, with the granular tier sub-grouped by object
// namespace, consistent with the MCP prompt surface. The listing must name
// every command in agent.CommandNames() and reflect the tier/object structure
// in its headings so a user can see the namespacing from the shell.
func TestCLIPromptsListSurfacesTiers(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--db", dbPath, "--config", configPath, "mcp", "prompts", "--list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(mcp prompts --list) error = %v, output = %s", err, out.String())
	}
	body := out.String()

	// Tier headings present, consistent with command_registry tiers.
	for _, heading := range []string{"Orchestrators (", "System (", "Granular ("} {
		if !strings.Contains(body, heading) {
			t.Fatalf("listing missing tier heading %q:\n%s", heading, body)
		}
	}

	// Granular object namespaces are surfaced as sub-groups.
	for _, object := range []string{"task (", "plan (", "project (", "note ("} {
		if !strings.Contains(body, object) {
			t.Fatalf("listing missing granular object group %q:\n%s", object, body)
		}
	}

	// Every registered command appears in the listing, on its own row, with its
	// prompts/list description — so the CLI surface stays in lockstep with the
	// MCP prompt surface.
	for _, name := range agent.CommandNames() {
		row := name + " "
		if !strings.Contains(body, row) {
			t.Fatalf("listing missing command %q:\n%s", name, body)
		}
	}

	// The count in the title must match the registry.
	if !strings.Contains(body, "40 commands") {
		t.Fatalf("listing title does not report 40 commands:\n%s", body)
	}
}
