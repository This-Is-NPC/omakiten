package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/components/gridtable"
)

// sanitizeTerminalText neutralises terminal escape and control sequences in
// strings sourced from the events log (task titles, MCP-supplied agent_model
// ids, guard rule/tag pairs, payload-derived bucket keys) before they reach
// the operator's terminal. These values are attacker-influenceable through
// the MCP surface, and truncateText's ansi.StringWidth counts escape
// sequences as zero-width — an escape-laden string would pass the width
// check verbatim and inject OSC/CSI into the TUI.
//
// ansi.Strip removes only ESC(0x1b)-introduced sequences; the rune filter
// must therefore drop every Cc control — C0 (<0x20), DEL (0x7f), AND the C1
// block (0x80–0x9F), whose U+009B/U+009D act as 8-bit CSI/OSC introducers on
// C1-honoring terminals. unicode.IsControl covers exactly that set.
func sanitizeTerminalText(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// truncateText caps s to a max VISIBLE cell budget, not a rune count.
// A CJK ideograph or emoji occupies two terminal cells, so the prior
// rune-count cut let a string with wide glyphs render at up to twice its
// budget and tip past the panel edge. Width is measured with
// ansi.StringWidth (display cells) and the cut walks runes while tracking
// accumulated cell width, reserving one cell for the trailing ellipsis so
// the result never exceeds max cells.
func truncateText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= max {
		return s
	}
	// Reserve one cell for the ellipsis; accumulate runes until adding the
	// next would push the visible width past the remaining budget.
	budget := max - 1
	width := 0
	var b strings.Builder
	for _, r := range s {
		rw := ansi.StringWidth(string(r))
		if width+rw > budget {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	return b.String() + "…"
}

// wrapWords breaks s into lines where the first line is constrained to firstWidth
// and subsequent lines to restWidth. It tries to keep whole words, but a single
// word wider than the active limit is HARD-WRAPPED by visible cell width so an
// unbroken token (a long URL, a path with no spaces, a run of CJK) can never
// produce a line that overflows the column. Widths are measured in display cells
// (lipgloss.Width) so wide glyphs are split at the correct cell boundary.
func wrapWords(s string, firstWidth, restWidth int) []string {
	if s == "" {
		return []string{""}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := ""
	limitFor := func() int {
		if len(lines) > 0 {
			return restWidth
		}
		return firstWidth
	}
	flush := func() {
		lines = append(lines, current)
		current = ""
	}
	for _, word := range words {
		limit := limitFor()
		// A word that cannot fit on a line of its own gets hard-wrapped into
		// fragments before the normal packing resumes. Fragments are sized to
		// the active limit (firstWidth on the first line, restWidth after) so
		// each emitted line stays within its column budget.
		if lipgloss.Width(word) > limit {
			if current != "" {
				flush()
			}
			// `limit` was captured BEFORE the flush above, so it still holds
			// the width of the line this fragment run STARTS on (firstWidth
			// when this token opens a fresh first line, restWidth otherwise).
			// Re-reading limitFor() here would always yield restWidth once the
			// flush pushed a line, under-filling firstWidth. The first fragment
			// fills `limit`; every fragment after it lands on a subsequent line
			// so it is sized to restWidth.
			frags := hardWrapToken(word, limit)
			current = frags[0]
			flush()
			if rest := strings.Join(frags[1:], ""); rest != "" {
				for _, frag := range hardWrapToken(rest, restWidth) {
					current = frag
					flush()
				}
			}
			continue
		}
		if current == "" {
			current = word
			continue
		}
		if lipgloss.Width(current+" "+word) <= limit {
			current += " " + word
		} else {
			flush()
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// hardWrapToken splits a single unbroken token into fragments each at most
// width visible cells wide, cutting on cell boundaries so a wide glyph is
// never split across two lines. Used by wrapWords to contain overlong tokens.
func hardWrapToken(token string, width int) []string {
	if width < 1 {
		width = 1
	}
	if lipgloss.Width(token) <= width {
		return []string{token}
	}
	var frags []string
	var b strings.Builder
	cur := 0
	for _, r := range token {
		rw := lipgloss.Width(string(r))
		if cur+rw > width && cur > 0 {
			frags = append(frags, b.String())
			b.Reset()
			cur = 0
		}
		b.WriteRune(r)
		cur += rw
	}
	if b.Len() > 0 {
		frags = append(frags, b.String())
	}
	return frags
}

func renderFixedBox(lines []string, width int, border lipgloss.Style) string {
	rows := []string{border.Render("┌" + strings.Repeat("─", width) + "┐")}
	for _, line := range lines {
		rows = append(rows, border.Render("│")+gridtable.PadLine(line, width)+border.Render("│"))
	}
	rows = append(rows, border.Render("└"+strings.Repeat("─", width)+"┘"))
	return strings.Join(rows, "\n")
}

func clampInt(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func indentBlock(block string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// renderPanel wraps any rendered body in the canonical panel chrome —
// leading newline (so the panel sits one row below the screen header),
// `m.styles.panel` border, and 2-space indent. Every render_*.go that
// drew its own surface used to inline this exact assembly; collapsing
// to a single call keeps the leading-blank / indent / border contract
// in one place so future tweaks (e.g. changing the indent) land here.
func (m Model) renderPanel(content string) string {
	return "\n" + indentBlock(m.styles.panel.Render(content), 2)
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

// hRule renders a horizontal rule of `width` columns in the separator
// style — the kicker/separator/body sandwich every panel uses. Pulled
// out so the `strings.Repeat("─", n)` literal stops appearing in render
// files; the rule glyph is now declared once.
func (m Model) hRule(width int) string {
	return m.styles.separator.Render(strings.Repeat("─", width))
}

// cursorMarker returns the accent-styled selectionMarker when `selected`
// is true, otherwise the neutral normalMarker. Pulled out so the
// four-line "marker := normalMarker / if cursor == idx ..." boilerplate
// stops repeating across every list / picker render. The comparison
// stays at the callsite because cursor/index can be int (slice index),
// int64 (domain id), or any other comparable — keeping the bool decision
// outside means no generics or type-switching here.
func (m Model) cursorMarker(selected bool) string {
	if selected {
		return m.styles.marker.Render(selectionMarker)
	}
	return normalMarker
}

// cursorChevron returns "› " accent-styled when `selected`, else the
// empty string. Used by card- and table-style surfaces whose cursor
// is the chevron glyph (board task cards, plan network rows). Caller
// pads with two spaces when the surface keeps a fixed cursor column
// regardless of selection state (table rows); card surfaces leave
// the unselected case empty so the title gets the freed width.
func (m Model) cursorChevron(selected bool) string {
	if !selected {
		return ""
	}
	return m.styles.marker.Render("›") + " "
}

// renderPickerPanel is the canonical assembly for any "kicker + hint +
// optional meta + horizontal rule + scrollable list, all wrapped in
// the standard panel" surface. The picker shape — used by the persona /
// theme / config / template-default / blocker pickers — is now declared
// once: callers build only the variant prefix (kicker, hint, optional
// metaRow lines) and pass dataRows + the picker's scroll/viewport state.
//
// The `header` slice is taken verbatim, then the rule, the scroll-clipped
// data rows, and the panel chrome are appended. Future tweaks ("every
// picker gets a count badge", "rule glyph changes", "panel chrome adds
// a footer") land here instead of in five render files.
func (m Model) renderPickerPanel(header, dataRows []string, scroll, viewport int) string {
	lines := make([]string, 0, len(header)+len(dataRows)+1)
	lines = append(lines, header...)
	lines = append(lines, m.hRule(m.availableWidth()-4))
	lines = append(lines, m.sliceScrollRows(dataRows, scroll, viewport)...)
	return m.renderPanel(strings.Join(lines, "\n"))
}
