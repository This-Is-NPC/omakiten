// Package multilineform renders a `bubbles/textarea` inside the
// canonical bordered form chrome shared by the task description, the
// inline comment-add modal, and the dedicated comment-edit overlay.
//
// Before this package, each render site inlined the same six-step
// dance: shallow-copy the textarea, set the cursor style, set the
// inner width, set the inner height, choose a border accent based on
// focus, and render the bordered chrome around `input.View()`. Two of
// the three sites also forgot to keep the persistent model's
// width/height in sync with the on-screen geometry, which left the
// bubbles textarea operating at its package-default 40-column wrap
// while the render-time copy was sized to ~68 columns. After the
// first `Update(msg)` the persistent viewport's `yOffset` referred to
// a row that no longer existed under the new wrap, so `View()`
// returned empty rows and the field "vanished" on every keystroke.
//
// The package solves both problems with a tiny pair of primitives:
//
//   - Render(input, width, height, focused, theme) — call from any
//     view function. Owns the copy + style overrides + bordered
//     chrome assembly.
//   - Resize(input, width, height, theme) — call from the open / mode
//     entry / window-resize handler. Mutates the persistent model so
//     subsequent `Update(msg)` calls wrap content at the same width
//     Render uses, keeping the viewport's yOffset valid.
//
// The package is a leaf: it imports `bubbles/textarea` and `lipgloss`
// only, never the parent `tui` package or any sibling adapter. It
// matches the precedent set by `internal/tui/components/{gridtable,
// picker, scrollwindow, viewport, detailscreen}` and is allowed by
// `internal/arch/arch_test.go::hexagonalRules`.
package multilineform

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// minInnerWidth is the smallest inner (content) width the textarea is
// allowed to shrink to. Below this the wrap math collapses and the
// caret becomes unusable; the floor matches the prior inline guard
// that lived at every render site.
const minInnerWidth = 8

// Theme bundles the lipgloss styles a multiline form input needs.
// Owners (e.g. `internal/tui/styles.go`) construct one Theme per
// surface and pass it into Render / Resize. The package itself owns
// no theming — only the render shape.
type Theme struct {
	// Border is the bordered chrome wrapping the textarea: border
	// glyphs, padding, foreground, and the inactive BorderForeground
	// color. Width and Height set on this style are ignored — Render
	// always overrides both from its width/height args so the chrome
	// follows the live terminal geometry.
	Border lipgloss.Style

	// BorderActive is the BorderForeground color applied when
	// `focused` is true. Every other property of `Border` carries
	// over so the focus swap is visually a single-property delta.
	BorderActive lipgloss.TerminalColor

	// Cursor is assigned to `input.Cursor.Style` so the reverse-video
	// cursor cell renders with an explicit foreground. Without this,
	// some terminals collapse the cursor against the textarea's
	// default line styling and the caret disappears (see commit
	// 1eed321 / 7f8b292's documentation note).
	Cursor lipgloss.Style
}

// Render produces the bordered, sized, focus-accented multiline
// input. `width` is the OUTER cell width — the inner textarea width
// is derived by subtracting `theme.Border`'s horizontal padding so
// border + padding + content fit exactly inside `width`. `height` is
// the textarea's visible row count.
//
// `input` is taken by value: Render receives a shallow copy and never
// mutates the caller's persistent model. SetWidth/SetHeight on the
// copy are required because lipgloss styling alone cannot resize the
// textarea's internal viewport — but the copy is discarded, so the
// caller MUST also call Resize on the persistent model whenever the
// surrounding layout changes (typically at form-open and window-resize
// time). See package doc for the bug this protects against.
func Render(input textarea.Model, width, height int, focused bool, theme Theme) string {
	inner := innerWidth(width, theme)
	input.Cursor.Style = theme.Cursor
	input.SetWidth(inner)
	input.SetHeight(height)
	chrome := theme.Border.Width(width).Height(height)
	if focused {
		chrome = chrome.BorderForeground(theme.BorderActive)
	}
	return chrome.Render(input.View())
}

// Resize calibrates the textarea's persistent viewport so subsequent
// `Update(msg)` calls wrap content at the same width Render will use.
// Call from the open/mode-entry handler (after SetValue, before
// CursorEnd so the end-of-content scroll is computed against the
// correct wrap) and from any window-resize handler.
//
// Without Resize, a freshly-created textarea retains the bubbles
// package-default geometry (40 cols / 6 rows). Render's render-time
// SetWidth on a shallow copy can't fix this because it never touches
// the persistent model — the persistent viewport's yOffset stays
// computed against the default wrap, then desyncs the moment a
// keystroke arrives. The visible symptom is an instantly-empty field
// after the first `Update(msg)`.
func Resize(input *textarea.Model, width, height int, theme Theme) {
	input.SetWidth(innerWidth(width, theme))
	input.SetHeight(height)
}

// innerWidth derives the textarea's inner content width from the
// outer cell width and the theme's horizontal padding. Floors at
// `minInnerWidth` so a tiny terminal still leaves a usable column.
func innerWidth(outer int, theme Theme) int {
	inner := outer - theme.Border.GetHorizontalPadding()
	if inner < minInnerWidth {
		inner = minInnerWidth
	}
	return inner
}
