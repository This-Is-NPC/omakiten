package app

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	spacesUnderscoresRE = regexp.MustCompile(`[\s_]+`)
	nonAlphanumRE       = regexp.MustCompile(`[^a-z0-9-]`)
	multiHyphenRE       = regexp.MustCompile(`-+`)
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
	s := strings.ToLower(strings.TrimSpace(raw))
	s = spacesUnderscoresRE.ReplaceAllString(s, "-")
	s = nonAlphanumRE.ReplaceAllString(s, "")
	s = multiHyphenRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if synonyms != nil {
		if canonical, ok := synonyms[s]; ok {
			s = canonical
		}
	}
	return s
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
