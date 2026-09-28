// Package choice paints a selectable option's control glyph and label.
// Screens translate domain into Option and Mode; this package does not
// know what the option is.
//
// It is a leaf: strings in, strings out, no config, domain, app, sqlite,
// parent tui import, or sibling adapter. Cursor chrome stays on the
// screen (kit.CursorMarker). Row does not paint Detail — dropdown owns
// the join because the two picker screens already disagree on how.
package choice

import (
	"omakiten/internal/tui/components/screenkit"
)

// Mode is which control mark an option wears.
type Mode int

const (
	Radio Mode = iota
	Checkbox
	Toggle
)

// Option is one selectable row. Screens fill it from domain; this
// package only reads Label and Selected when painting.
type Option struct {
	Label    string
	Detail   string
	Selected bool
}

// Glyph is the control mark for mode and selected.
// Radio: "•" / " "; Checkbox: "[x]" / "[ ]"; Toggle: "◉" / "☐".
func Glyph(mode Mode, selected bool) string {
	switch mode {
	case Radio:
		if selected {
			return "•"
		}
		return " "
	case Checkbox:
		if selected {
			return "[x]"
		}
		return "[ ]"
	case Toggle:
		if selected {
			return "◉"
		}
		return "☐"
	default:
		return ""
	}
}

// Row is Glyph, a space, and Label. Detail is left to dropdown.
func Row(mode Mode, opt Option) string {
	return Glyph(mode, opt.Selected) + " " + screenkit.Sanitize(opt.Label)
}
