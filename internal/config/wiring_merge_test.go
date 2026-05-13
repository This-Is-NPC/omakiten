package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// asMap is a test helper: parses a YAML literal into map[string]any so
// merge tests can assert against concrete keys without re-marshalling.
func asMap(t *testing.T, src string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := yaml.Unmarshal([]byte(src), &out); err != nil {
		t.Fatalf("unmarshal helper: %v", err)
	}
	return out
}

func TestMergeWiringMaps_WorkflowsReplaceByKey(t *testing.T) {
	base := asMap(t, `
workflows:
  - { id: 1, key: omakase, name: Omakase }
  - { id: 2, key: izakaya, name: Izakaya }
`)
	overlay := asMap(t, `
workflows:
  - { id: 1, key: omakase, name: Omakase Custom }
`)
	merged := mergeWiringMaps(base, overlay)

	list := merged["workflows"].([]any)
	if len(list) != 2 {
		t.Fatalf("workflows len = %d, want 2", len(list))
	}
	first := list[0].(map[string]any)
	if first["name"] != "Omakase Custom" {
		t.Fatalf("first.name = %v, want Omakase Custom", first["name"])
	}
	second := list[1].(map[string]any)
	if second["key"] != "izakaya" {
		t.Fatalf("second.key = %v, want izakaya", second["key"])
	}
}

func TestMergeWiringMaps_WorkflowsAddNewKey(t *testing.T) {
	base := asMap(t, `workflows: [{id: 1, key: omakase, name: Omakase}]`)
	overlay := asMap(t, `workflows: [{id: 9, key: mine, name: Mine}]`)
	merged := mergeWiringMaps(base, overlay)

	list := merged["workflows"].([]any)
	if len(list) != 2 {
		t.Fatalf("workflows len = %d, want 2", len(list))
	}
	if list[1].(map[string]any)["key"] != "mine" {
		t.Fatalf("appended workflow key = %v, want mine", list[1])
	}
}

func TestMergeWiringMaps_PersonasReplaceBySlug(t *testing.T) {
	base := asMap(t, `
personas:
  - { slug: engineer, skills: [go], laws: [scope] }
  - { slug: reviewer, skills: [review] }
`)
	overlay := asMap(t, `
personas:
  - { slug: engineer, skills: [rust], laws: [scope, conventional-commits] }
`)
	merged := mergeWiringMaps(base, overlay)
	list := merged["personas"].([]any)
	if len(list) != 2 {
		t.Fatalf("personas len = %d, want 2", len(list))
	}
	engineer := list[0].(map[string]any)
	if skills := engineer["skills"].([]any); skills[0] != "rust" {
		t.Fatalf("engineer.skills[0] = %v, want rust (overlay replaces entire entry)", skills[0])
	}
}

func TestMergeWiringMaps_MCPCommandsReplaceByMapKey(t *testing.T) {
	base := asMap(t, `
mcp_commands:
  global:    { laws: [scope] }
  okt-build: { persona: engineer, laws: [tests-passing] }
`)
	overlay := asMap(t, `
mcp_commands:
  okt-build: { persona: shipper, laws: [docs] }
`)
	merged := mergeWiringMaps(base, overlay)
	cmds := merged["mcp_commands"].(map[string]any)
	if len(cmds) != 2 {
		t.Fatalf("mcp_commands len = %d, want 2", len(cmds))
	}
	build := cmds["okt-build"].(map[string]any)
	if build["persona"] != "shipper" {
		t.Fatalf("okt-build.persona = %v, want shipper (overlay wholesale-replaces map value)", build["persona"])
	}
	if global := cmds["global"].(map[string]any); global["laws"].([]any)[0] != "scope" {
		t.Fatalf("global preserved wrong: %+v", global)
	}
}

func TestMergeWiringMaps_ConfigDeepMerges(t *testing.T) {
	base := asMap(t, `
config:
  output:
    json_minified: true
    omit_empty: false
  context:
    default_level: 2
`)
	overlay := asMap(t, `
config:
  output:
    omit_empty: true
  mcp:
    recent_comment_limit: 10
`)
	merged := mergeWiringMaps(base, overlay)
	cfg := merged["config"].(map[string]any)
	output := cfg["output"].(map[string]any)
	if output["json_minified"] != true {
		t.Fatalf("json_minified preserved wrong: %v", output["json_minified"])
	}
	if output["omit_empty"] != true {
		t.Fatalf("omit_empty overlay wrong: %v", output["omit_empty"])
	}
	context := cfg["context"].(map[string]any)
	if context["default_level"] != 2 {
		t.Fatalf("context preserved wrong: %v", context)
	}
	mcp := cfg["mcp"].(map[string]any)
	if mcp["recent_comment_limit"] != 10 {
		t.Fatalf("mcp overlay-only wrong: %v", mcp)
	}
}

func TestMergeWiringMaps_SkillsLawsTemplatesUnion(t *testing.T) {
	base := asMap(t, `
skills: [go, sqlite]
laws: [scope]
templates: [user-story]
`)
	overlay := asMap(t, `
skills: [rust, go]
laws: [tests-passing]
templates: [pull-request]
`)
	merged := mergeWiringMaps(base, overlay)
	if got := merged["skills"].([]any); len(got) != 3 {
		t.Fatalf("skills union len = %d, want 3 (go, sqlite, rust)", len(got))
	}
	if got := merged["laws"].([]any); len(got) != 2 {
		t.Fatalf("laws union len = %d, want 2", len(got))
	}
}

func TestMergeWiringMaps_DisabledLists(t *testing.T) {
	base := asMap(t, `
workflows: [{id: 1, key: omakase}, {id: 2, key: izakaya}]
personas: [{slug: engineer}, {slug: reviewer}]
projects: [{slug: foo, name: Foo}, {slug: bar, name: Bar}]
mcp_commands:
  okt-build: {}
  okt-test: {}
skills: [go, rust]
`)
	overlay := asMap(t, `
workflows_disabled: [izakaya]
personas_disabled: [reviewer]
projects_disabled: [bar]
mcp_commands_disabled: [okt-test]
skills_disabled: [rust]
`)
	merged := mergeWiringMaps(base, overlay)

	if list := merged["workflows"].([]any); len(list) != 1 || list[0].(map[string]any)["key"] != "omakase" {
		t.Fatalf("workflows_disabled failed: %+v", list)
	}
	if list := merged["personas"].([]any); len(list) != 1 || list[0].(map[string]any)["slug"] != "engineer" {
		t.Fatalf("personas_disabled failed: %+v", list)
	}
	if list := merged["projects"].([]any); len(list) != 1 || list[0].(map[string]any)["slug"] != "foo" {
		t.Fatalf("projects_disabled failed: %+v", list)
	}
	if cmds := merged["mcp_commands"].(map[string]any); len(cmds) != 1 {
		t.Fatalf("mcp_commands_disabled failed: %+v", cmds)
	}
	if list := merged["skills"].([]any); len(list) != 1 || list[0] != "go" {
		t.Fatalf("skills_disabled failed: %+v", list)
	}
	// Disabled keys must be stripped from the output so a strict decode
	// of the merged YAML does not treat them as unknown wiring fields.
	for k := range merged {
		if strings.HasSuffix(k, "_disabled") {
			t.Fatalf("merged result leaked disabled key: %s", k)
		}
	}
}

func TestMergeWiringMaps_OverlayOnlySectionAdds(t *testing.T) {
	base := asMap(t, `workflows: [{key: omakase}]`)
	overlay := asMap(t, `personas: [{slug: agent}]`)
	merged := mergeWiringMaps(base, overlay)
	if _, ok := merged["personas"]; !ok {
		t.Fatalf("overlay-only section not added")
	}
	if _, ok := merged["workflows"]; !ok {
		t.Fatalf("base-only section dropped")
	}
}

func TestMergeWiringMaps_ScalarOverlayReplaces(t *testing.T) {
	base := asMap(t, `version: 1`)
	overlay := asMap(t, `version: 2`)
	merged := mergeWiringMaps(base, overlay)
	if merged["version"] != 2 {
		t.Fatalf("version = %v, want 2", merged["version"])
	}
}

func TestMergeWiringYAML_RoundTripStrictDecodes(t *testing.T) {
	base := []byte(`
version: 1
kit: {id: 1, key: omakase, name: Omakase}
config:
  output: {json_minified: true, omit_empty: true}
  context: {default_level: 2, max_tokens: 12000}
  workflow: {active: omakase}
  theme: {active: catppuccin}
  template_defaults: [user-story]
  priorities:
    - {id: 1, value: low}
    - {id: 2, value: normal, default: true}
    - {id: 3, value: high}
  severities:
    - {id: 1, value: info}
    - {id: 2, value: warning, default: true}
    - {id: 3, value: error}
workflows:
  - id: 1
    key: omakase
    name: Omakase
    buckets: [{id: 1, key: backlog, name: Backlog, position: 1}]
    transitions: []
`)
	overlay := []byte(`
workflows_disabled: [doesnotexist]
personas: [{slug: agent}]
`)
	merged, err := mergeWiringYAML(base, overlay)
	if err != nil {
		t.Fatalf("mergeWiringYAML: %v", err)
	}
	w, err := decodeWiring(merged)
	if err != nil {
		t.Fatalf("decodeWiring: %v\n--- yaml ---\n%s", err, merged)
	}
	if len(w.Personas) != 1 || w.Personas[0].Slug != "agent" {
		t.Fatalf("personas after merge: %+v", w.Personas)
	}
	if w.Version != 1 {
		t.Fatalf("version: %d", w.Version)
	}
}

func TestReadWiringWithRepoLocal_NoOverlayWhenDirEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/omakiten.yaml", `version: 1
kit: {id: 1, key: omakase, name: Omakase}
`)
	w, err := readWiringWithRepoLocal(dir+"/omakiten.yaml", "")
	if err != nil {
		t.Fatalf("readWiringWithRepoLocal: %v", err)
	}
	if w.Version != 1 {
		t.Fatalf("version = %d", w.Version)
	}
}

func TestReadWiringWithRepoLocal_MissingOverlayFileOK(t *testing.T) {
	dir := t.TempDir()
	repoLocal := t.TempDir()
	writeFile(t, dir+"/omakiten.yaml", `version: 1
kit: {id: 1, key: omakase, name: Omakase}
`)
	w, err := readWiringWithRepoLocal(dir+"/omakiten.yaml", repoLocal)
	if err != nil {
		t.Fatalf("readWiringWithRepoLocal: %v", err)
	}
	if w.Kit.Key != "omakase" {
		t.Fatalf("kit.key = %q", w.Kit.Key)
	}
}
