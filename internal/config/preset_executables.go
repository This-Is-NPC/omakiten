package config

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PresetExecutables lists what a bundle can run on the user's machine: exec
// hooks, the agent adapters it declares, and its bucket teams.
func PresetExecutables(bundle Bundle) []string {
	return slices.Concat(hookExecutables(bundle), adapterExecutables(bundle.Config.Agents), teamExecutables(bundle))
}

func hookExecutables(bundle Bundle) []string {
	var out []string
	for _, h := range bundle.Config.Hooks {
		if h.Do == "exec" {
			out = append(out, fmt.Sprintf("hook %s: %s", h.On, argvText(h.Args["argv"])))
		}
	}
	return out
}

func adapterExecutables(agents *AgentsSettings) []string {
	if agents == nil {
		return nil
	}
	var out []string
	for _, name := range slices.Sorted(maps.Keys(agents.Harnesses)) {
		out = append(out, fmt.Sprintf("agent harness %s: %s", name, agentArgvText(agents.Harnesses[name].Argv)))
	}
	return out
}

func teamExecutables(bundle Bundle) []string {
	var out []string
	for _, wf := range bundle.Workflows {
		for _, b := range wf.Buckets {
			if b.Agents == nil {
				continue
			}
			seats := []string{seatText(bundle, "lead", b.Agents.Lead)}
			for _, m := range b.Agents.Members {
				seats = append(seats, seatText(bundle, m.Key, m))
			}
			out = append(out, fmt.Sprintf("agent team on %s (%s): %s", b.Key, b.Agents.Trigger, strings.Join(seats, ", ")))
		}
	}
	return out
}

func argvText(raw any) string {
	var parts []string
	switch v := raw.(type) {
	case []string:
		parts = v
	case []any:
		for _, item := range v {
			parts = append(parts, fmt.Sprint(item))
		}
	}
	return strings.Join(parts, " ")
}

func agentArgvText(args []AgentArgYAML) string {
	var parts []string
	for _, a := range args {
		parts = append(parts, a.Values...)
	}
	return strings.Join(parts, " ")
}

// seatText names a seat's command and the harness and model it runs; what
// neither the role nor the preset sets comes from the user's preferences.
func seatText(bundle Bundle, key string, role AgentRoleConfig) string {
	var defaults AgentDefaultsConfig
	if bundle.Config.Agents != nil {
		defaults = bundle.Config.Agents.Defaults
	}
	harness, model := "harness from your preferences", "model from your preferences or the harness"
	if h := cmp.Or(role.Harness, defaults.Harness); h != "" {
		harness = "harness " + h
	}
	if m := cmp.Or(role.Model, defaults.Model); m != "" {
		model = "model " + m
	}
	return fmt.Sprintf("%s %s [%s, %s]", key, role.Command, harness, model)
}
