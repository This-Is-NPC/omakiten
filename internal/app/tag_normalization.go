package app

import (
	"strings"
	"unicode"

	"omakiten/internal/taskvalidation"
)

// NormalizeTagName converts a raw tag name to its canonical kebab-case
// form and applies one hop of synonym substitution from the supplied
// alias table. synonyms is per-project — Phase 3f dropped the process-
// global registry the previous shape relied on; callers thread the
// active project's `bundle.Config.TagSynonyms` (or
// `pr.TagSynonyms` from the BundleCache) so two projects can keep
// disjoint synonym tables in the same process.
//
// Two-hop chains (a→b→c) are intentionally not followed — the
// validator rejects those at config-load time so the runtime stays
// predictable.
//
// Passing a nil synonyms map skips the substitution step but still
// returns kebab-case output. Tests that do not care about synonyms
// pass nil.
func NormalizeTagName(raw string, synonyms map[string]string) string {
	return taskvalidation.NormalizeTagName(raw, synonyms)
}

// TagLabel derives a display label from the raw input (first letter uppercased).
func TagLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	runes := []rune(raw)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
