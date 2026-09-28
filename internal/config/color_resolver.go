package config

import (
	"fmt"
	"regexp"
	"strings"
)

// ColorTransparent is the literal that resolves to lipgloss.NoColor{}.
const ColorTransparent = "transparent"

// ThemeColorPrefix marks a color reference into the active Theme.Colors
// map: `$theme.<key>` is replaced with the hex value at <key>.
const ThemeColorPrefix = "$theme."

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ResolvedColor is the discriminated result of ResolveColor — exactly
// one of Transparent/Color is meaningful at a time. Callers that target
// lipgloss.Style use the Apply* helpers rather than reading fields.
type ResolvedColor struct {
	Transparent bool
	Color       string
}

// IsTransparent reports whether the value asked for "no color".
func (r ResolvedColor) IsTransparent() bool { return r.Transparent }

// ResolveColor maps the notification color grammar onto a lipgloss color.
//
// Accepted forms:
//   - "transparent"        → ResolvedColor{Transparent: true}
//   - "$theme.<key>"       → theme.Colors[<key>] parsed as hex
//   - "#rrggbb"            → literal hex
//
// Returns an error for any other shape (including empty string and
// short hex like "#fff"). Errors carry enough context for the notification
// validator to wrap with the notification name + path.
func ResolveColor(value string, theme Theme) (ResolvedColor, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ResolvedColor{}, fmt.Errorf("color value is empty")
	}
	if trimmed == ColorTransparent {
		return ResolvedColor{Transparent: true}, nil
	}
	if strings.HasPrefix(trimmed, ThemeColorPrefix) {
		key := strings.TrimPrefix(trimmed, ThemeColorPrefix)
		if key == "" {
			return ResolvedColor{}, fmt.Errorf("color reference %q has empty theme key", value)
		}
		hex, ok := theme.Colors[key]
		if !ok {
			return ResolvedColor{}, fmt.Errorf("color reference %q: theme %q has no color %q", value, theme.Key, key)
		}
		if !hexColorPattern.MatchString(hex) {
			return ResolvedColor{}, fmt.Errorf("color reference %q resolved to %q which is not #rrggbb", value, hex)
		}
		return ResolvedColor{Color: hex}, nil
	}
	if hexColorPattern.MatchString(trimmed) {
		return ResolvedColor{Color: trimmed}, nil
	}
	return ResolvedColor{}, fmt.Errorf("color value %q is not transparent | $theme.<key> | #rrggbb", value)
}

// IsValidColorSyntax checks the grammar without resolving against a
// theme — used by the validator at LoadBundle time when the active
// theme is not yet known. Returns nil for transparent / `$theme.*`
// (assuming a non-empty key) / `#rrggbb`.
func IsValidColorSyntax(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("color value is empty")
	}
	if trimmed == ColorTransparent {
		return nil
	}
	if strings.HasPrefix(trimmed, ThemeColorPrefix) {
		if strings.TrimPrefix(trimmed, ThemeColorPrefix) == "" {
			return fmt.Errorf("color reference %q has empty theme key", value)
		}
		return nil
	}
	if hexColorPattern.MatchString(trimmed) {
		return nil
	}
	return fmt.Errorf("color value %q is not transparent | $theme.<key> | #rrggbb", value)
}
