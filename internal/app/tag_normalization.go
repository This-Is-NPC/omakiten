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

// applyTagLabel pairs a normalised tag name with its display label
// derived from the raw input. Centralises the "name + label" tuple so
// Create and Edit paths cannot drift — both note service flows (and the
// sqlite UpdateNote tag replacement) feed raw user input through here
// to keep canonical Name and human-readable Label in lock-step.
//
// Returns empty name+label when the raw input normalises to an empty
// canonical (caller should skip the resulting tag).
func applyTagLabel(raw string, synonyms map[string]string) (name, label string) {
	name = NormalizeTagName(raw, synonyms)
	if name == "" {
		return "", ""
	}
	return name, TagLabel(raw)
}
