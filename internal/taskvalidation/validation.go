package taskvalidation

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"omakiten/internal/domain"
)

var (
	spacesUnderscoresRE = regexp.MustCompile(`[\s_]+`)
	nonAlphanumRE       = regexp.MustCompile(`[^a-z0-9-]`)
	multiHyphenRE       = regexp.MustCompile(`-+`)
)

var ErrTitleRequired = errors.New("task title is required")

// ValidateTitle checks the form-required title rule and the domain length cap.
func ValidateTitle(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return ErrTitleRequired
	}
	return domain.ValidateTaskTitle(value)
}

// PositiveID parses the optional task parent field.
func PositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}

// NormalizeTags canonicalizes a comma-separated tag field for equality checks.
// Empty entries are discarded and the result is deterministic.
func NormalizeTags(value string) string {
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		if tag := strings.TrimSpace(part); tag != "" {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return strings.Join(tags, ",")
}

// CanonicalTags returns normalized, synonym-resolved, duplicate-free tags in
// input order for persistence writes.
func CanonicalTags(value string, synonyms map[string]string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = NormalizeTagName(part, synonyms)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

// NormalizeTagName converts a raw tag to canonical kebab-case and applies one
// hop of the active project's synonym table.
func NormalizeTagName(raw string, synonyms map[string]string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = spacesUnderscoresRE.ReplaceAllString(s, "-")
	s = nonAlphanumRE.ReplaceAllString(s, "")
	s = multiHyphenRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if canonical, ok := synonyms[s]; ok {
		return canonical
	}
	return s
}
