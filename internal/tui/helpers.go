package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenkit"
)

// truncateText caps s to a max VISIBLE cell budget, not a rune count.
// A CJK ideograph or emoji occupies two terminal cells, so the prior
// rune-count cut let a string with wide glyphs render at up to twice its
// budget and tip past the panel edge. Width is measured with
// ansi.StringWidth (display cells) and the cut walks runes while tracking
// accumulated cell width, reserving one cell for the trailing ellipsis so
// the result never exceeds max cells.
func truncateText(s string, max int) string {
	return screenkit.Truncate(s, max)
}

func renderFixedBox(lines []string, width int, border lipgloss.Style) string {
	return panel.FixedBox(lines, width, border)
}

func clampInt(value, minValue, maxValue int) int {
	return screenkit.Clamp(value, minValue, maxValue)
}

// renderPanel wraps any rendered body in the canonical panel chrome —
// leading newline (so the panel sits one row below the screen header),
// `m.styles.panel` border, and 2-space indent. Every render_*.go that
// drew its own surface used to inline this exact assembly; collapsing
// to a single call keeps the leading-blank / indent / border contract
// in one place so future tweaks (e.g. changing the indent) land here.
func (m Model) renderPanel(content string) string {
	return m.screenKit().Panel(content)
}

// formHint renders the canonical hint line for any form / modal: the
// supplied tokens joined by ` · ` and painted in the muted hint style.
// Centralising the assembly means every form surface uses the same
// separator and the same color, so the same key/action pair reads
// identically across task edit, comment add, comment edit, and any
// future form. Empty tokens are dropped so callers can build the
// list conditionally without leaking double separators.
func (m Model) formHint(tokens ...string) string {
	kept := tokens[:0]
	for _, t := range tokens {
		if t == "" {
			continue
		}
		kept = append(kept, t)
	}
	return m.styles.hint.Render(strings.Join(kept, " · "))
}
