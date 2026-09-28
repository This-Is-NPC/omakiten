package tui

import (
	"fmt"
)

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
