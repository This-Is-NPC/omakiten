// Package list holds the four item-window surfaces: Cards, Window,
// Picker and Viewport. They share a concept — items, cursor or scroll,
// a visible window, partials at the edges — but they are not one state
// machine: Cards starts with no selection, Window starts at 0, Picker
// exports Cursor/Scroll and dispatches keys, Viewport has no cursor.
//
// scrollwindow is the math leaf underneath. This package is itself a
// leaf: strings and geometry in, bytes and window state out. No config,
// domain, app or sqlite.
package list

import (
	"fmt"
)

// pageStep is the half-page key increment Picker and Viewport share.
// Floored at 4 so tiny viewports still feel distinct from j/k.
func pageStep(viewport int) int {
	step := viewport / 2
	if step < 4 {
		return 4
	}
	return step
}

// tr resolves a catalog key when text is wired; otherwise it formats
// the English fallback. Cards and Viewport both paint scroll hints
// through it.
func tr(text func(string) string, key, fallback string, args ...any) string {
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
