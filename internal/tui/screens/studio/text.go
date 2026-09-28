package studio

import "omakiten/internal/config/bundledraft"

// Text resolves an i18n catalog key to operator-facing copy. Nil means use the
// English fallback at each call site — domain tests assert that fallback, and
// the en pack ships the same wording so a painted body and a Report() agree.
// It is the engine's own resolver type so a screen and a report cannot drift
// into two incompatible spellings of the same function.
type Text = bundledraft.Text

// tr picks the catalog value when text is wired and the key resolves, otherwise
// the English fallback. Format args are applied after resolution so packs can
// keep "%s" / "%d" placeholders.
func tr(text Text, key, fallback string, args ...any) string {
	return bundledraft.Tr(text, key, fallback, args...)
}

// (m Screen).tr is the screen-bound form — catalog via kit.T, English fallback.
func (m Screen) tr(key, fallback string, args ...any) string {
	return tr(m.t, key, fallback, args...)
}
