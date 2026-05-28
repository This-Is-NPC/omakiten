package config

import (
	"testing"
)

// TestComputeSettingsSources_DefaultBaselineAllDefault pins the canonical
// install case: a user file that exactly mirrors the kit baseline labels
// every leaf SourceDefault. This is the "fresh install, no overrides"
// scenario — the validator already guarantees every required leaf is
// present, so the diff has full overlap to classify.
func TestComputeSettingsSources_DefaultBaselineAllDefault(t *testing.T) {
	kit, err := LoadKitConfigByKey("omakase")
	if err != nil {
		t.Fatalf("LoadKitConfigByKey(omakase): %v", err)
	}
	sources := computeSettingsSources(kit, kit)
	if len(sources) == 0 {
		t.Fatal("computeSettingsSources returned no entries for kit-vs-kit; want every leaf labelled")
	}
	for path, label := range sources {
		if label != SourceDefault {
			t.Errorf("path %q: got %q, want %q (kit-vs-kit must be all default)", path, label, SourceDefault)
		}
	}
}

// TestComputeSettingsSources_ProjectOverrideFlipsOneLeaf pins the
// per-leaf granularity: a single divergence promotes one path to
// SourceProject and leaves the rest at SourceDefault. The renderer
// relies on this so the viewer column highlights only the leaves the
// user actually edited.
func TestComputeSettingsSources_ProjectOverrideFlipsOneLeaf(t *testing.T) {
	kit, err := LoadKitConfigByKey("omakase")
	if err != nil {
		t.Fatalf("LoadKitConfigByKey(omakase): %v", err)
	}
	user := kit
	user.MCP.RecentCommentLimit = kit.MCP.RecentCommentLimit + 99

	sources := computeSettingsSources(user, kit)
	got, ok := sources["mcp.recent_comment_limit"]
	if !ok {
		t.Fatal("expected mcp.recent_comment_limit in sources map")
	}
	if got != SourceProject {
		t.Fatalf("mcp.recent_comment_limit: got %q, want %q", got, SourceProject)
	}
	// Neighbouring leaves remain default — the override is single-leaf.
	neighbour, ok := sources["mcp.max_comment_chars"]
	if !ok {
		t.Fatal("expected mcp.max_comment_chars in sources map")
	}
	if neighbour != SourceDefault {
		t.Fatalf("mcp.max_comment_chars: got %q, want %q (neighbour should stay default)", neighbour, SourceDefault)
	}
}

// TestComputeSettingsSources_UserOnlyLeafIsProject pins the "leaf
// absent from kit" branch: a user-introduced path (e.g. a tag synonym
// the kit does not ship) is SourceProject because there is no kit
// baseline to compare against. The classifier's nil-kit branch handles
// it without panicking on the missing counterpart.
func TestComputeSettingsSources_UserOnlyLeafIsProject(t *testing.T) {
	kit, err := LoadKitConfigByKey("omakase")
	if err != nil {
		t.Fatalf("LoadKitConfigByKey(omakase): %v", err)
	}
	user := kit
	user.TagSynonyms = map[string]string{"feat": "feature"}

	sources := computeSettingsSources(user, kit)
	got, ok := sources["tag_synonyms.feat"]
	if !ok {
		t.Fatal("expected tag_synonyms.feat in sources map")
	}
	if got != SourceProject {
		t.Fatalf("tag_synonyms.feat: got %q, want %q (user-only leaf)", got, SourceProject)
	}
}

// TestApplyEnvOverlay_PromotesPathWhenEnvSet pins the env layer
// promotion: a binding whose env var is set in the lookup flips the
// target path to SourceEnv even when the leaf was previously labelled
// default. Production callers pass an empty registry today; the test
// uses applyEnvOverlayWithBindings to inject a synthetic row so the
// end-to-end path is exercised before any real env var lands.
func TestApplyEnvOverlay_PromotesPathWhenEnvSet(t *testing.T) {
	sources := map[string]string{
		"theme.active":         SourceDefault,
		"mcp.cache_prompts":    SourceProject,
		"output.json_minified": SourceDefault,
	}
	bindings := []envOverlayBinding{
		{envVar: "OMAKITEN_TEST_THEME", path: "theme.active"},
	}
	lookup := func(name string) (string, bool) {
		if name == "OMAKITEN_TEST_THEME" {
			return "catppuccin", true
		}
		return "", false
	}
	applyEnvOverlayWithBindings(sources, lookup, bindings)
	if got := sources["theme.active"]; got != SourceEnv {
		t.Fatalf("theme.active after overlay: got %q, want %q", got, SourceEnv)
	}
	// Unrelated paths stay where they were.
	if got := sources["mcp.cache_prompts"]; got != SourceProject {
		t.Errorf("mcp.cache_prompts: overlay must not touch unrelated paths; got %q", got)
	}
	if got := sources["output.json_minified"]; got != SourceDefault {
		t.Errorf("output.json_minified: overlay must not touch unrelated paths; got %q", got)
	}
}

// TestApplyEnvOverlay_SkipsUnsetVars pins the "no env, no promotion"
// half: a binding whose env var is absent leaves the source map
// untouched. Production callers see this branch on every load (the
// registry is empty), so the no-op default behaviour is the one
// LoadBundle relies on staying side-effect-free.
func TestApplyEnvOverlay_SkipsUnsetVars(t *testing.T) {
	sources := map[string]string{"theme.active": SourceDefault}
	bindings := []envOverlayBinding{
		{envVar: "OMAKITEN_TEST_THEME", path: "theme.active"},
	}
	lookup := func(string) (string, bool) { return "", false }
	applyEnvOverlayWithBindings(sources, lookup, bindings)
	if got := sources["theme.active"]; got != SourceDefault {
		t.Fatalf("theme.active: unset env must not flip; got %q want %q", got, SourceDefault)
	}
}

// TestBundleSourceFor_NilMapFallsBackToDefault pins the consumer
// contract: bundles constructed without LoadBundle (test fixtures via
// newTwoBucketBundle, MCP composer mocks) have a nil Sources map and
// must report SourceDefault for any path. The accessor relies on this
// so the TUI viewer never renders an empty source cell.
func TestBundleSourceFor_NilMapFallsBackToDefault(t *testing.T) {
	b := Bundle{}
	if got := b.SourceFor("anything.at.all"); got != SourceDefault {
		t.Fatalf("nil Sources: SourceFor(...) = %q, want %q", got, SourceDefault)
	}
}

// TestBundleSourceFor_TrimsAndResolves pins the dot-path lookup: paths
// land in the map verbatim, but whitespace at call sites is tolerated
// so the TUI viewer can pass the EffectiveTuple join string without
// pre-trimming.
func TestBundleSourceFor_TrimsAndResolves(t *testing.T) {
	b := Bundle{
		Sources: map[string]string{"mcp.cache_prompts": SourceProject},
	}
	if got := b.SourceFor("mcp.cache_prompts"); got != SourceProject {
		t.Fatalf("direct lookup: got %q want %q", got, SourceProject)
	}
	if got := b.SourceFor("  mcp.cache_prompts  "); got != SourceProject {
		t.Fatalf("whitespace lookup: got %q want %q", got, SourceProject)
	}
	if got := b.SourceFor("missing.path"); got != SourceDefault {
		t.Fatalf("missing path: got %q want %q", got, SourceDefault)
	}
}
