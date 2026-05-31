package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeImportYAML writes body to dir/name (creating parents) and returns the
// absolute path.
func writeImportYAML(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

// parseImportRoot parses body into a document's root content node, mirroring how
// the loader hands a decoded tree to the resolver.
func parseImportRoot(t *testing.T, body string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		t.Fatalf("parsed node is not a non-empty document: kind=%d", doc.Kind)
	}
	return doc.Content[0]
}

// runResolve writes rootBody to dir/root.yml, parses it, resolves imports, and
// returns the resolved node, sources, and error. The canonical root path
// (symlink-resolved) is returned for source assertions.
func runResolve(t *testing.T, dir, rootBody string) (*yaml.Node, []string, string, error) {
	t.Helper()
	rootPath := writeImportYAML(t, dir, "root.yml", rootBody)
	node, sources, err := resolveImports(parseImportRoot(t, rootBody), rootPath)
	return node, sources, canonical(t, rootPath), err
}

// canonical returns the symlink-resolved absolute path, matching what the
// resolver records in its sources list.
func canonical(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs(%s): %v", path, err)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func TestResolveImportsReplacesWithScalarRoot(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "scalar.yml", "hello\n")
	node, sources, rootPath, err := runResolve(t, dir, "value:\n  from: ./scalar.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	value := mapVal(t, node, "value")
	if value.Kind != yaml.ScalarNode || value.Value != "hello" {
		t.Fatalf("value node = %+v, want scalar 'hello'", value)
	}
	wantSrc(t, sources, rootPath, canonical(t, filepath.Join(dir, "scalar.yml")))
}

func TestResolveImportsReplacesWithSequenceRoot(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "seq.yml", "- a\n- b\n- c\n")
	node, sources, rootPath, err := runResolve(t, dir, "items:\n  from: ./seq.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	items := mapVal(t, node, "items")
	if items.Kind != yaml.SequenceNode || len(items.Content) != 3 {
		t.Fatalf("items node = %+v, want 3-element sequence", items)
	}
	wantSrc(t, sources, rootPath, canonical(t, filepath.Join(dir, "seq.yml")))
}

func TestResolveImportsReplacesWithMappingRoot(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "hooks.yml", "on_move:\n  - run: echo hi\n")
	node, sources, rootPath, err := runResolve(t, dir, "config:\n  hooks:\n    from: ./hooks.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	hooks := mapVal(t, mapVal(t, node, "config"), "hooks")
	if hooks.Kind != yaml.MappingNode {
		t.Fatalf("hooks node kind = %d, want mapping", hooks.Kind)
	}
	if got := mapVal(t, hooks, "on_move"); got.Kind != yaml.SequenceNode {
		t.Fatalf("hooks.on_move kind = %d, want sequence", got.Kind)
	}
	wantSrc(t, sources, rootPath, canonical(t, filepath.Join(dir, "hooks.yml")))
}

func TestResolveImportsTopLevelDirective(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "workflows.yml", "- key: omakase\n  name: Omakase\n")
	node, sources, rootPath, err := runResolve(t, dir, "from: ./workflows.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) != 1 {
		t.Fatalf("root node = %+v, want 1-element sequence", node)
	}
	wantSrc(t, sources, rootPath, canonical(t, filepath.Join(dir, "workflows.yml")))
}

func TestResolveImportsNestedDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "leaf.yml", "deep: true\n")
	writeImportYAML(t, dir, "mid.yml", "inner:\n  from: ./leaf.yml\n")
	node, sources, rootPath, err := runResolve(t, dir, "outer:\n  from: ./mid.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	deep := mapVal(t, mapVal(t, mapVal(t, node, "outer"), "inner"), "deep")
	if deep.Value != "true" {
		t.Fatalf("outer.inner.deep = %q, want true", deep.Value)
	}
	wantSrc(t, sources, rootPath,
		canonical(t, filepath.Join(dir, "mid.yml")),
		canonical(t, filepath.Join(dir, "leaf.yml")),
	)
}

func TestResolveImportsRelativeToDeclaringFile(t *testing.T) {
	dir := t.TempDir()
	// mid.yml lives in sub/ and imports a sibling in sub/, so the path must
	// resolve relative to sub/, not the root directory.
	writeImportYAML(t, dir, "sub/leaf.yml", "ok: yes\n")
	writeImportYAML(t, dir, "sub/mid.yml", "v:\n  from: ./leaf.yml\n")
	node, sources, rootPath, err := runResolve(t, dir, "wrap:\n  from: ./sub/mid.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	if mapVal(t, mapVal(t, mapVal(t, node, "wrap"), "v"), "ok").Value != "yes" {
		t.Fatalf("wrap.v.ok != yes")
	}
	wantSrc(t, sources, rootPath,
		canonical(t, filepath.Join(dir, "sub", "mid.yml")),
		canonical(t, filepath.Join(dir, "sub", "leaf.yml")),
	)
}

func TestResolveImportsSharedImportListedOnce(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "shared.yml", "x: 1\n")
	// shared.yml is imported twice in two distinct branches (not a cycle); it
	// must appear exactly once in stable first-encounter order.
	_, sources, rootPath, err := runResolve(t, dir, "a:\n  from: ./shared.yml\nb:\n  from: ./shared.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	wantSrc(t, sources, rootPath, canonical(t, filepath.Join(dir, "shared.yml")))
}

func TestResolveImportsRejectsCycle(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "a.yml", "loop:\n  from: ./b.yml\n")
	writeImportYAML(t, dir, "b.yml", "loop:\n  from: ./a.yml\n")
	_, _, _, err := runResolve(t, dir, "start:\n  from: ./a.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want cycle rejection")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %q, want cycle", err.Error())
	}
	if !strings.Contains(err.Error(), "a.yml") {
		t.Fatalf("error = %q, want chain context naming a.yml", err.Error())
	}
}

func TestResolveImportsRejectsSelfCycle(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "self.yml", "me:\n  from: ./self.yml\n")
	_, _, _, err := runResolve(t, dir, "start:\n  from: ./self.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want self-cycle rejection")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %q, want cycle", err.Error())
	}
}

func TestResolveImportsRejectsDepthOverflow(t *testing.T) {
	dir := t.TempDir()
	// Linear chain longer than maxImportDepth: f0 -> f1 -> ... last is a scalar.
	const chain = maxImportDepth + 2
	for i := 0; i < chain; i++ {
		name := "f" + strconv.Itoa(i) + ".yml"
		if i == chain-1 {
			writeImportYAML(t, dir, name, "leaf\n")
			continue
		}
		writeImportYAML(t, dir, name, "next:\n  from: ./f"+strconv.Itoa(i+1)+".yml\n")
	}
	_, _, _, err := runResolve(t, dir, "head:\n  from: ./f0.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want depth-overflow rejection")
	}
	if !strings.Contains(err.Error(), "depth") {
		t.Fatalf("error = %q, want depth", err.Error())
	}
}

// TestResolveImportsMappingWithSiblingsPassesThrough pins the contract: a
// mapping carrying `from` alongside other keys is NOT an import directive — it
// is an ordinary mapping (canonically a workflow transition `{from: x, to: y}`)
// and must pass through untouched, never read the path, never error.
func TestResolveImportsMappingWithSiblingsPassesThrough(t *testing.T) {
	dir := t.TempDir()
	// ./x.yml is intentionally NOT created — a passthrough mapping must never
	// attempt to read the `from` value as a path.
	node, sources, rootPath, err := runResolve(t, dir, "node:\n  from: ./x.yml\n  extra: nope\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v, want passthrough (no import)", err)
	}
	inner := mapVal(t, node, "node")
	if inner.Kind != yaml.MappingNode {
		t.Fatalf("node.node kind = %d, want mapping (untouched)", inner.Kind)
	}
	if got := mapVal(t, inner, "from"); got.Value != "./x.yml" {
		t.Fatalf("node.node.from = %q, want ./x.yml (untouched)", got.Value)
	}
	if got := mapVal(t, inner, "extra"); got.Value != "nope" {
		t.Fatalf("node.node.extra = %q, want nope (untouched)", got.Value)
	}
	wantSrc(t, sources, rootPath)
}

// TestResolveImportsTransitionPassesThrough pins the concrete
// workflow-transition shape that motivated the passthrough rule.
func TestResolveImportsTransitionPassesThrough(t *testing.T) {
	dir := t.TempDir()
	node, sources, rootPath, err := runResolve(t, dir, "transitions:\n  - from: dev\n    to: review\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v, want transition passthrough", err)
	}
	tr := mapVal(t, node, "transitions")
	if tr.Kind != yaml.SequenceNode || len(tr.Content) != 1 {
		t.Fatalf("transitions = %+v, want 1-element sequence", tr)
	}
	if got := mapVal(t, tr.Content[0], "from"); got.Value != "dev" {
		t.Fatalf("transition.from = %q, want dev (untouched)", got.Value)
	}
	wantSrc(t, sources, rootPath)
}

func TestResolveImportsRejectsBadDirectives(t *testing.T) {
	cases := map[string]struct {
		body    string
		wantErr string
	}{
		"missing file":     {body: "v:\n  from: ./nope.yml\n", wantErr: "nope.yml"},
		"absolute path":    {body: "v:\n  from: " + filepath.Join(string(os.PathSeparator), "etc", "x.yml") + "\n", wantErr: "must be relative"},
		"parent escape":    {body: "v:\n  from: ../x.yml\n", wantErr: "parent directory"},
		"non-scalar value": {body: "v:\n  from:\n    - nested\n", wantErr: "from"},
		"empty value":      {body: "v:\n  from: \"\"\n", wantErr: "empty"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			_, _, _, err := runResolve(t, dir, tc.body)
			if err == nil {
				t.Fatalf("resolveImports() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestResolveImportsRejectsInvalidImportedYAML(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "broken.yml", "key: [unterminated\n")
	_, _, _, err := runResolve(t, dir, "v:\n  from: ./broken.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want invalid-YAML rejection")
	}
	if !strings.Contains(err.Error(), "broken.yml") {
		t.Fatalf("error = %q, want chain context naming broken.yml", err.Error())
	}
}

func TestResolveImportsRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Target lives outside configDir but inside dir so EvalSymlinks resolves.
	// Lexical guards pass (no '..', not absolute) — only the canonical-path
	// comparison detects the escape.
	outside := writeImportYAML(t, dir, "outside.yml", "secret: 1\n")
	link := filepath.Join(configDir, "escape.yml")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	rootBody := "v:\n  from: ./escape.yml\n"
	rootPath := writeImportYAML(t, configDir, "root.yml", rootBody)
	_, _, err := resolveImports(parseImportRoot(t, rootBody), rootPath)
	if err == nil {
		t.Fatal("resolveImports() error = nil, want symlink-escape rejection")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Fatalf("error = %q, want escape rejection", err.Error())
	}
}

func TestResolveImportsRejectsOversizedImport(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("k: vvvvvvvvvv\n", int(importFileMaxBytes/12)+1024)
	writeImportYAML(t, dir, "big.yml", big)
	_, _, _, err := runResolve(t, dir, "v:\n  from: ./big.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want size-cap rejection")
	}
	if !IsConfigTooLarge(err) {
		t.Fatalf("error = %q, want ErrConfigTooLarge", err.Error())
	}
}

func TestResolveImportsNoDirectivePassThrough(t *testing.T) {
	dir := t.TempDir()
	node, sources, rootPath, err := runResolve(t, dir, "a: 1\nb:\n  c: 2\nlist:\n  - x\n  - y\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	if mapVal(t, node, "a").Value != "1" {
		t.Fatalf("a != 1 after pass-through")
	}
	wantSrc(t, sources, rootPath)
}

// --- merge_from: tests ---

func TestMergeFromImportsBaseKeys(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "base.yml", "output:\n  minified: true\ncontext:\n  level: 2\n")
	node, _, _, err := runResolve(t, dir, "config:\n  merge_from: ./base.yml\n  workflow:\n    active: omakase\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	cfg := mapVal(t, node, "config")
	// base key not in override should be present
	if mapVal(t, cfg, "output").Kind != yaml.MappingNode {
		t.Fatal("config.output missing after merge_from")
	}
	if mapVal(t, cfg, "context").Kind != yaml.MappingNode {
		t.Fatal("config.context missing after merge_from")
	}
	// own sibling key must be present
	if mapVal(t, mapVal(t, cfg, "workflow"), "active").Value != "omakase" {
		t.Fatal("config.workflow.active lost after merge_from")
	}
}

func TestMergeFromSiblingOverridesImported(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "base.yml", "theme:\n  active: default\noutput:\n  minified: false\n")
	node, _, _, err := runResolve(t, dir, "config:\n  merge_from: ./base.yml\n  theme:\n    active: custom\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	cfg := mapVal(t, node, "config")
	// sibling key wins over imported value
	if got := mapVal(t, mapVal(t, cfg, "theme"), "active").Value; got != "custom" {
		t.Fatalf("config.theme.active = %q, want custom (sibling override)", got)
	}
	// non-conflicting key from base still present
	if mapVal(t, cfg, "output").Kind != yaml.MappingNode {
		t.Fatal("config.output missing — base key should survive sibling override")
	}
}

func TestMergeFromDeepMergesNestedMapping(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "base.yml", "events:\n  limit: 50\n  defaults:\n    log: true\n")
	// override only adds `overrides:` to events; the other base keys must survive
	node, _, _, err := runResolve(t, dir, "config:\n  merge_from: ./base.yml\n  events:\n    overrides:\n      tag.added:\n        log: false\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	events := mapVal(t, mapVal(t, node, "config"), "events")
	if mapVal(t, events, "limit").Value != "50" {
		t.Fatal("events.limit lost after deep merge")
	}
	if mapVal(t, events, "defaults").Kind != yaml.MappingNode {
		t.Fatal("events.defaults lost after deep merge")
	}
	if mapVal(t, events, "overrides").Kind != yaml.MappingNode {
		t.Fatal("events.overrides missing — should come from override sibling")
	}
}

func TestMergeFromReplacesListNotAppends(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "base.yml", "hooks:\n  - on: task.created\n  - on: task.moved\n")
	node, _, _, err := runResolve(t, dir, "config:\n  merge_from: ./base.yml\n  hooks:\n    - on: task.removed\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	hooks := mapVal(t, mapVal(t, node, "config"), "hooks")
	if hooks.Kind != yaml.SequenceNode || len(hooks.Content) != 1 {
		t.Fatalf("hooks = %+v (len %d), want 1-element sequence (sibling replaces, not appends)", hooks, len(hooks.Content))
	}
}

func TestMergeFromRejectsNonMappingImport(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "seq.yml", "- a\n- b\n")
	_, _, _, err := runResolve(t, dir, "config:\n  merge_from: ./seq.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want rejection for non-mapping import")
	}
	if !strings.Contains(err.Error(), "mapping") {
		t.Fatalf("error = %q, want 'mapping' in message", err.Error())
	}
}

func TestMergeFromRecordedInSources(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "base.yml", "x: 1\n")
	_, sources, rootPath, err := runResolve(t, dir, "config:\n  merge_from: ./base.yml\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	wantSrc(t, sources, rootPath, canonical(t, filepath.Join(dir, "base.yml")))
}

func TestMergeFromCycleRejected(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "a.yml", "x:\n  merge_from: ./b.yml\n")
	writeImportYAML(t, dir, "b.yml", "y:\n  merge_from: ./a.yml\n")
	_, _, _, err := runResolve(t, dir, "root:\n  merge_from: ./a.yml\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want cycle rejection")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %q, want cycle", err.Error())
	}
}

// --- fragment selector tests ---

func TestFragmentSelectorSelectsSubtree(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "theme.yml", "personas:\n  - slug: foo\ncommands:\n  global:\n    laws: []\n")
	node, _, _, err := runResolve(t, dir, "personas:\n  from: ./theme.yml#personas\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	personas := mapVal(t, node, "personas")
	if personas.Kind != yaml.SequenceNode || len(personas.Content) != 1 {
		t.Fatalf("personas = %+v, want 1-element sequence", personas)
	}
}

func TestFragmentSelectorNotFound(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "theme.yml", "personas:\n  - slug: foo\n")
	_, _, _, err := runResolve(t, dir, "x:\n  from: ./theme.yml#missing\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want fragment-not-found rejection")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %q, want fragment name in message", err.Error())
	}
}

func TestFragmentSelectorRejectsNonMappingRoot(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "seq.yml", "- a\n- b\n")
	_, _, _, err := runResolve(t, dir, "x:\n  from: ./seq.yml#key\n")
	if err == nil {
		t.Fatal("resolveImports() error = nil, want rejection for non-mapping root")
	}
	if !strings.Contains(err.Error(), "not a mapping") {
		t.Fatalf("error = %q, want 'not a mapping'", err.Error())
	}
}

func TestFragmentWithMergeFrom(t *testing.T) {
	dir := t.TempDir()
	writeImportYAML(t, dir, "theme.yml", "base:\n  x: 1\n  y: 2\n")
	node, _, _, err := runResolve(t, dir, "config:\n  merge_from: ./theme.yml#base\n  z: 3\n")
	if err != nil {
		t.Fatalf("resolveImports() error = %v", err)
	}
	cfg := mapVal(t, node, "config")
	if mapVal(t, cfg, "x").Value != "1" {
		t.Fatal("config.x missing after merge_from with fragment")
	}
	if mapVal(t, cfg, "y").Value != "2" {
		t.Fatal("config.y missing after merge_from with fragment")
	}
	if mapVal(t, cfg, "z").Value != "3" {
		t.Fatal("config.z (sibling) missing")
	}
}

// mapVal returns the value node for key in a mapping node.
func mapVal(t *testing.T, m *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if m.Kind != yaml.MappingNode {
		t.Fatalf("node kind = %d, want mapping while looking up %q", m.Kind, key)
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	t.Fatalf("key %q not found in mapping", key)
	return nil
}

// wantSrc asserts sources equals want exactly, in order.
func wantSrc(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sources = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sources[%d] = %q, want %q (full got=%v)", i, got[i], want[i], got)
		}
	}
}
