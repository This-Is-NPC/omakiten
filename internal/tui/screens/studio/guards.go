package studio

import "strings"

func (m Screen) resolveStudioGuardHint(hint string) string {
	if hint == "" || !strings.Contains(hint, "${{intl:") {
		return ""
	}
	if snap := m.repos.activeSnapshot(); snap != nil {
		return snap.ResolveGuardHint(hint)
	}
	return m.repos.Catalog.Resolve(hint)
}
