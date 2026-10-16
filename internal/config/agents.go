package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"omakiten/defaults"
	"omakiten/internal/domain"
)

// AgentArgYAML is one adapter argv element: a string, or a list of strings
// that expands as a group.
type AgentArgYAML domain.AgentArg

// UnmarshalYAML accepts a scalar or a sequence of scalars.
func (a *AgentArgYAML) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*a = AgentArgYAML{Values: []string{node.Value}}
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := node.Decode(&values); err != nil {
			return err
		}
		if len(values) == 0 {
			return fmt.Errorf("line %d: an argv group needs at least one value", node.Line)
		}
		*a = AgentArgYAML{Values: values, Group: true}
		return nil
	default:
		return fmt.Errorf("line %d: an argv element is a string or a list of strings", node.Line)
	}
}

// MarshalYAML writes a group as a list and a single value as a string.
func (a AgentArgYAML) MarshalYAML() (any, error) {
	if a.Group {
		return a.Values, nil
	}
	if len(a.Values) == 1 {
		return a.Values[0], nil
	}
	return a.Values, nil
}

// HarnessConfig is a harness adapter declaration: the headless command
// line that does a seat's work and exits.
type HarnessConfig struct {
	Argv []AgentArgYAML `yaml:"argv" json:"argv"`
}

// AgentDefaultsConfig names the harness, model, and timeout roles inherit.
type AgentDefaultsConfig struct {
	Harness string `yaml:"harness,omitempty" json:"harness,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// AgentsSettings declares agent adapters and defaults. The same shape is
// read from Omakiten's defaults, the user's preferences, and a preset.
type AgentsSettings struct {
	Defaults  AgentDefaultsConfig      `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Harnesses map[string]HarnessConfig `yaml:"harnesses,omitempty" json:"harnesses,omitempty"`
}

// AgentPreferences adds the user-only session limits to AgentsSettings.
type AgentPreferences struct {
	AgentsSettings `yaml:",inline"`
	MaxSessions    int `yaml:"max_sessions,omitempty" json:"max_sessions,omitempty"`
	MaxMembers     int `yaml:"max_members,omitempty" json:"max_members,omitempty"`
}

// AgentRoleConfig is one seat of a bucket team.
type AgentRoleConfig struct {
	Key     string `yaml:"key,omitempty" json:"key,omitempty"`
	Command string `yaml:"command" json:"command"`
	Harness string `yaml:"harness,omitempty" json:"harness,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Brief   string `yaml:"brief,omitempty" json:"brief,omitempty"`
}

// BucketAgentsConfig is the team a bucket opens for an entering task.
type BucketAgentsConfig struct {
	Trigger string            `yaml:"trigger" json:"trigger"`
	Lead    AgentRoleConfig   `yaml:"lead" json:"lead"`
	Members []AgentRoleConfig `yaml:"members,omitempty" json:"members,omitempty"`
}

var agentMemberKey = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// defaultAgentSettings reads the adapters Omakiten ships.
func defaultAgentSettings() (AgentsSettings, error) {
	raw, err := defaults.FS.ReadFile("agents.yaml")
	if err != nil {
		return AgentsSettings{}, err
	}
	var s AgentsSettings
	if err := decodeYAMLStrict(raw, &s); err != nil {
		return AgentsSettings{}, fmt.Errorf("embedded agent adapters: %w", err)
	}
	return s, nil
}

// mergeAgentAdapters resolves the effective adapters: Omakiten's templates,
// then the user's preferences, then the preset. A named adapter replaces
// the same name from a lower layer; defaults merge key by key. Only the
// user's own defaults serve as the fallback for missing executables.
func mergeAgentAdapters(prefs *AgentPreferences, preset *AgentsSettings) (domain.AgentAdapters, error) {
	base, err := defaultAgentSettings()
	if err != nil {
		return domain.AgentAdapters{}, err
	}
	out := domain.AgentAdapters{Harnesses: map[string]domain.HarnessSpec{}}
	layers := []AgentsSettings{base}
	if prefs != nil {
		layers = append(layers, prefs.AgentsSettings)
		out.Fallback = agentDefaults(prefs.Defaults)
		out.MaxSessions, out.MaxMembers = prefs.MaxSessions, prefs.MaxMembers
	}
	if preset != nil {
		layers = append(layers, *preset)
	}
	for _, layer := range layers {
		out.Defaults = overlayAgentDefaults(out.Defaults, layer.Defaults)
		for name, h := range layer.Harnesses {
			out.Harnesses[name] = domain.HarnessSpec{Argv: toDomainArgs(h.Argv)}
		}
	}
	return out, nil
}

func agentDefaults(d AgentDefaultsConfig) domain.AgentDefaults {
	return domain.AgentDefaults{Harness: d.Harness, Model: d.Model, Timeout: d.Timeout}
}

func overlayAgentDefaults(base domain.AgentDefaults, layer AgentDefaultsConfig) domain.AgentDefaults {
	if layer.Harness != "" {
		base.Harness = layer.Harness
	}
	if layer.Model != "" {
		base.Model = layer.Model
	}
	if layer.Timeout != "" {
		base.Timeout = layer.Timeout
	}
	return base
}

func toDomainArgs(args []AgentArgYAML) []domain.AgentArg {
	out := make([]domain.AgentArg, len(args))
	for i, a := range args {
		out[i] = domain.AgentArg(a)
	}
	return out
}

func toDomainBucketAgents(c *BucketAgentsConfig) *domain.BucketAgents {
	if c == nil {
		return nil
	}
	out := &domain.BucketAgents{Trigger: c.Trigger, Lead: toDomainRole(c.Lead)}
	out.Lead.Key = domain.AgentLeadKey
	for _, m := range c.Members {
		out.Members = append(out.Members, toDomainRole(m))
	}
	return out
}

func toDomainRole(r AgentRoleConfig) domain.AgentRole {
	return domain.AgentRole{Key: r.Key, Command: r.Command, Harness: r.Harness, Model: r.Model, Timeout: r.Timeout, Brief: r.Brief}
}

// validateAgentAdapters checks the adapter declarations of one layer.
func validateAgentAdapters(scope string, s AgentsSettings) error {
	for name, h := range s.Harnesses {
		if len(h.Argv) == 0 {
			return fmt.Errorf("%s.harnesses.%s.argv: required non-empty list", scope, name)
		}
	}
	return validateAgentTimeout(scope+".defaults.timeout", s.Defaults.Timeout)
}

// validateBucketAgents checks every bucket team of the bundle's workflows
// against the effective adapters and, when present, the command bindings.
func validateBucketAgents(bundle Bundle) error {
	for _, wf := range bundle.Workflows {
		for _, b := range wf.Buckets {
			if b.Agents == nil {
				continue
			}
			if err := validateBucketTeam(bundle, fmt.Sprintf("workflows.%s.buckets.%s.agents", wf.Key, b.Key), *b.Agents); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBucketTeam(bundle Bundle, scope string, team BucketAgentsConfig) error {
	if team.Trigger != domain.AgentTriggerAuto && team.Trigger != domain.AgentTriggerManual {
		return fmt.Errorf("%s.trigger: must be %s or %s", scope, domain.AgentTriggerAuto, domain.AgentTriggerManual)
	}
	if team.Lead.Key != "" && team.Lead.Key != domain.AgentLeadKey {
		return fmt.Errorf("%s.lead.key: the lead's key is %s", scope, domain.AgentLeadKey)
	}
	if err := validateAgentRole(bundle, scope+".lead", team.Lead); err != nil {
		return err
	}
	seen := map[string]bool{domain.AgentLeadKey: true}
	for i, m := range team.Members {
		at := fmt.Sprintf("%s.members[%d]", scope, i)
		if !agentMemberKey.MatchString(m.Key) {
			return fmt.Errorf("%s.key: required kebab-case key", at)
		}
		if seen[m.Key] {
			return fmt.Errorf("%s.key: %q is already used in this team", at, m.Key)
		}
		seen[m.Key] = true
		if err := validateAgentRole(bundle, at, m); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentRole(bundle Bundle, scope string, role AgentRoleConfig) error {
	if strings.TrimSpace(role.Command) == "" {
		return fmt.Errorf("%s.command: required", scope)
	}
	if bundle.Commands != nil {
		if _, ok := bundle.Commands[role.Command]; !ok {
			return fmt.Errorf("%s.command: %q is not bound in commands", scope, role.Command)
		}
	}
	if role.Harness != "" {
		if _, ok := bundle.AgentAdapters.Harnesses[role.Harness]; !ok {
			return fmt.Errorf("%s.harness: %q is not a declared harness", scope, role.Harness)
		}
	}
	return validateAgentTimeout(scope+".timeout", role.Timeout)
}

// validateAgentTimeout accepts an empty timeout or a positive Go duration.
func validateAgentTimeout(scope, timeout string) error {
	if timeout == "" {
		return nil
	}
	if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
		return fmt.Errorf("%s: %q is not a positive Go duration such as 30m", scope, timeout)
	}
	return nil
}
