package tui

import (
	"fmt"

	"omakiten/internal/tui/components/scrollwindow"
)

// sliceViewport is the line-based slicer used by callers that emit
// their own combined "▲ X above · ▼ Y below · j/k..." footer outside
// the viewport budget — i.e. the detail-screen surfaces (renderHelp,
// applyTaskViewScroll). Routes through scrollwindow.Slice with
// HintsNone since the caller already reduced viewport by 1 to leave
// room for the footer it adds itself.
//
// Scroll is clamped to a valid offset so callers can store a "1 << 20"
// sentinel for "jump to end" without doing the bounds check themselves.
func sliceViewport(lines []string, scroll, viewport int) (visible []string, above, below int) {
	if viewport <= 0 || len(lines) <= viewport {
		return lines, 0, 0
	}
	if scroll < 0 {
		scroll = 0
	}
	if maxOffset := len(lines) - viewport; scroll > maxOffset {
		scroll = maxOffset
	}
	heights := make([]int, len(lines))
	for i := range heights {
		heights[i] = 1
	}
	end := scrollwindow.Slice(scroll, heights, viewport, scrollwindow.HintsNone)
	return lines[scroll:end], scrollwindow.Above(scroll), scrollwindow.Below(end, len(lines))
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
