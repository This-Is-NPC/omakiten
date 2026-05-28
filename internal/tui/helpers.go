package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/gridtable"
)

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func truncateText(s string, max int) string {
	runes := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// wrapWords breaks s into lines where the first line is constrained to firstWidth
// and subsequent lines to restWidth. It tries to keep whole words.
func wrapWords(s string, firstWidth, restWidth int) []string {
	if s == "" {
		return []string{""}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		limit := firstWidth
		if len(lines) > 0 {
			limit = restWidth
		}
		if lipgloss.Width(current+" "+word) <= limit {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	lines = append(lines, current)
	return lines
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
