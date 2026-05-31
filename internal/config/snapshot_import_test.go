package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// snapshotFromImportFixture scaffolds a fresh default config root, applies
// mutate to its omakase profile, then loads + builds a snapshot. mutate is the
// per-test rewrite (e.g. externalizing one section through a `from:` import);
// pass a no-op to obtain the inline baseline. The returned snapshot is what the
// app actually consumes — no call site is import-aware, so equality between the
// imported and inline snapshots proves the resolver feeds the same resolved
// config through the existing BuildSnapshot path.
func snapshotFromImportFixture(t *testing.T, mutate func(t *testing.T, profile string)) *Snapshot {
	t.Helper()
	_, profile := importFixture(t)
	if mutate != nil {
		mutate(t, profile)
	}
	bundle, err := LoadBundle(profile)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	return BuildSnapshot(bundle)
}

// TestSnapshotImportedHooksMatchInline pins acceptance #1: hooks supplied to
// config.hooks via a `from:` import surface through Snapshot.Hooks() exactly as
// if they had been written inline. The app reads hooks only through the
// snapshot accessor, so snapshot-level equality is the consumer-facing proof.
func TestSnapshotImportedHooksMatchInline(t *testing.T) {
	const hooksBody = "- on: task.created\n  do: noop\n- on: task.completed\n  do: noop\n"

	inline := snapshotFromImportFixture(t, func(t *testing.T, profile string) {
		// Write the same hooks inline by reusing setConfigSubkey's file then
		// inlining it: setConfigSubkey both writes the import file and points
		// config.hooks at it. For the inline baseline we instead splice the
		// literal sequence directly into config.hooks.
		inlineConfigHooks(t, profile, hooksBody)
	})
	imported := snapshotFromImportFixture(t, func(t *testing.T, profile string) {
		setConfigSubkey(t, profile, "hooks", "hooks.yml", hooksBody)
	})

	if got := imported.Hooks(); len(got) == 0 {
		t.Fatal("imported Snapshot.Hooks() empty, want resolved from import")
	}
	if !reflect.DeepEqual(imported.Hooks(), inline.Hooks()) {
		t.Fatalf("Snapshot.Hooks() imported = %+v, inline = %+v; want identical", imported.Hooks(), inline.Hooks())
	}
}

// TestSnapshotImportedWorkflowsMatchInline pins acceptance #2: a top-level
// workflows block supplied via `from:` builds the same active workflow snapshot
// (buckets + transitions) as the inline profile. Workflow() and Transitions()
// are the accessors the move/guard engine reads, so equality there is the
// behavioural guarantee.
func TestSnapshotImportedWorkflowsMatchInline(t *testing.T) {
	inline := snapshotFromImportFixture(t, nil) // default profile: workflows inline
	imported := snapshotFromImportFixture(t, func(t *testing.T, profile string) {
		externalizeTopLevel(t, profile, "workflows", "workflows.yml")
	})

	if got := imported.Workflow(); len(got.Buckets) == 0 {
		t.Fatal("imported Snapshot.Workflow() has no buckets, want resolved from import")
	}
	if !reflect.DeepEqual(imported.Workflow(), inline.Workflow()) {
		t.Fatalf("Snapshot.Workflow() imported = %+v, inline = %+v; want identical", imported.Workflow(), inline.Workflow())
	}
	if !reflect.DeepEqual(imported.Transitions(), inline.Transitions()) {
		t.Fatalf("Snapshot.Transitions() imported = %+v, inline = %+v; want identical", imported.Transitions(), inline.Transitions())
	}
}

// TestSnapshotImportedMCPCommandsMatchInline pins acceptance #3 and proves the
// resolver is generic, NOT hardcoded to hooks/workflows: the mcp_commands
// section — a third, structurally different profile-level value (a map, not a
// sequence) — imports successfully and surfaces through Snapshot.MCPCommands()
// identically to the inline profile.
func TestSnapshotImportedMCPCommandsMatchInline(t *testing.T) {
	inline := snapshotFromImportFixture(t, nil) // default profile: mcp_commands inline
	imported := snapshotFromImportFixture(t, func(t *testing.T, profile string) {
		externalizeTopLevel(t, profile, "mcp_commands", "mcp-commands.yml")
	})

	if got := imported.MCPCommands(); len(got) == 0 {
		t.Fatal("imported Snapshot.MCPCommands() empty, want resolved from import")
	}
	if !reflect.DeepEqual(imported.MCPCommands(), inline.MCPCommands()) {
		t.Fatalf("Snapshot.MCPCommands() imported = %+v, inline = %+v; want identical", imported.MCPCommands(), inline.MCPCommands())
	}
}

// TestLoadBundleRejectsFromWithSiblings pins acceptance #4: a `from:` paired
// with sibling keys is NOT treated as an import directive (the resolver passes
// it through verbatim, reserving that shape for domain mappings such as a
// workflow transition). When the surrounding section cannot hold such a mapping
// — here config.hooks, which expects a sequence — the un-expanded mapping
// reaches strict decode and is rejected with a clear error. This is distinct
// from the node-level passthrough test: it proves the rejection actually
// surfaces to the operator through the normal load path rather than silently
// importing.
func TestLoadBundleRejectsFromWithSiblings(t *testing.T) {
	_, profile := importFixture(t)
	// config.hooks expects a sequence; a mapping carrying `from` + a sibling is
	// passed through (not an import) and then fails the strict sequence decode.
	inlineConfigHooks(t, profile, "from: ./hooks.yml\nextra: nope\n")

	_, err := LoadBundle(profile)
	if err == nil {
		t.Fatal("LoadBundle(from + siblings) error = nil, want rejection through strict decode")
	}
	// The error must come from the decode/validation path, not a phantom import
	// read of ./hooks.yml (which is never created).
	if strings.Contains(err.Error(), "read import") {
		t.Fatalf("LoadBundle error = %q; a from+siblings mapping must not be read as an import", err.Error())
	}
}

// TestLoadBundleRejectsWrongImportedRootType pins acceptance #5: when an
// imported root has the wrong shape for its destination section, the failure is
// caught by the existing strict decode AFTER expansion — the resolver does not
// shield a malformed section. Here `workflows` expects a list, but the imported
// file's root is a mapping.
func TestLoadBundleRejectsWrongImportedRootType(t *testing.T) {
	_, profile := importFixture(t)
	// Point top-level `workflows` at a mapping-rooted file. externalizeTopLevel
	// writes the original (list) value out first; overwrite that file with a
	// mapping so expansion yields the wrong type for the strict decode.
	importPath := externalizeTopLevel(t, profile, "workflows", "workflows.yml")
	if err := os.WriteFile(importPath, []byte("not_a_list: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", importPath, err)
	}

	_, err := LoadBundle(profile)
	if err == nil {
		t.Fatal("LoadBundle(wrong imported root type) error = nil, want strict-decode rejection")
	}
	// A type mismatch under KnownFields decode names the offending key/shape;
	// assert it is the decode path, not an import-resolution error.
	if strings.Contains(err.Error(), "import chain") {
		t.Fatalf("LoadBundle error = %q; type mismatch must surface through decode, not the resolver", err.Error())
	}
}

// inlineConfigHooks splices body (raw YAML for a config.hooks value) directly
// into config.hooks as an inline value — no import directive. It mirrors
// setConfigSubkey's config-descent but writes the literal parsed node instead
// of a `from:` pointer, so a test can compare an inline section against the
// imported equivalent.
func inlineConfigHooks(t *testing.T, profile, body string) {
	t.Helper()
	raw, err := os.ReadFile(profile)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", profile, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal(%s): %v", profile, err)
	}
	top := doc.Content[0]
	var cfg *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "config" {
			cfg = top.Content[i+1]
			break
		}
	}
	if cfg == nil || cfg.Kind != yaml.MappingNode {
		t.Fatal("profile has no config mapping")
	}

	var valDoc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &valDoc); err != nil {
		t.Fatalf("Unmarshal(body): %v", err)
	}
	if len(valDoc.Content) == 0 {
		t.Fatal("inline body parsed to an empty document")
	}
	valNode := valDoc.Content[0]

	replaced := false
	for i := 0; i+1 < len(cfg.Content); i += 2 {
		if cfg.Content[i].Value == "hooks" {
			cfg.Content[i+1] = valNode
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Content = append(cfg.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "hooks"},
			valNode,
		)
	}
	if err := os.WriteFile(profile, mustMarshalNode(t, &doc), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", profile, err)
	}
}
