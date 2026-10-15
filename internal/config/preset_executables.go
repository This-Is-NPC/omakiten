package config

import (
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
	for _, name := range slices.Sorted(maps.Keys(agents.Launchers)) {
		out = append(out, fmt.Sprintf("agent launcher %s: %s", name, agentArgvText(agents.Launchers[name].Open)))
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
			seats := []string{"lead " + b.Agents.Lead.Command}
			for _, m := range b.Agents.Members {
				seats = append(seats, m.Key+" "+m.Command)
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
