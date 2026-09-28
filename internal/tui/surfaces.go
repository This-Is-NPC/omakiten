package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/tui/screenhost"
)

// tuiSurfaceDenied reports whether the census slug is off for the TUI
// surface. An empty snapshot or an empty table is unrestricted so tests
// that never wire surfaces keep their footers. Unknown slugs are not
// hidden — they are not an extra census row.
func (m Model) tuiSurfaceDenied(slug string) bool {
	if slug == "" {
		return false
	}
	table := m.surfaceTable()
	if len(table) == 0 {
		return false
	}
	row, ok := table[slug]
	if !ok || row.TUI == nil {
		return false
	}
	return !*row.TUI
}

func (m Model) surfaceTable() config.SurfaceTable {
	snap := m.repos.activeSnapshot()
	if snap == nil {
		return nil
	}
	return snap.Surfaces()
}

func (m Model) surfaceDeniedMessage(slug string) string {
	table := m.surfaceTable()
	reason := ""
	if row, ok := table[slug]; ok {
		reason = strings.TrimSpace(m.resolveSurfaceReason(row.Reason))
	}
	if reason != "" {
		return reason
	}
	return fmt.Sprintf("operation %q denied on tui", slug)
}

func (m Model) resolveSurfaceReason(reason string) string {
	if reason == "" {
		return ""
	}
	if m.repos.Catalog != nil {
		return m.repos.Catalog.Resolve(reason)
	}
	return pkgTUICatalog().Resolve(reason)
}

func (m Model) bindingDenied(screen screenhost.Screen, token string) bool {
	for _, frag := range bindingKeyFragments(token) {
		if _, denied := m.firstDeniedSlug(m.productSlugsForBinding(screen, frag)); denied {
			return true
		}
	}
	return false
}

func (m Model) firstDeniedSlug(slugs []string) (string, bool) {
	for _, slug := range slugs {
		if m.tuiSurfaceDenied(slug) {
			return slug, true
		}
	}
	return "", false
}

func (m Model) filterFooterBindings(screen screenhost.Screen, bindings []screenhost.FooterBinding) []screenhost.FooterBinding {
	if len(m.surfaceTable()) == 0 {
		return bindings
	}
	out := make([]screenhost.FooterBinding, 0, len(bindings))
	for _, binding := range bindings {
		if m.bindingDenied(screen, binding.Key) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

func (m Model) filterFooterTokens(screen screenhost.Screen, tokens []footerToken) []footerToken {
	if len(m.surfaceTable()) == 0 {
		return tokens
	}
	out := make([]footerToken, 0, len(tokens))
	for _, token := range tokens {
		if m.bindingDenied(screen, token.key) {
			continue
		}
		out = append(out, token)
	}
	return out
}

func (m Model) filterHostedHelpGroups(screen screenhost.Screen, groups []screenhost.HelpGroup) []screenhost.HelpGroup {
	if len(m.surfaceTable()) == 0 {
		return groups
	}
	out := make([]screenhost.HelpGroup, 0, len(groups))
	for _, group := range groups {
		bindings := make([]screenhost.HelpBinding, 0, len(group.Bindings))
		for _, binding := range group.Bindings {
			if m.bindingDenied(screen, binding.Key) {
				continue
			}
			bindings = append(bindings, binding)
		}
		group.Bindings = bindings
		out = append(out, group)
	}
	return out
}

func (m Model) helpTokenDenied(groupID, token string) bool {
	for _, frag := range bindingKeyFragments(token) {
		if _, denied := m.firstDeniedSlug(productSlugsForHelpGroup(groupID, frag)); denied {
			return true
		}
	}
	return false
}

// denyProductKey consumes a key that would invoke a tui:false census
// slug and writes the configured reason to status. Navigation, scroll,
// and help keys have no slug and pass through.
func (m *Model) denyProductKey(screen screenhost.Screen, key string) bool {
	if textEntryAbsorbsKey(screen, key) {
		return false
	}
	slug, denied := m.firstDeniedSlug(m.productSlugsForBinding(screen, key))
	if !denied {
		return false
	}
	m.status = m.surfaceDeniedMessage(slug)
	return true
}

func (m *Model) denyProductAction(outcome screenhost.Outcome) bool {
	slug, denied := m.firstDeniedSlug(m.productSlugsForAction(outcome.Action.Kind, outcome.Screen))
	if !denied {
		return false
	}
	m.status = m.surfaceDeniedMessage(slug)
	return true
}

func (m *Model) interceptDeniedKey(screen screenhost.Screen, msg tea.Msg) bool {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	return m.denyProductKey(screen, key.String())
}
