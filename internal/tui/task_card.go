package tui

import (
	"omakiten/internal/tui/components/card"
)

// cardPainter is the card renderer bound to the current theme.
//
// The whole card — prefix, hanging title, metadata rows, badge line, border
// precedence and the height a column budgets against — lives in components/card
// now. The root's contribution is the theme and the shared style cache, because
// the board repaints a full column on every keystroke and lipgloss hands back a
// fresh Style for each Width call.
func (m Model) cardPainter() card.Painter {
	return card.Painter{Styles: m.styles.screenStyles(), Cache: m.cardStyles}
}
