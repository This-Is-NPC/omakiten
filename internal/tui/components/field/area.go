package field

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
// Owners construct one Theme per surface and pass it into RenderArea /
// Resize. The package itself owns no theming — only the render shape.
type Theme struct {
	// Border is the bordered chrome wrapping the textarea: border
	// glyphs, padding, foreground, and the inactive BorderForeground
	// color. Width and Height set on this style are ignored — RenderArea
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

// RenderArea produces the bordered, sized, focus-accented multiline
// input. `width` is the OUTER cell width — the inner textarea width
// is derived by subtracting `theme.Border`'s horizontal padding so
// border + padding + content fit exactly inside `width`. `height` is
// the textarea's visible row count.
//
// `input` is taken by value: RenderArea receives a shallow copy and never
// mutates the caller's persistent model. SetWidth/SetHeight on the
// copy are required because lipgloss styling alone cannot resize the
// textarea's internal viewport — but the copy is discarded, so the
// caller MUST also call Resize on the persistent model whenever the
// surrounding layout changes (typically at form-open and window-resize
// time). See package doc for the bug this protects against.
func RenderArea(input textarea.Model, width, height int, focused bool, theme Theme) string {
	// innerWidth already floors what the textarea gets; the CHROME did not.
	// `theme.Border.Width(0)` leaves the border unconstrained, so the form would
	// paint a box wider than the cell that asked for it while the textarea
	// inside it stayed at its floor — the two disagreeing about the same width.
	width = max(1, width)
	height = max(1, height)
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
// `Update(msg)` calls wrap content at the same width RenderArea will use.
// Call from the open/mode-entry handler (after SetValue, before
// CursorEnd so the end-of-content scroll is computed against the
// correct wrap) and from any window-resize handler.
//
// Without Resize, a freshly-created textarea retains the bubbles
// package-default geometry (40 cols / 6 rows). RenderArea's render-time
// SetWidth on a shallow copy can't fix this because it never touches
// the persistent model — the persistent viewport's yOffset stays
// computed against the default wrap, then desyncs the moment a
// keystroke arrives. The visible symptom is an instantly-empty field
// after the first `Update(msg)`.
func Resize(input *textarea.Model, width, height int, theme Theme) {
	input.SetWidth(innerWidth(width, theme))
	input.SetHeight(height)
}

// WidthFor converts a budget of terminal cells into the `width` argument that
// makes RenderArea paint exactly that many cells wide.
//
// RenderArea's `width` is handed to lipgloss Style.Width, which sizes the content
// and padding but NOT the border — so the field lands on screen two cells wider
// than the number passed in. A caller sizing a form to fit inside a panel has
// to account for that, and the two that did were each doing it by subtracting a
// hand-picked constant from AvailableWidth: a private copy of a number this
// package already knows, and one of them was off by exactly the border, which
// pushed the editor past the panel that framed it.
//
// The border is measured from the live theme rather than assumed to be two, so
// a theme that changes it moves the fit with it.
func WidthFor(cells int, theme Theme) int {
	return cells - theme.Border.GetHorizontalBorderSize()
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
