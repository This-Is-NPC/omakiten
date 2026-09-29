package studio

import (
	"sort"
)

func (m Screen) knownCommandNames() []string {
	if len(m.repos.CommandNames) > 0 {
		return m.repos.CommandNames
	}
	commands := m.studioCandidateCommands()
	names := make([]string, 0, len(commands))
	for name := range commands {
		if name != "global" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
