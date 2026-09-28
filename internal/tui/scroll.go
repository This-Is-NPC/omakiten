package tui

import (
	"strings"
)

// panelViewportRows is the canonical "rows the data area gets" budget for any
// view that draws a single panel under the screen chrome (table, board lane,
// entity grid, settings entity). It subtracts the live chrome — measured
// rather than hard-coded, so changes in the header / nav / sub strip update
// the budget automatically — from the terminal height.
//
// `panelChrome` is the rows the panel itself owns (border + kicker +
// separator + any trailing hint), so the returned number is exactly what
// the data window should use. Returns 0 on tiny
// terminals so callers fall back to "render everything and let
// header.Stack charge the middle slot" — never returning a negative budget.
func (m Model) panelViewportRows(panelChrome int) int {
	return m.framedScreenKit().PanelViewportRows(panelChrome)
}

// hostChromeRows measures the terminal rows the root chrome occupies above any
// screen body: the screen header (nav strip, plus the sub strip when the
// active top has one) and the status badge when one is showing. Screens cannot
// compute it — the chrome is root-owned — so it travels to them on the frame.
func (m Model) hostChromeRows() int {
	screenHeader := strings.Count(m.renderHeader(), "\n") + 1
	statusLine := 0
	if m.status != "" {
		statusLine = 2 // separator newline + the status badge
	}
	return screenHeader + statusLine
}
