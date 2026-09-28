package studio

import (
	"omakiten/internal/config/bundledraft"
)

// tr picks the catalog value when text is wired and the key resolves, otherwise
// the English fallback. Format args are applied after resolution so packs can
// keep "%s" / "%d" placeholders.
func tr(text bundledraft.Text, key, fallback string, args ...any) string {
	return bundledraft.Tr(text, key, fallback, args...)
}

// (m Screen).tr is the screen-bound form — catalog via kit.T, English fallback.
func (m Screen) tr(key, fallback string, args ...any) string {
	return tr(m.t, key, fallback, args...)
}
