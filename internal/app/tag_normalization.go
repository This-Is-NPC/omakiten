package app

import (
	"regexp"
	"strings"
	"unicode"
)

var canonicalSynonyms = map[string]string{
	"golang":     "go",
	"javascript": "js",
	"typescript": "ts",
	"nodejs":     "node",
	"node-js":    "node",
	"postgres":   "postgresql",
	"psql":       "postgresql",
	"mongo":      "mongodb",
	"k8s":        "kubernetes",
	"tf":         "terraform",
	"py":         "python",
}

var (
	spacesUnderscoresRE = regexp.MustCompile(`[\s_]+`)
	nonAlphanumRE       = regexp.MustCompile(`[^a-z0-9-]`)
	multiHyphenRE       = regexp.MustCompile(`-+`)
)

// NormalizeTagName converts a raw tag name to its canonical kebab-case form.
func NormalizeTagName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = spacesUnderscoresRE.ReplaceAllString(s, "-")
	s = nonAlphanumRE.ReplaceAllString(s, "")
	s = multiHyphenRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if canonical, ok := canonicalSynonyms[s]; ok {
		s = canonical
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
