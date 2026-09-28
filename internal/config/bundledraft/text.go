package bundledraft

import (
	"fmt"
)

// Text resolves an i18n catalog key to operator-facing copy. Nil means use the
// English fallback at each call site — domain tests assert that fallback, and
// the en pack ships the same wording so a painted body and a Report() agree.
type Text func(key string) string

// Tr picks the catalog value when text is wired and the key resolves, otherwise
// the English fallback. Format args are applied after resolution so packs can
// keep "%s" / "%d" placeholders.
func Tr(text Text, key, fallback string, args ...any) string {
	tmpl := fallback
	if text != nil {
		if got := text(key); got != "" && got != key {
			tmpl = got
		}
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}
