package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadBundleAcceptsMCPSettings round-trips a YAML carrying a fully
// populated `config.mcp` block and verifies the loader preserves every
// declared field — including pointer booleans (omitted == nil, declared
// false == non-nil with *false). Strict validator now rejects omitted
// fields, so this fixture declares them all.
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
    recent_context_limit: 2
    next_work_limit: 4
    similar_task_limit: 6
  tui:
    token_badge: { yellow_at: 150, red_at: 400 }
  template_defaults: [task]
  priorities:
    - {id: 1, value: low}
    - {id: 2, value: normal, default: true}
    - {id: 3, value: high}
  severities:
    - {id: 1, value: info}
    - {id: 2, value: warning, default: true}
    - {id: 3, value: error}
  views:
    board: { sort: {field: created_at, order: desc}, filter: {priority: []} }
    table: { sort: {field: created_at, order: desc}, filter: {priority: [], bucket: []} }
    graph: { sort: {field: id, order: asc} }
    logs: { sort: {order: desc}, limit: 50, filter: {source: []} }
    task_activity: { sort: {order: asc} }
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

// TestMCPSettingsRejectsOmittedFields locks the strict contract: every
// MCP knob is required in the loaded bundle. The kit's `defaults/
// omakiten.yaml` is the canonical source; any user file missing a
// field fails loudly so authoring mistakes never silently fall through
// to a code-side default (which no longer exists).
func TestMCPSettingsRejectsOmittedFields(t *testing.T) {
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

	if _, err := LoadBundle(configPath); err == nil {
		t.Fatal("LoadBundle() = nil, want validation error: bundle without config.mcp must be rejected")
	}
}

// TestLoadBundleRejectsNegativeMCPSettings ensures the validator
// surfaces obviously-wrong values. Negative recent_comment_limit fails
// in the strict-mode check (must be > 0) with a descriptive message.
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
    max_comment_chars: 0
    include_workflow_in_continue: true
    cache_prompts: true
    recent_context_limit: 3
    next_work_limit: 5
    similar_task_limit: 5
  tui:
    token_badge: { yellow_at: 150, red_at: 400 }
  template_defaults: [task]
  priorities:
    - {id: 1, value: low}
    - {id: 2, value: normal, default: true}
    - {id: 3, value: high}
  severities:
    - {id: 1, value: info}
    - {id: 2, value: warning, default: true}
    - {id: 3, value: error}
  views:
    board: { sort: {field: created_at, order: desc}, filter: {priority: []} }
    table: { sort: {field: created_at, order: desc}, filter: {priority: [], bucket: []} }
    graph: { sort: {field: id, order: asc} }
    logs: { sort: {order: desc}, limit: 50, filter: {source: []} }
    task_activity: { sort: {order: asc} }
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
