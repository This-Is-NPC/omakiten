package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCleanDocsTreePassesWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	docPath := writeTestFile(t, root, ".docs/README.md", "# Docs\n\nCurrent structure.\n")

	if err := run(root, true); err != nil {
		t.Fatalf("check mode returned error: %v", err)
	}
	if err := run(root, false); err != nil {
		t.Fatalf("refresh mode returned error: %v", err)
	}

	got, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Docs\n\nCurrent structure.\n" {
		t.Fatalf("doc was rewritten unexpectedly:\n%s", got)
	}
}

func TestRunCheckRejectsGeneratedDir(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".docs/README.md", "# Docs\n")
	writeTestFile(t, root, ".docs/_generated/old.md", "legacy\n")

	err := run(root, true)
	if err == nil {
		t.Fatal("check mode succeeded with .docs/_generated present")
	}
	if !strings.Contains(err.Error(), ".docs/_generated exists") {
		t.Fatalf("error did not mention generated dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".docs/_generated/old.md")); err != nil {
		t.Fatalf("check mode should not remove generated files: %v", err)
	}
}

func TestRunRefreshRemovesGeneratedDir(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".docs/README.md", "# Docs\n")
	writeTestFile(t, root, ".docs/_generated/old.md", "legacy\n")

	if err := run(root, false); err != nil {
		t.Fatalf("refresh mode returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".docs/_generated")); !os.IsNotExist(err) {
		t.Fatalf("generated dir still exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunRejectsLegacyMarkersOutsideFences(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".docs/guide.md", "# Guide\n\n<!-- BEGIN include:old.md -->\nlegacy\n<!-- END include -->\n")

	err := run(root, true)
	if err == nil {
		t.Fatal("check mode succeeded with legacy markers present")
	}
	if !strings.Contains(err.Error(), ".docs/guide.md:3 contains legacy marker") {
		t.Fatalf("error did not include marker location: %v", err)
	}
}

func TestRunAllowsLegacyMarkersInsideFences(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".docs/authoring.md", "# Authoring\n\n```md\n<!-- BEGIN include:example.md -->\n<!-- SECTION:example -->\n<!-- END include -->\n```\n")

	if err := run(root, true); err != nil {
		t.Fatalf("check mode rejected fenced marker example: %v", err)
	}
}

func writeTestFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
