package tui

import (
	"fmt"
)

// sliceViewport returns the visible slice of `lines` at the given scroll
// offset and viewport height, plus the counts of lines hidden above and
// below the window. Pure math — does not render hints; callers compose
// their own indicator lines around the returned slice (single footer at
// the bottom for detail screens, split header/footer for the activity
// feed).
//
// When all content fits or `viewport` is non-positive, returns the full
// slice with above=below=0 — callers can use that to skip indicator rows
// entirely.
//
// Scroll is clamped to a valid offset so callers can store a "1 << 20"
// sentinel for "jump to end" without doing the bounds check themselves.
func sliceViewport(lines []string, scroll, viewport int) (visible []string, above, below int) {
	if viewport <= 0 || len(lines) <= viewport {
		return lines, 0, 0
	}
	offset := scroll
	if offset < 0 {
		offset = 0
	}
	maxOffset := len(lines) - viewport
	if offset > maxOffset {
		offset = maxOffset
	}
	return lines[offset : offset+viewport], offset, len(lines) - (offset + viewport)
}

// viewportFooterHint renders the standard "▲ X above · ▼ Y below · j/k
// pgup/pgdn g/G" footer used by every detail screen with a scrollable
// body. Returns "" when no content is hidden in either direction so the
// caller can safely concatenate without a stray blank line.
func (m Model) viewportFooterHint(above, below int) string {
	if above == 0 && below == 0 {
		return ""
	}
	return m.styles.hint.Render(fmt.Sprintf("▲ %d above · ▼ %d below  · j/k pgup/pgdn g/G", above, below))
}
