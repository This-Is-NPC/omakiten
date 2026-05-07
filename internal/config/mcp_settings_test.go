package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadBundleAcceptsMCPSettings round-trips a yaml carrying a populated
// `config.mcp` block. The merged Settings must preserve every declared field,
// including the boolean pointers (so that omitted == nil, declared false ==
// non-nil with *false). The Effective* accessors are checked against the
// canonical default constants on the side.
func TestLoadBundleAcceptsMCPSettings(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  context: { default_level: 2, max_tokens: 12000 }
  workflow: { active: default }
  theme: { active: catppuccin }
  mcp:
    recent_comment_limit: 3
    max_comment_chars: 400
    include_workflow_in_continue: false
    cache_prompts: false
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	mcp := bundle.Config.MCP
	if mcp.RecentCommentLimit != 3 {
		t.Fatalf("RecentCommentLimit = %d, want 3", mcp.RecentCommentLimit)
	}
	if mcp.MaxCommentChars != 400 {
		t.Fatalf("MaxCommentChars = %d, want 400", mcp.MaxCommentChars)
	}
	if mcp.IncludeWorkflowInContinue == nil || *mcp.IncludeWorkflowInContinue != false {
		t.Fatalf("IncludeWorkflowInContinue = %v, want *false", mcp.IncludeWorkflowInContinue)
	}
	if mcp.CachePrompts == nil || *mcp.CachePrompts != false {
		t.Fatalf("CachePrompts = %v, want *false", mcp.CachePrompts)
	}
	// Effective getters must surface the declared values verbatim.
	if mcp.EffectiveRecentCommentLimit() != 3 || mcp.EffectiveMaxCommentChars() != 400 {
		t.Fatalf("Effective getters disagree with declared values: %+v", mcp)
	}
	if mcp.EffectiveIncludeWorkflowInContinue() != false {
		t.Fatal("EffectiveIncludeWorkflowInContinue = true, want false")
	}
	if mcp.EffectiveCachePrompts() != false {
		t.Fatal("EffectiveCachePrompts = true, want false")
	}
}

// TestMCPSettingsDefaultsWhenOmitted guards the contract documented on
// MCPSettings: omitted fields resolve to the canonical defaults via the
// Effective* accessors so the runtime always sees a consistent picture.
func TestMCPSettingsDefaultsWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  context: { default_level: 2, max_tokens: 12000 }
  workflow: { active: default }
  theme: { active: catppuccin }
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
`)

	bundle, err := LoadBundle(configPath)
	if err != nil {
		t.Fatalf("LoadBundle() error = %v", err)
	}
	mcp := bundle.Config.MCP
	if mcp.EffectiveRecentCommentLimit() != DefaultRecentCommentLimit {
		t.Fatalf("RecentCommentLimit default = %d, want %d", mcp.EffectiveRecentCommentLimit(), DefaultRecentCommentLimit)
	}
	if mcp.EffectiveMaxCommentChars() != DefaultMaxCommentChars {
		t.Fatalf("MaxCommentChars default = %d, want %d", mcp.EffectiveMaxCommentChars(), DefaultMaxCommentChars)
	}
	if mcp.EffectiveIncludeWorkflowInContinue() != DefaultIncludeWorkflowInContinue {
		t.Fatalf("IncludeWorkflowInContinue default = %v, want %v", mcp.EffectiveIncludeWorkflowInContinue(), DefaultIncludeWorkflowInContinue)
	}
	if mcp.EffectiveCachePrompts() != DefaultCachePrompts {
		t.Fatalf("CachePrompts default = %v, want %v", mcp.EffectiveCachePrompts(), DefaultCachePrompts)
	}
}

// TestLoadBundleRejectsNegativeMCPSettings ensures the validator surfaces
// obviously-wrong values instead of silently coercing them — typos like a
// negative comment cap should fail loudly.
func TestLoadBundleRejectsNegativeMCPSettings(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "omakiten.yaml")
	writeFile(t, configPath, `version: 1
kit: { id: 1, key: default, name: Default }
config:
  output: { json_minified: true, omit_empty: true }
  context: { default_level: 2, max_tokens: 12000 }
  workflow: { active: default }
  theme: { active: catppuccin }
  mcp:
    recent_comment_limit: -5
workflows:
  - id: 1
    key: default
    name: Default
    buckets:
      - { id: 1, key: backlog, name: Backlog, position: 1 }
    transitions: []
`)

	_, err := LoadBundle(configPath)
	if err == nil {
		t.Fatal("LoadBundle() error = nil, want negative-value rejection")
	}
	if !strings.Contains(err.Error(), "recent_comment_limit") {
		t.Fatalf("LoadBundle() error = %v, want recent_comment_limit message", err)
	}
}
