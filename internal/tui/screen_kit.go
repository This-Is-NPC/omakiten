package tui

import (
	"omakiten/internal/tui/components/screenkit"
)

// screenKit is the root's rendering toolkit: the theme projection plus live
// geometry and the catalog resolver. Root's own render helpers (renderPanel,
// hRule, cursorMarker, summaryRows, renderSummaryTables, sliceScrollRows,
// availableWidth) all route through it, so root and every extracted screen
// share exactly one implementation of each algorithm.
//
// ChromeRows is intentionally left at zero here: measuring it renders the
// header, and the generic render helpers are called from inside header-adjacent
// code paths. Callers that need a chrome-aware budget use framedScreenKit.
func (m Model) screenKit() screenkit.Kit {
	tokens := tokensFromTheme(m.theme)
	tokens.ImageFormat = m.t("tui.markdown.image_fmt")
	return screenkit.Kit{
		Styles:   m.styles.screenStyles(),
		Markdown: tokens,
		Text:     m.t,
		Width:    m.width,
		Height:   m.height,
	}
}

// framedScreenKit is the screenKit a screen body is rendered against: it adds
// the measured host chrome budget so viewport math inside a screen package can
// subtract the header and status rows it cannot see.
func (m Model) framedScreenKit() screenkit.Kit {
	kit := m.screenKit()
	kit.ChromeRows = m.hostChromeRows()
	return kit
}
