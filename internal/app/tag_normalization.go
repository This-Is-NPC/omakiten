package app

import (
	"regexp"
	"strings"
	"sync/atomic"
	"unicode"
)

// tagSynonyms is the active alias map applied by NormalizeTagName. Lives
// as a process-global atomic pointer for the same reasons the priority +
// severity registries in internal/domain do: the call sites are leaf
// helpers (NormalizeTagName, TagLabel) with no place to inject a per-call
// resolver, and the runtime composition root writes the value once at
// startup. Tests that need a custom map call RegisterTagSynonyms with
// their fixture; production wires the bundle's config.tag_synonyms.
var tagSynonyms atomic.Pointer[map[string]string]

// RegisterTagSynonyms installs the active alias table. Composition root
// resolves the bundle's config.tag_synonyms and writes it here once at
// startup; tests that call NormalizeTagName without going through a
// composition root register their own map (often via testfixtures).
// Passing nil clears the registry so an unwired runtime still returns
// kebab-case tags — just without alias collapsing.
func RegisterTagSynonyms(synonyms map[string]string) {
	if synonyms == nil {
		tagSynonyms.Store(nil)
		return
	}
	copyMap := make(map[string]string, len(synonyms))
	for k, v := range synonyms {
		copyMap[k] = v
	}
	tagSynonyms.Store(&copyMap)
}

var (
	spacesUnderscoresRE = regexp.MustCompile(`[\s_]+`)
	nonAlphanumRE       = regexp.MustCompile(`[^a-z0-9-]`)
	multiHyphenRE       = regexp.MustCompile(`-+`)
)

// NormalizeTagName converts a raw tag name to its canonical kebab-case form,
// then applies one hop of synonym substitution from the registry installed
// by RegisterTagSynonyms. Two-hop chains (a→b→c) are intentionally not
// followed — the validator rejects those at config-load time so the runtime
// stays predictable.
func NormalizeTagName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = spacesUnderscoresRE.ReplaceAllString(s, "-")
	s = nonAlphanumRE.ReplaceAllString(s, "")
	s = multiHyphenRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if reg := tagSynonyms.Load(); reg != nil {
		if canonical, ok := (*reg)[s]; ok {
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
