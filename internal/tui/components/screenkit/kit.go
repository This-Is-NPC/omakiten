// Package screenkit is the rendering toolkit an extracted TUI screen paints
// with. It carries the theme projection a screen may see plus the shared panel,
// rule, cursor, scroll-window and viewport-budget algorithms that used to live
// as methods on the root Model.
//
// The package exists so root and every extracted screen share exactly ONE
// implementation of each of those algorithms: the root methods (renderPanel,
// hRule, cursorMarker, summaryRows, renderSummaryTables, renderScrollWindowSplit,
// sliceScrollRows, panelViewportRows, availableWidth) are thin delegations onto
// a Kit, so a change to panel chrome or scroll hinting can only be made in one
// place and lands on both sides at once.
//
// screenkit knows nothing about navigation, data services or the root Model —
// it is a leaf next to the other components packages.
//
// # Width contract
//
// Kit reports geometry honestly. AvailableWidth is a measurement of the
// terminal, never a render budget: it never exceeds Kit.Width and never returns
// a negative number, so a consumer that compares it against a required minimum
// (the layout engine picking stacked versus side-by-side) is never handed an
// inflated figure. It carries no minimum of its own, because no single minimum
// fits every surface — a screen that cannot paint below some width clamps at
// its own callsite, where the number's meaning is visible.
//
// Two primitives absorb the constraint themselves, because in both cases the
// behaviour is a property of the glyph rather than of a screen's layout policy:
//
//   - HRule absorbs a non-positive width by rendering nothing, rather than
//     letting strings.Repeat panic at six separate subtraction sites.
//   - PanelBox (and Panel, which indents it) clamps its border to
//     AvailableWidth when — and only when — the body it is given is wider. A
//     bordered style with no Width sizes itself to its widest line, so one long
//     line would otherwise carry the whole box off the right edge.
//
// Neither is a budget a screen has to remember to apply; both are the primitive
// refusing to paint outside the terminal it was measured against.
package screenkit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/components/scrollwindow"
)

// selectionMarker / normalMarker are the cursor glyphs every list surface
// paints its selected row with.
const (
	selectionMarker = "▌"
	normalMarker    = " "
)

// Kit is the per-frame rendering context handed to a screen. It is a value:
// the host rebuilds it on every frame from live geometry and the active theme,
// so a screen never retains stale width, height or colours.
type Kit struct {
	// Styles is the theme projection the screen paints with.
	Styles Styles
	// Markdown is the four colour tokens the markdown renderer needs. DATA from
	// the theme, not a paint callback: a screen holds a markdown.Renderer built
	// from these (markdown.New) and paints the body itself. Theme rotation is
	// Renderer.Reload, not a process-wide cache slot.
	Markdown MarkdownTokens
	// Text resolves an i18n catalog key. Nil-safe: T returns the key itself
	// when no resolver is wired, which keeps headless tests readable.
	Text func(string) string
	// Width and Height are the raw terminal geometry.
	Width  int
	Height int
	// ChromeRows is the number of terminal rows the host chrome occupies
	// ABOVE the screen body — the screen header (nav + sub strip) plus the
	// status badge when one is showing. A screen cannot measure it (the
	// chrome is host-owned), so the host supplies it and the viewport-budget
	// helpers subtract it.
	ChromeRows int
}

// MarkdownTokens is the slim subset of theme colours the markdown renderer
// needs. Flat strings rather than a theme map so token equality is trivial
// (Reload is a no-op when they match), and so neither this package nor
// components/markdown imports config.
type MarkdownTokens struct {
	ThemeKey    string
	Foreground  string
	Border      string
	Primary     string
	Secondary   string
	ImageFormat string // catalog-resolved glamour image label; empty uses English fallback
}

// T resolves an i18n key, degrading to the key itself when no catalog is wired.
func (k Kit) T(key string) string {
	if k.Text == nil {
		return key
	}
	// Catalog values are configuration data, not framework styling. Insert a
	// string-control terminator before percent markers so an unterminated OSC
	// cannot swallow printf verbs before callers format the translation.
	text := k.Text(key)
	if !hasControlExceptLF(text) {
		return text
	}
	if strings.Contains(text, "%") {
		text = strings.ReplaceAll(text, "%", "\x9c%")
	}
	return SanitizeMultiline(text)
}

// gutters is the number of terminal columns reserved for host chrome around a
// screen body — the 2-cell indent Panel applies plus the matching allowance on
// the right. Named so the subtraction stops reading as a bare 4.
const gutters = 4

// unmeasuredWidth is the terminal width assumed before the host has received
// its first WindowSizeMsg, so a headless render still produces a sane layout.
const unmeasuredWidth = 120

// unmeasuredHeight is the same assumption on the other axis. It existed only
// for width until #2425, and the asymmetry had a consequence: a screen that
// budgets its body against Height reported ZERO rows before the first
// WindowSizeMsg and painted nothing at all, while the same screen's width came
// back as a usable 120. Screens hid it by rendering unclipped whenever their
// budget came back zero — which is the "0 means unlimited" sentinel the row
// budget work has been removing everywhere else.
const unmeasuredHeight = 40

// Rows is the terminal rows the host has, with the unmeasured assumption
// applied — the height-axis twin of AvailableWidth.
//
// A caller that needs to tell "the terminal is tiny" from "the terminal has not
// been measured" reads this rather than Height: one to four rows is tiny, zero
// is unmeasured, and conflating them is what made a headless body empty.
func (k Kit) Rows() int {
	if k.Height <= 0 {
		return unmeasuredHeight
	}
	return k.Height
}

// AvailableWidth is the content width a screen body may consume: the terminal
// width minus the chrome gutters, with a 120-cell assumption for an unmeasured
// terminal.
//
// CONTRACT — this is an honest measurement, not a render budget. The returned
// value never exceeds k.Width and is never negative: on a 20-column terminal it
// reports 16, and on a 3-column terminal it reports 0. It used to floor at 24,
// which over-reported a narrow terminal by up to 4 columns; the layout engine
// decides stacked-versus-side-by-side by comparing this number against the sum
// of section minimum widths, and an inflated input would bake an overflow into
// every breakpoint built on it.
//
// A caller that cannot paint below some minimum owns that minimum itself. There
// was never one floor to share — the detail screens clamp at 24, entitylist at
// 30, the hint boxes at 32, entitydetail at 20 — so the clamp belongs at the
// callsite where the number's meaning is visible, not hidden in this accessor.
func (k Kit) AvailableWidth() int {
	width := k.Width
	if width <= 0 {
		width = unmeasuredWidth
	}
	if width < gutters {
		return 0
	}
	return width - gutters
}

// Panel wraps a rendered body in the canonical panel chrome — leading newline
// (so the panel sits one row below the screen header), the panel border, and a
// 2-space indent. Keeping the leading-blank / indent / border contract in one
// place means a future tweak lands on every surface at once.
//
// The chrome is owned here: leading newline, the one-sided width clamp
// (PanelBox), then a panelIndent-space indent.
func (k Kit) Panel(content string) string {
	return "\n" + Indent(k.PanelBox(content), panelIndent)
}

// panelIndent is the columns Panel indents its box by — and therefore exactly
// the columns the box itself may not use. It is a named constant rather than a
// literal in two places because the indent and the width budget have to be the
// same number: an indent that grew without the budget shrinking would push the
// box back off the edge this clamp exists to keep it inside.
const panelIndent = 2

// BoxWidth is the widest a bordered box may render once a screen has indented
// it by the standard body indent: the terminal minus that indent.
//
// It is the budget for the BOX — the border included — which is what makes it
// the right number for any surface that renders a framed thing and indents it,
// whether the frame is Panel's border, a form's, or a prompt input's.
//
// This is deliberately NOT AvailableWidth. AvailableWidth subtracts a gutter on
// both sides because it is the budget for a screen's CONTENT, which sits inside
// a border and its padding; the box is the thing the indent is applied to, and
// it is allowed to run to the last column. Every panel in the tree is already
// sized this way — a screen computes its content at AvailableWidth()-4 and the
// border and padding spend the remaining 6, landing the box on the terminal's
// right edge exactly. Budgeting a box at AvailableWidth instead would narrow
// every panel that already fits by two columns, which is a layout change on
// every surface rather than a fix to the ones that overflow.
func (k Kit) BoxWidth() int {
	width := k.Width
	if width <= 0 {
		width = unmeasuredWidth
	}
	return max(0, width-panelIndent)
}

// PanelContentWidth is the widest a LINE inside a panel may be before the box
// has to wrap it: the box budget less the border and padding the box spends on
// itself.
//
// It is the single number three separate obligations are derived from, which is
// why it is a method and not three subtractions:
//
//   - PanelBox clamps when its body is wider than this.
//   - Chrome.Lines charges a chrome block the rows it will occupy AFTER that
//     clamp wraps it, so a wrapped hint cannot cost a row nobody paid for.
//   - A section handed this as its screenlayout.Box.Width caps its own
//     single-line rows to it (CapRows), so the rows the arranger's scroll
//     window counts stay single-line once the box has had them.
//
// Measured from the live Panel style rather than assumed, so a theme that
// changes the border or the padding moves all three at once.
func (k Kit) PanelContentWidth() int {
	return max(0, k.BoxWidth()-k.Styles.Panel.GetHorizontalFrameSize())
}

// PanelBox is the bordered box Panel indents: the same border, the same width
// clamp, without the leading blank row or the 2-space indent.
//
// It exists for the surfaces that compose the panel into a larger body instead
// of being the body — the Logs inspector stacks a chip strip and two summary
// tables above its panel and indents the whole stack once. Those surfaces used
// to reach past Kit for `Styles.Panel.Render` directly, which meant they were
// re-implementing the box contract and silently opted out of every fix made to
// it. Going through PanelBox keeps one author for the border and its clamp.
//
// # The clamp
//
// A lipgloss border with no Width sizes itself to the widest line it is given,
// so one long line — a footer note, an untranslated hint, an unwrapped picker
// row — drags the whole box past the right edge and takes every other row's
// border with it. The terminal then cuts the surplus, which is why the box
// looks broken rather than merely wide.
//
// The clamp is deliberately ONE-SIDED. lipgloss Width() is a minimum as well as
// a maximum: setting it unconditionally would pad every panel in the tree out
// to the full terminal, which is a layout change on every surface rather than a
// fix to the eight that overflow. So the Width is applied only when the content
// actually exceeds the budget, and a body that already fits renders the exact
// bytes it rendered before.
//
// Overflowing content is WRAPPED, not truncated. lipgloss soft-wraps at spaces
// and hard-breaks a token with no break opportunity, both ANSI-aware and both
// measured in display cells, so a URL, a CJK title and a styled run all land
// inside the border with nothing dropped. Truncating would trade an overflow
// the user can see for content the user cannot reach.
func (k Kit) PanelBox(content string) string {
	style := k.Styles.Panel
	// lipgloss Width() counts the content plus horizontal padding but not the
	// border, so the border is what comes off the box budget to set it.
	// Whether the clamp is needed at all is decided against PanelContentWidth,
	// which is that budget less the whole frame — what the content itself has
	// to fit inside.
	boxWidth, contentWidth := k.BoxWidth(), k.PanelContentWidth()
	if budget := boxWidth - style.GetHorizontalBorderSize(); budget > 0 && lipgloss.Width(content) > contentWidth {
		style = style.Width(budget)
	}
	return style.Render(content)
}

// WrapBody folds a block that NO box will wrap for it — a note, a hint, a
// footer line a screen indents straight into its body — to the width such a
// block may paint at.
//
// Panel and PanelBox clamp what they frame, but a screen is free to indent a
// line without framing it, and two do: the Settings general hint and the Plan
// Network dependency footer are both appended to their body as bare indented
// text. Neither is inside a border, so neither was reached by the panel clamp,
// and both ran off the right edge at the 80-column floor — the Settings hint at
// 102 cells, the Plan Network footer at 124 cells, which is past 120 as well.
//
// The wrap is ANSI-aware and measured in display cells, and it does NOT pad the
// short lines: a bare block has no border to square off, so padding it would
// only add trailing whitespace to every body in the tree. A block that already
// fits is returned untouched.
func (k Kit) WrapBody(block string) string {
	return WrapAt(block, k.BoxWidth())
}

// WrapAt is WrapBody at an explicit width, for a caller that frames a block at
// a width Kit does not own — a layout engine handing one column of a
// side-by-side arrangement its share of the terminal, say.
//
// Split out so there is one implementation of "fold this block to N columns
// without padding it". A second hand-rolled copy is how the width axis grew six
// independent `AvailableWidth() - chrome` derivations; the algorithm has one
// author here and every caller measures what this produces.
func WrapAt(block string, width int) string {
	if width <= 0 {
		return block
	}
	lines := strings.Split(block, "\n")
	wrapped := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		if lipgloss.Width(line) <= width {
			wrapped = append(wrapped, line)
			continue
		}
		changed = true
		wrapped = append(wrapped, strings.Split(ansi.Wrap(line, width, " "), "\n")...)
	}
	if !changed {
		return block
	}
	return strings.Join(wrapped, "\n")
}

// HRule renders a horizontal rule of `width` columns in the separator style —
// the kicker/separator/body sandwich every panel uses.
//
// A non-positive width renders nothing. Six surfaces derive their rule width by
// subtracting panel chrome from AvailableWidth, and on a terminal too narrow to
// hold that chrome the subtraction goes negative — which strings.Repeat panics
// on. The guard lives here, in the one place that owns the glyph, rather than
// as six copies of the same max() at the subtraction sites.
func (k Kit) HRule(width int) string {
	if width <= 0 {
		return ""
	}
	return k.Styles.Separator.Render(strings.Repeat("─", width))
}

// CursorMarker returns the accent-styled selection glyph when `selected`,
// otherwise the neutral spacer. The comparison stays at the callsite because
// cursor/index can be a slice index, a domain id, or any other comparable.
func (k Kit) CursorMarker(selected bool) string {
	if selected {
		return k.Styles.Marker.Render(selectionMarker)
	}
	return normalMarker
}

// ScrollWindowSplit assembles a scrollable list with separate "▲ N above" /
// "▼ N below" hint rows. heights[i] is item i's terminal-row count; items[i] is
// the pre-rendered string for that item. The slice math is delegated to
// scrollwindow.Slice so styling, wording and reservation can never drift across
// surfaces.
//
// Returns the full content as-is when nothing is hidden in either direction —
// callers don't need to special-case the no-scroll path.
func (k Kit) ScrollWindowSplit(items []string, heights []int, offset, viewport int) []string {
	if len(items) == 0 || len(items) != len(heights) {
		return items
	}
	if viewport <= 0 {
		return items
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(items)-1 {
		offset = len(items) - 1
	}
	end := scrollwindow.Slice(offset, heights, viewport, scrollwindow.HintsSplit)
	if offset == 0 && end == len(items) {
		return items
	}
	out := make([]string, 0, end-offset+2)
	if above := scrollwindow.Above(offset); above > 0 {
		out = append(out, k.Styles.Hint.Render(tr(k.Text, "tui.scroll.above_fmt", "▲ %d above", above)))
	}
	out = append(out, items[offset:end]...)
	if below := scrollwindow.Below(end, len(items)); below > 0 {
		out = append(out, k.Styles.Hint.Render(tr(k.Text, "tui.scroll.below_fmt", "▼ %d below", below)))
	}
	return out
}

// TruncateStyled caps a row that carries ANSI styling, which is what a rendered
// list row always is.
//
// Truncate is NOT usable here. It walks runes and charges each one
// ansi.StringWidth, which reports 1 for the printable ASCII inside an escape
// sequence — so `\x1b[38;2;73;77;100m` is billed as fifteen cells of content and
// a 60-cell budget yields an 18-cell row. Worse, the cut can land inside a
// sequence and emit `\x1b[38;` to the terminal, and it never re-terminates the
// styles still open at the cut, so the colour bleeds across the rest of the
// screen.
//
// None of that is visible in a fixture: the recorder strips ANSI before it
// compares, and a test binary has no TTY so lipgloss emits no escapes at all.
// It only appears in a real terminal, which is why this is measured with the
// ANSI-aware truncation rather than the rune-walking one.
//
// Truncate keeps its callers — every one of them passes plain, sanitized text —
// but a styled row belongs here.
func TruncateStyled(row string, width int) string {
	return ansi.Truncate(row, width, "…")
}

// CapRows truncates every line of every row to width. Used by screenlayout after
// ScrollWindowSplit so "▲ N above" / "▼ N below" cannot overflow a section
// narrower than the untruncated hint.
func CapRows(rows []string, width int) []string {
	if width <= 0 || len(rows) == 0 {
		return rows
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = capBlock(row, width)
	}
	return out
}

func capBlock(row string, width int) string {
	lines := strings.Split(row, "\n")
	for j, line := range lines {
		if lipgloss.Width(line) > width {
			lines[j] = TruncateStyled(line, width)
		}
	}
	return strings.Join(lines, "\n")
}

// PanelViewportRows is the canonical "rows the data area gets" budget for any
// view that draws a single panel under the screen chrome. It subtracts the live
// host chrome (ChromeRows, measured by the host rather than hard-coded) from
// the terminal height.
//
// `panelChrome` is the rows the panel itself owns (border + kicker + separator
// + any trailing hint), so the returned number is exactly the data window a
// caller should scroll against. Returns 0 on tiny terminals so callers fall
// back to "render everything and let the host clamp" — never a negative budget.
func (k Kit) PanelViewportRows(panelChrome int) int {
	return k.ViewportRows(panelChrome)
}

// ViewportRows is the general form of PanelViewportRows: `bodyChrome` is every
// row consumed between the host chrome and the scrollable data window, for a
// surface whose chrome is not a single panel (the Logs inspector stacks a chip
// strip and two summary tables above its panel and measures them live).
func (k Kit) ViewportRows(bodyChrome int) int {
	if k.Height <= 0 {
		return 0
	}
	const (
		leadingBlank = 1 // "\n" prepended by every screen body
		footerLines  = 2 // newline + indented keybinding row
	)
	rows := k.Height - (k.ChromeRows + leadingBlank + bodyChrome + footerLines)
	if rows < 4 {
		return 0
	}
	return rows
}

// ScrollDataRows is the adapter between a section's full item viewport and the
// data-row window a cursor-tracking helper should target: the renderer
// reserves up to 2 rows for the "▲ above" / "▼ below" hints. Returns at least 1
// so cursor follow stays responsive on tiny terminals instead of locking up at
// zero.
func ScrollDataRows(viewport int) int {
	const reservedHints = 2
	if viewport <= reservedHints {
		return 1
	}
	return viewport - reservedHints
}

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
