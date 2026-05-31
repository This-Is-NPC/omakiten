package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// importFixture scaffolds a full valid config root (entity dirs, languages,
// themes, default profiles) and returns the root dir plus the omakase profile
// path. The profile loads cleanly before any import rewrite.
func importFixture(t *testing.T) (root, profile string) {
	t.Helper()
	root = t.TempDir()
	if err := EnsureDefaultFiles(root); err != nil {
		t.Fatalf("EnsureDefaultFiles() error = %v", err)
	}
	return root, filepath.Join(root, "config", "omakase.yaml")
}

// externalizeTopLevel rewrites profile so that the top-level YAML key `key`
// becomes `key: { from: ./<importName> }`, writing the key's original value
// node to <profileDir>/<importName>. Returns the absolute import path. The
// rewrite goes through yaml node surgery so it is robust to formatting.
func externalizeTopLevel(t *testing.T, profile, key, importName string) string {
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
	if top.Kind != yaml.MappingNode {
		t.Fatalf("profile root is not a mapping (kind=%d)", top.Kind)
	}
	var valNode *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == key {
			valNode = top.Content[i+1]
			// Replace the value with a `from:` import mapping.
			top.Content[i+1] = importDirectiveNode(importName)
			break
		}
	}
	if valNode == nil {
		t.Fatalf("profile has no top-level key %q to externalize", key)
	}

	importPath := filepath.Join(filepath.Dir(profile), importName)
	if err := os.WriteFile(importPath, mustMarshalNode(t, valNode), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", importPath, err)
	}
	if err := os.WriteFile(profile, mustMarshalNode(t, &doc), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", profile, err)
	}
	return importPath
}

// setConfigSubkey rewrites profile so that config.<key> becomes a `from:`
// import pointing at <importName>, and writes importBody to that file. Used for
// the config.hooks acceptance case. importBody is raw YAML.
func setConfigSubkey(t *testing.T, profile, key, importName, importBody string) string {
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
	directive := importDirectiveNode(importName)
	replaced := false
	for i := 0; i+1 < len(cfg.Content); i += 2 {
		if cfg.Content[i].Value == key {
			cfg.Content[i+1] = directive
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Content = append(cfg.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key},
			directive,
		)
	}

	importPath := filepath.Join(filepath.Dir(profile), importName)
	if err := os.WriteFile(importPath, []byte(importBody), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", importPath, err)
	}
	if err := os.WriteFile(profile, mustMarshalNode(t, &doc), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", profile, err)
	}
	return importPath
}

func importDirectiveNode(rel string) *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: importDirectiveKey},
			{Kind: yaml.ScalarNode, Value: "./" + rel},
		},
	}
}

func mustMarshalNode(t *testing.T, n *yaml.Node) []byte {
	t.Helper()
	out, err := yaml.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal node: %v", err)
	}
	return out
}

// TestLoadBundleAcceptsTopLevelWorkflowsImport pins acceptance #2 + #4: a
// top-level `workflows:` block supplied via `from:` resolves and decodes
// identically to an inline sequence, and the imported file is tracked in
// Bundle.SourcePaths.
func TestLoadBundleAcceptsTopLevelWorkflowsImport(t *testing.T) {
	_, profile := importFixture(t)
	importPath := externalizeTopLevel(t, profile, "workflows", "workflows.yml")

	bundle, err := LoadBundle(profile)
	if err != nil {
		t.Fatalf("LoadBundle(workflows import) error = %v", err)
	}
	if len(bundle.Workflows) == 0 {
		t.Fatal("LoadBundle(workflows import): Workflows empty, want resolved from import")
	}
	assertSourcePathContains(t, bundle.SourcePaths, importPath)
}

// TestLoadBundleAcceptsHooksImport pins acceptance #1 + #4: a `config.hooks:`
// block supplied via `from:` resolves to a valid hooks sequence and the
// imported file is tracked in SourcePaths.
func TestLoadBundleAcceptsHooksImport(t *testing.T) {
	_, profile := importFixture(t)
	importPath := setConfigSubkey(t, profile, "hooks", "hooks.yml",
		"- on: task.created\n  do: noop\n")

	bundle, err := LoadBundle(profile)
	if err != nil {
		t.Fatalf("LoadBundle(hooks import) error = %v", err)
	}
	if len(bundle.Config.Hooks) == 0 {
		t.Fatal("LoadBundle(hooks import): Config.Hooks empty, want resolved from import")
	}
	if bundle.Config.Hooks[0].On != "task.created" || bundle.Config.Hooks[0].Do != "noop" {
		t.Fatalf("LoadBundle(hooks import): hook = %+v, want {on:task.created do:noop}", bundle.Config.Hooks[0])
	}
	assertSourcePathContains(t, bundle.SourcePaths, importPath)
}

// TestLoadBundleImportedUnknownFieldFails pins acceptance #3: an unknown key
// introduced through an imported file fails under the SAME strict-decode path
// that rejects unknown inline keys.
func TestLoadBundleImportedUnknownFieldFails(t *testing.T) {
	_, profile := importFixture(t)
	setConfigSubkey(t, profile, "hooks", "hooks.yml",
		"- on: task.created\n  do: noop\n  not_a_real_hook_field: 1\n")

	_, err := LoadBundle(profile)
	if err == nil {
		t.Fatal("LoadBundle(imported unknown field) error = nil, want strict-decode rejection")
	}
	if !strings.Contains(err.Error(), "not_a_real_hook_field") {
		t.Fatalf("LoadBundle error = %q, want mention of the unknown imported field", err.Error())
	}
}

// TestLoadBundleSubtaskKitImportsTracked pins acceptance #5: when a sub-kit
// profile itself uses an import, both the sub-kit profile and its imported file
// participate in resolution and source tracking on the root bundle.
func TestLoadBundleSubtaskKitImportsTracked(t *testing.T) {
	root, profile := importFixture(t)

	subProfile := filepath.Join(root, "config", "izakaya.yaml")
	subImport := externalizeTopLevel(t, subProfile, "workflows", "sub-workflows.yml")

	appendTopLevelYAML(t, profile, "subtask_kit: izakaya.yaml\n")

	bundle, err := LoadBundle(profile)
	if err != nil {
		t.Fatalf("LoadBundle(sub-kit import) error = %v", err)
	}
	if bundle.SubtaskBundle == nil {
		t.Fatal("LoadBundle(sub-kit import): SubtaskBundle nil")
	}
	assertSourcePathContains(t, bundle.SourcePaths, subProfile)
	assertSourcePathContains(t, bundle.SourcePaths, subImport)
}

// TestLoadBundleImportFailureIsAtomic pins acceptance #6: a failed import yields
// an error and a zero Bundle — no partial bundle escapes. The cache/runtime swap
// downstream consumes the returned Bundle, so an error here guarantees no
// partial snapshot/runtime swap.
func TestLoadBundleImportFailureIsAtomic(t *testing.T) {
	_, profile := importFixture(t)
	setConfigSubkey(t, profile, "hooks", "missing.yml", "") // body unused; we delete it
	if err := os.Remove(filepath.Join(filepath.Dir(profile), "missing.yml")); err != nil {
		t.Fatalf("Remove(missing.yml): %v", err)
	}

	bundle, err := LoadBundle(profile)
	if err == nil {
		t.Fatal("LoadBundle(missing import) error = nil, want failure")
	}
	if len(bundle.SourcePaths) != 0 || bundle.Kit.Key != "" {
		t.Fatalf("LoadBundle(missing import) returned non-zero bundle = %+v, want zero (atomic)", bundle)
	}
}

func assertSourcePathContains(t *testing.T, paths []string, want string) {
	t.Helper()
	wantCanon := want
	if real, err := filepath.EvalSymlinks(want); err == nil {
		wantCanon = real
	}
	for _, p := range paths {
		if p == want || p == wantCanon {
			return
		}
		if real, err := filepath.EvalSymlinks(p); err == nil && real == wantCanon {
			return
		}
	}
	t.Fatalf("SourcePaths %v does not contain %q (canonical %q)", paths, want, wantCanon)
}
