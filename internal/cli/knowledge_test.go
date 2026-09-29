package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/contract"
)

func TestCLIInventoryExportsLiveCommandTree(t *testing.T) {
	cmd := NewRootCommand("test")
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"knowledge", "export-cli"})
	if code := Execute(cmd); code != 0 {
		t.Fatalf("export-cli exit = %d: %s", code, &output)
	}
	var inventory contract.CLIInventory
	if err := json.Unmarshal(output.Bytes(), &inventory); err != nil {
		t.Fatal(err)
	}
	for _, item := range inventory.Commands {
		if item.ID == "task.create" && item.Parent == "task" && len(item.Flags) > 0 {
			return
		}
	}
	t.Fatalf("task.create missing from live CLI inventory: %+v", inventory)
}

func TestCLIKnowledgeReadsFilesWithoutIndex(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, profile := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, profile, "init", "--name", "Example", "--slug", "example", "--root", root)
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "docs", "endpoint.md")
	if err := os.WriteFile(path, []byte("---\ntype: Reference\ntitle: Create order\n---\nCalls the API."), 0o600); err != nil {
		t.Fatal(err)
	}
	validated := decodeEnvelope(t, runCLI(t, db, profile, "knowledge", "validate"))["data"].(map[string]any)
	if validated["resources"] != float64(1) {
		t.Fatalf("validate data = %v", validated)
	}
	listed := runCLI(t, db, profile, "knowledge", "list", "--type", "Reference")
	if !strings.Contains(listed, "markdown:docs/endpoint") || strings.Contains(listed, "Calls the API") {
		t.Fatalf("list output = %s", listed)
	}
	shown := runCLI(t, db, profile, "knowledge", "show", "markdown:docs/endpoint")
	if !strings.Contains(shown, "Calls the API") {
		t.Fatalf("show output = %s", shown)
	}
	if err := os.WriteFile(path, []byte("---\ntype: Reference\ntitle: Updated order\n---\nCalls the API."), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := runCLI(t, db, profile, "knowledge", "search", "Updated order"); !strings.Contains(got, "Updated order") {
		t.Fatalf("search did not read changed file: %s", got)
	}
}
