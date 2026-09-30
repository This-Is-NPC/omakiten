package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"omakiten/internal/workfile"
)

const planDocument = `---
type: Omakiten Plan
title: File transport
tags: [portable]
custom: {reviewer: team}
omakiten:
  version: 1
  slug: file-transport
  custom_policy: preserve
  waves:
    - key: foundation
      name: Foundation
      extra: retained
      tasks:
        - key: reader
          title: Read work document
          description: |
            ## Acceptance
            Preserve Markdown and metadata.
          tags: [parser]
          custom_task: retained
        - key: child
          title: Validate references
          parent: reader
    - key: shipping
      name: Shipping
      tasks:
        - key: writer
          title: Publish export
          depends_on: [reader]
---
## Goal

One portable file.
`

func TestCLIWorkDocumentRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, profile := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, profile, "init", "--name", "Source", "--slug", "source", "--root", root)
	input := filepath.Join(root, "plan.md")
	if err := os.WriteFile(input, []byte(planDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := decodeEnvelope(t, runCLI(t, db, profile, "plan", "import", "--file", input, "--dry-run"))["data"].(map[string]any)
	if preview["task_count"] != float64(3) || preview["dry_run"] != true {
		t.Fatalf("preview: %v", preview)
	}
	created := decodeEnvelope(t, runCLI(t, db, profile, "plan", "create", "--file", input))["data"].(map[string]any)
	if created["wave_count"] != float64(2) {
		t.Fatalf("created: %v", created)
	}
	exported := filepath.Join(root, "export.md")
	runCLI(t, db, profile, "plan", "export", "file-transport", "--output", exported)
	data, err := os.ReadFile(exported)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"custom_policy: preserve", "custom_task: retained", "extra: retained", "reviewer: team", "## Acceptance", "parent: reader", "depends_on:", "- reader"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("missing %q in %s", expected, data)
		}
	}
	runCLIExpectError(t, db, profile, "validation_error", "plan", "export", "file-transport", "--output", exported)
	writeFile(t, exported, "replace this destination")
	runCLI(t, db, profile, "plan", "export", "file-transport", "--output", exported, "--force")
	readBackEquals(t, exported, string(data))
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	runCLI(t, db, profile, "init", "--name", "Target", "--slug", "target", "--root", target)
	runCLI(t, db, profile, "--project", "target", "plan", "import", "--file", exported)
	second := filepath.Join(root, "second.md")
	runCLI(t, db, profile, "--project", "target", "plan", "export", "file-transport", "--output", second)
	other, err := os.ReadFile(second)
	if err != nil || !bytes.Equal(data, other) {
		t.Fatalf("round trip changed: %v\n%s\n%s", err, data, other)
	}
}

func TestCLIWorkImportRollsBackAndTaskMarkdown(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, profile := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, profile, "init", "--name", "Example", "--slug", "example", "--root", root)
	file := filepath.Join(root, "plan.md")
	invalidDocuments := map[string]string{
		"late priority failure": strings.Replace(planDocument, "title: Publish export", "title: Publish export\n          priority: unknown-priority", 1),
		"dependency cycle":      strings.Replace(planDocument, "title: Read work document", "title: Read work document\n          depends_on: [writer]", 1),
		"unknown reference":     strings.Replace(planDocument, "depends_on: [reader]", "depends_on: [missing]", 1),
	}
	for name, invalid := range invalidDocuments {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(file, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			runCLIExpectError(t, db, profile, "validation_error", "plan", "import", "--file", file)
		})
	}
	if err := os.WriteFile(file, []byte(planDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	runCLI(t, db, profile, "plan", "import", "--file", file)
	plain := filepath.Join(root, "resume.md")
	if err := os.WriteFile(plain, []byte("## Resume\n\nFinish documentation."), 0o600); err != nil {
		t.Fatal(err)
	}
	created := decodeEnvelope(t, runCLI(t, db, profile, "task", "create", "--file", plain, "--title", "Documentation", "--confirm"))["data"].(map[string]any)["task"].(map[string]any)
	id := strconv.FormatInt(int64(created["id"].(float64)), 10)
	out := filepath.Join(root, "task.md")
	runCLI(t, db, profile, "task", "export", id, "--output", out)
	runCLI(t, db, profile, "task", "create", "--file", out, "--confirm", "--dry-run")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workfile.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func TestCLIWorkExportIncludesDescendantsAndRejectsExternalDependencies(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, profile := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, profile, "init", "--name", "Example", "--slug", "example", "--root", root)
	input := strings.ReplaceAll(planDocument, "reader", "task-1")
	cmd := NewRootCommand("test", Runners{})
	var imported bytes.Buffer
	cmd.SetOut(&imported)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs([]string{"--db", db, "--config", profile, "plan", "create", "--file", "-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stdin import: %v, %s", err, imported.String())
	}
	ids := decodeEnvelope(t, imported.String())["data"].(map[string]any)["tasks"].(map[string]any)
	parent := strconv.FormatInt(int64(ids["task-1"].(float64)), 10)
	child := decodeEnvelope(t, runCLI(t, db, profile, "task", "create", "--parent", parent, "--title", "Unassigned descendant", "--confirm"))["data"].(map[string]any)["task"].(map[string]any)
	childID := strconv.FormatInt(int64(child["id"].(float64)), 10)
	cmd = NewRootCommand("test", Runners{})
	var exported bytes.Buffer
	cmd.SetOut(&exported)
	cmd.SetArgs([]string{"--db", db, "--config", profile, "plan", "export", "file-transport"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stdout export: %v, %s", err, exported.String())
	}
	doc, err := workfile.Decode(&exported)
	if err != nil {
		t.Fatal(err)
	}
	var descendantFound bool
	for _, task := range doc.TaskList() {
		if task.Title == "Unassigned descendant" {
			descendantFound = task.Parent == "task-1" && task.Key != "task-1" && task.PlanMember != nil && !*task.PlanMember
		}
	}
	if !descendantFound {
		t.Fatalf("descendant membership lost: %+v", doc)
	}
	external := decodeEnvelope(t, runCLI(t, db, profile, "task", "create", "--title", "External prerequisite", "--confirm"))["data"].(map[string]any)["task"].(map[string]any)
	externalID := strconv.FormatInt(int64(external["id"].(float64)), 10)
	runCLI(t, db, profile, "depend", "add", childID, "--on", externalID)
	runCLIExpectError(t, db, profile, "validation_error", "plan", "export", "file-transport")
}
