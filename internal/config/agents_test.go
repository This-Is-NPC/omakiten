package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMergeAgentAdaptersPrecedence verifies Omakiten defaults < preferences
// < preset, with the preferences' defaults kept as the fallback.
func TestMergeAgentAdaptersPrecedence(t *testing.T) {
	var prefs AgentPreferences
	if err := yaml.Unmarshal([]byte(`
defaults: {harness: opencode, model: pref-model}
harnesses:
  opencode: {argv: [my-opencode, "{prompt}"]}
max_members: 2
`), &prefs); err != nil {
		t.Fatal(err)
	}
	var preset AgentsSettings
	if err := yaml.Unmarshal([]byte(`
defaults: {model: preset-model, launcher: herdr}
harnesses:
  codex: {argv: [codex, [-m, "{model}"], "{prompt}"]}
`), &preset); err != nil {
		t.Fatal(err)
	}
	got, err := mergeAgentAdapters(&prefs, &preset)
	if err != nil {
		t.Fatal(err)
	}
	if got.Defaults.Harness != "opencode" || got.Defaults.Model != "preset-model" || got.Defaults.Launcher != "herdr" {
		t.Fatalf("defaults = %+v, want preferences' harness and preset's model and launcher", got.Defaults)
	}
	if got.Fallback.Harness != "opencode" || got.Fallback.Launcher != "tmux" {
		t.Fatalf("fallback = %+v, want the preferences over Omakiten's defaults", got.Fallback)
	}
	if got.Harnesses["opencode"].Argv[0].Values[0] != "my-opencode" || got.Harnesses["claude-code"].Argv[0].Values[0] != "claude" {
		t.Fatalf("harnesses = %+v, want the preference replacing opencode and the shipped claude-code kept", got.Harnesses)
	}
	if model := got.Harnesses["codex"].Argv[1]; !model.Group || model.Values[1] != "{model}" {
		t.Fatalf("codex argv group = %+v", model)
	}
	if got.MaxMembers != 2 || got.MaxSessions != 0 {
		t.Fatalf("limits = %d/%d, want preferences' max_members and no session limit", got.MaxSessions, got.MaxMembers)
	}
}

func TestPresetAgentsCannotSetLimits(t *testing.T) {
	var preset AgentsSettings
	if err := decodeYAMLStrict([]byte("max_sessions: 3\n"), &preset); err == nil {
		t.Fatal("preset agents accepted max_sessions; limits belong to preferences")
	}
}

func TestValidateBucketTeam(t *testing.T) {
	adapters, err := mergeAgentAdapters(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := Bundle{Commands: map[string]CommandSpec{"okt-task-review": {}}, AgentAdapters: adapters}
	ok := BucketAgentsConfig{Trigger: "auto", Lead: AgentRoleConfig{Command: "okt-task-review"}, Members: []AgentRoleConfig{{Key: "security", Command: "okt-task-review", Harness: "opencode"}}}
	if err := validateBucketTeam(bundle, "agents", ok); err != nil {
		t.Fatalf("valid team rejected: %v", err)
	}
	for name, mutate := range map[string]func(*BucketAgentsConfig){
		"trigger":          func(c *BucketAgentsConfig) { c.Trigger = "always" },
		"unbound command":  func(c *BucketAgentsConfig) { c.Lead.Command = "okt-missing" },
		"unknown harness":  func(c *BucketAgentsConfig) { c.Members[0].Harness = "codex" },
		"reserved key":     func(c *BucketAgentsConfig) { c.Members[0].Key = "lead" },
		"key not kebab":    func(c *BucketAgentsConfig) { c.Members[0].Key = "Sec" },
		"unknown launcher": func(c *BucketAgentsConfig) { c.Lead.Launcher = "screen" },
	} {
		team := ok
		team.Members = append([]AgentRoleConfig(nil), ok.Members...)
		mutate(&team)
		if err := validateBucketTeam(bundle, "agents", team); err == nil || !strings.HasPrefix(err.Error(), "agents.") {
			t.Fatalf("%s: error = %v, want a scoped rejection", name, err)
		}
	}
}
