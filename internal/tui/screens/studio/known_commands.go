package studio

import (
	"omakiten/internal/commandcatalog"
)

func (m Screen) knownCommandNames() []string {
	if len(m.repos.CommandNames) > 0 {
		return m.repos.CommandNames
	}
	return commandcatalog.CommandNames()
}
