package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"omakiten/internal/domain"
)

// TestMergeAgentAdaptersPrecedence verifies Omakiten templates < preferences
// < preset, with only the preferences' defaults kept as the fallback.
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
	if got.Fallback != (domain.AgentDefaults{Harness: "opencode", Model: "pref-model"}) {
		t.Fatalf("fallback = %+v, want exactly the preferences' defaults", got.Fallback)
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

// TestShippedHarnessesEndOptionsBeforeThePrompt verifies every shipped
// harness whose prompt is positional ends option parsing with `--` first,
// so a variadic flag cannot swallow the prompt.
func TestShippedHarnessesEndOptionsBeforeThePrompt(t *testing.T) {
	shipped, err := defaultAgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	for name, h := range shipped.Harnesses {
		for i, arg := range h.Argv {
			if len(arg.Values) != 1 || arg.Values[0] != "{prompt}" || arg.Group {
				continue
			}
			if previous := h.Argv[i-1].Values; previous[len(previous)-1] != "--" && !strings.HasPrefix(previous[len(previous)-1], "--prompt") {
				t.Errorf("harness %s passes {prompt} after %q, want `--` before a positional prompt", name, previous)
			}
		}
	}
}

// TestShippedAdaptersChooseNothing verifies Omakiten ships how to call each
// harness and launcher but never which one a role uses.
func TestShippedAdaptersChooseNothing(t *testing.T) {
	shipped, err := defaultAgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	if shipped.Defaults != (AgentDefaultsConfig{}) {
		t.Fatalf("shipped defaults = %+v, want none", shipped.Defaults)
	}
	got, err := mergeAgentAdapters(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Defaults != (domain.AgentDefaults{}) || got.Fallback != (domain.AgentDefaults{}) {
		t.Fatalf("effective defaults without preferences or preset = %+v / %+v, want none", got.Defaults, got.Fallback)
	}
	if len(got.Harnesses) == 0 || len(got.Launchers) == 0 {
		t.Fatal("the shipped adapter templates are missing")
	}
}

// TestPresetExecutablesNameEachSeatsHarness verifies the consent listing
// says which harness and model each seat runs, or that they come from the
// user's preferences.
func TestPresetExecutablesNameEachSeatsHarness(t *testing.T) {
	bundle := Bundle{
		Config: Settings{Agents: &AgentsSettings{Defaults: AgentDefaultsConfig{Model: "m1"}}},
		Workflows: []Workflow{{Key: "w", Buckets: []Bucket{{Key: "review", Agents: &BucketAgentsConfig{
			Trigger: "auto",
			Lead:    AgentRoleConfig{Command: "okt-task-review", Harness: "opencode"},
			Members: []AgentRoleConfig{{Key: "security", Command: "okt-task-review"}},
		}}}}},
	}
	got := teamExecutables(bundle)
	want := "agent team on review (auto): lead okt-task-review [harness opencode, model m1], security okt-task-review [harness from your preferences, model m1]"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("teamExecutables() = %q, want %q", got, want)
	}
}

// TestShippedLaunchersSubmitSeparately verifies every shipped launcher types
// text with send and presses Enter only in a separate submit step.
func TestShippedLaunchersSubmitSeparately(t *testing.T) {
	shipped, err := defaultAgentSettings()
	if err != nil {
		t.Fatal(err)
	}
	for name, l := range shipped.Launchers {
		if len(l.Submit) == 0 {
			t.Errorf("launcher %s has no submit step", name)
		}
		for _, arg := range l.Send {
			for _, v := range arg.Values {
				if strings.EqualFold(v, "enter") {
					t.Errorf("launcher %s presses Enter inside send: %+v", name, l.Send)
				}
			}
		}
	}
}
