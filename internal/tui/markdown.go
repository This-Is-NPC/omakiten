package tui

import (
	"omakiten/internal/config"
	"omakiten/internal/tui/components/screenkit"
)

// tokensFromTheme extracts the four tokens the markdown renderer consumes
// from the active theme. Empty strings are kept so the StyleConfig builder
// can fall through to glamour's default (no Color set). Screens hold a
// markdown.Renderer built from Kit.Markdown; the root only needs Tokens on the kit.
func tokensFromTheme(theme config.Theme) screenkit.MarkdownTokens {
	pick := func(key string) string { return theme.Colors[key] }
	return screenkit.MarkdownTokens{
		ThemeKey:   theme.Key,
		Foreground: pick("foreground"),
		Border:     pick("border"),
		Primary:    pick("primary"),
		Secondary:  pick("secondary"),
	}
}

// toggleMarkdownRendered flips the session-only render mode and surfaces a
// status badge so the user gets confirmation that the keystroke landed.
func (m *Model) toggleMarkdownRendered() {
	m.markdownRendered = !m.markdownRendered
	if m.markdownRendered {
		m.status = m.t("tui.status.markdown_rendered")
	} else {
		m.status = m.t("tui.status.markdown_raw")
	}
}
