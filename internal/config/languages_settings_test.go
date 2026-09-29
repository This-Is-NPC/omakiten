package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLanguageSettings_EffectiveAppliesDefaults(t *testing.T) {
	s := LanguageSettings{}
	eff := s.Effective()
	if eff.CLI != "en" {
		t.Fatalf("default CLI: got %q, want %q", eff.CLI, "en")
	}
	if eff.TUI != "en" {
		t.Fatalf("default TUI: got %q, want %q", eff.TUI, "en")
	}
	if eff.AgentOutput != "" {
		t.Fatalf("default AgentOutput: got %q, want empty", eff.AgentOutput)
	}
}

func TestLanguageSettings_EffectiveKeepsConfiguredValues(t *testing.T) {
	s := LanguageSettings{CLI: "pt-br", TUI: "en", AgentOutput: "English"}
	eff := s.Effective()
	if eff.CLI != "pt-br" || eff.TUI != "en" || eff.AgentOutput != "English" {
		t.Fatalf("Effective mutated configured values: %+v", eff)
	}
}

func TestLanguageSettings_PartialOverrideFillsRest(t *testing.T) {
	s := LanguageSettings{CLI: "pt-br"}
	eff := s.Effective()
	if eff.CLI != "pt-br" {
		t.Fatalf("CLI override lost: %q", eff.CLI)
	}
	if eff.TUI != "en" {
		t.Fatalf("TUI default not applied: %q", eff.TUI)
	}
	if eff.AgentOutput != "" {
		t.Fatalf("AgentOutput default not empty: %q", eff.AgentOutput)
	}
}

func TestLanguageSettings_YAMLRoundTrip(t *testing.T) {
	in := `cli: pt-br
tui: en
agent_output: English
`
	var got LanguageSettings
	if err := yaml.Unmarshal([]byte(in), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CLI != "pt-br" || got.TUI != "en" || got.AgentOutput != "English" {
		t.Fatalf("decoded: %+v", got)
	}
	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var redecoded LanguageSettings
	if err := yaml.Unmarshal(out, &redecoded); err != nil {
		t.Fatalf("redecode: %v", err)
	}
	if redecoded != got {
		t.Fatalf("round trip lost data: got %+v, want %+v", redecoded, got)
	}
}
