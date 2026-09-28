package tui

import (
	"fmt"

	"omakiten/internal/config"
)

func (m Model) resolveStudioCommand(bundle config.Bundle, name string) (string, error) {
	if m.repos.ResolveCommandPreview == nil {
		return "", fmt.Errorf("command preview is unavailable")
	}
	return m.repos.ResolveCommandPreview(bundle, name)
}
