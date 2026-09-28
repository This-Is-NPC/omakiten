package tui

import (
	"fmt"

	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/header"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
)

// renderHeader renders the global title breadcrumb + the navigation
// kicker. Delegates to components/header so the four states (Home,
// Overlay, Compact fallback, Full strip) share one painter the gallery
// can inspect. Falls back to the compact "active tab + hint" form when
// the full nav row would overflow — that decision lives inside header.Render.
func (m Model) renderHeader() string {
	return header.Render(m.headerStyles(), m.headerOptions())
}

func (m Model) headerStyles() header.Styles {
	return header.Styles{
		Title:     m.styles.title,
		Nav:       m.styles.nav,
		ActiveNav: m.styles.activeNav,
		Hint:      m.styles.hint,
	}
}

func (m Model) headerOptions() header.Options {
	opts := header.Options{
		Width:          m.availableWidth(),
		Brand:          "omakiten",
		HomeLabel:      "00 // HOME",
		CompactHint:    "  tab/1-3 switch zones · ,// switch sub · 0 home · ctrl+o back",
		HomeReturnHint: "  ctrl+h returns here from any view",
	}
	if m.onHome() {
		opts.Home = true
		opts.Segment = "home"
		opts.SegmentHint = "select a project"
		return opts
	}
	opts.Segment = truncateText(m.project.Slug, 40)
	opts.SegmentHint = "local checkpoint"
	if m.helpOpen || len(m.screenStack) > 0 || m.studioApplyOverlayOpen() {
		opts.Overlay = true
		return opts
	}
	opts.Tops = make([]header.Item, 0, len(topOrder))
	for i, t := range topOrder {
		opts.Tops = append(opts.Tops, header.Item{
			Label:  fmt.Sprintf("%02d // %s", i+1, topLabels[t]),
			Active: t == m.top,
		})
	}
	subs := subsByTop[m.top]
	opts.Subs = make([]header.Item, 0, len(subs))
	for _, s := range subs {
		opts.Subs = append(opts.Subs, header.Item{
			Label:  fmt.Sprintf("// %s", subLabels[s]),
			Active: s == m.sub,
		})
	}
	return opts
}

func (m Model) studioApplyOverlayOpen() bool {
	descriptor, ok := m.activeScreenDescriptor()
	if !ok {
		return false
	}
	switch descriptor.ID {
	case screenhost.StudioWorkflow, screenhost.StudioCommands, screenhost.StudioPersonas, screenhost.StudioHooks:
		return m.studioScreen.ApplyOverlayOpen()
	}
	return false
}

// renderInput renders the modal text-input bar shown when the user is
// typing a move target (modeMove). Delegates to header.Input → field.RenderLine.
func (m Model) renderInput() string {
	return header.Input(m.styles.screenStyles(), m.status, m.moveInput, m.moveInput.Width)
}

// renderCurrentView resolves every addressable surface through its registered
// descriptor. Screen-local modes are represented by the hosted screen itself.
func (m Model) renderCurrentView() string {
	descriptor, ok := m.activeScreenDescriptor()
	if !ok {
		return ""
	}
	frame := m.screenFrame()
	screen := descriptor.Factory(legacyScreenHost{frame: frame, model: m})
	return screen.View(frame)
}

// footerToken is one keybinding entry on the footer hint row. `Primary`
// signals that the action is the focal verb for the current surface
// (`enter` / `n` / `e` etc.) — `renderFooter` highlights up to three
// primaries with `hintAccent` so the eye lands on them first; the rest
// stay in the muted `hint` style.
type footerToken struct {
	key     string
	label   string
	primary bool
}

// renderFooter ladders through every modal/overlay/view in priority
// order — the most-specific surface wins — and emits its keybinding
// hint as a list of structured tokens. Delegates layout to header.Footer.
func (m Model) renderFooter() string {
	return header.Footer(toKeyFooterTokens(m.footerTokens()), tokenstrip.KeyStyles{
		Primary:   m.styles.hintAccent,
		Secondary: m.styles.footer,
	})
}

func toKeyFooterTokens(tokens []footerToken) []tokenstrip.Key {
	out := make([]tokenstrip.Key, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenstrip.Key{Key: t.key, Label: t.label, Primary: t.primary})
	}
	return out
}

// helpToken returns the `?` token used at the trailing edge of every
// surface that has the help overlay reachable. Centralised so the
// `?` always appears last and never accidentally becomes a primary.
func (m Model) helpToken() footerToken {
	return footerToken{key: "?", label: m.t("tui.footer.help")}
}

// footerTokens returns the keybinding hint for the active surface as
// a structured list. Order encodes priority: the most relevant action
// for the surface comes first (and is usually marked `primary`).
func (m Model) footerTokens() []footerToken {
	switch {
	case m.mode != modeNormal:
		return m.filterFooterTokens(nil, []footerToken{
			{key: "enter", label: m.t("tui.footer.save"), primary: true},
			{key: "esc", label: m.t("tui.footer.cancel")},
			{key: "ctrl+c", label: m.t("tui.footer.quit")},
		})
	case len(m.screenStack) > 0:
		return m.activeScreenFooterTokens()
	case m.activeHostedScreenOK():
		// Extracted screens declare their own keybinding hints; the host
		// appends the global navigation trailer.
		return m.activeScreenFooterTokens()
	default:
		return m.filterFooterTokens(nil, []footerToken{
			{key: "enter", label: m.t("tui.footer.open"), primary: true},
			{key: "n", label: m.t("tui.footer.new"), primary: true},
			{key: "m", label: m.t("tui.footer.move"), primary: true},
			{key: "up/down", label: m.t("tui.footer.select")},
			{key: "pgup/pgdn", label: m.t("tui.footer.scroll")},
			{key: "g/G", label: m.t("tui.footer.top_bottom")},
			{key: keynav.Default.Tops.Primary(), label: m.t("tui.footer.tabs")},
			{key: keynav.Default.Zones.Primary(), label: m.t("tui.footer.zones")},
			{key: ",//", label: m.t("tui.footer.subs")},
			m.helpToken(),
		})
	}
}
