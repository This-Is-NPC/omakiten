// Package framed turns panel's bordered-section algorithm into a ready
// screenlayout.Block, so a screen stops re-deriving "inner width, wrap every
// item, pad to the row budget" every time it paints a kickered box.
//
// Task Detail's zone frame and Studio's inspector box and framed list each
// grew their own copy of that arithmetic — one of them re-painting the
// ┌─┐ / ├─┤ / └─┘ glyphs panel already owns. This package is the one place
// that turns panel's primitives into a Block; screens hand it a border style,
// a width, a kicker and the raw items, and get back Header, Items, Footer
// and Chrome already wrapped.
//
// It is a leaf: panel and screenlayout in, nothing from tui or any screen.
// panel does not import screenlayout and screenlayout does not import panel,
// so this package is the one place the two meet.
package framed

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenlayout"
)

// Rows is the constant chrome row cost of a framed Box: the top border, the
// wrapped kicker, the joining rule and the bottom border — the shape
// panel.Frame always produces. Callers that must budget a zone before
// painting it (Task Detail sizes its subtask board against the rows left
// after this chrome) read the number here rather than re-deriving it.
const Rows = panel.Borders + 2

// Box paints a bordered, kickered section as a ready screenlayout.Block: the
// top edge, the wrapped kicker, the joining rule, the wrapped items, and the
// bottom edge. width is the section's total column budget, including the
// two side borders; the inner width items wrap to is derived from it exactly
// as panel.Frame derives it.
//
// Chrome on the returned Block is the same side-wrapper every item was
// wrapped through, so hint rows the arranger injects for a scrolling
// section land inside the same box.
func Box(border lipgloss.Style, width int, kicker string, items []string) screenlayout.Block {
	inner := width - panel.Borders
	header, footer, wrap := panel.Frame(border, inner, kicker)
	wrapped := make([]string, len(items))
	for i, item := range items {
		wrapped[i] = wrap(item)
	}
	return screenlayout.Block{Header: header, Items: wrapped, Footer: footer, Chrome: wrap}
}

// List is [Box] for a scrollable section whose header carries one or more
// pinned lines below the kicker's joining rule — a column heading, a hint
// line — before the scrolled rows start. Every list-style screen (Table,
// Plans, the two option pickers, Logs, Insights, Project Resume, Studio's
// inspector box) rebuilt the same shape: derive the inner width, call
// panel.Frame, CapRows the rows, wrap every row and every extra header line
// by hand, then assemble the Block literal. List is that shape, once.
//
// columnHeader lines are appended to Header, wrapped the same way the
// kicker's own line is, so a scroll never carries them off with the rows
// they name. rows are handed to [Box] untouched: [Box]'s own wrap already
// truncates each one to the box's inner width, so a caller that used to
// CapRows its rows before wrapping was paying for the same clamp twice.
func List(border lipgloss.Style, width int, kicker string, columnHeader []string, rows []string) screenlayout.Block {
	block := Box(border, width, kicker, rows)
	for _, line := range columnHeader {
		block.Header = append(block.Header, block.Chrome(line))
	}
	return block
}

// Document splits an already fully-painted string — a panel, a bordered
// gridtable, any other component that painted its own chrome — into the
// lines a static, non-selectable Block hands the arranger, dropping exactly
// the document's own trailing newline.
//
// Project, Plan Network, Comment Detail and Task Detail each mounted one
// already-painted document this way: `strings.Split(strings.TrimSuffix(view,
// "\n"), "\n")`, paired with `Cursor: screenlayout.NoSelection()` so a
// section shared with a cursor-bearing sibling mode cannot inherit a stale
// selection. Document is that split, once; callers still state the
// NoSelection cursor themselves; whether the result belongs in Header or
// Items is the caller's own layout, not this package's.
func Document(view string) []string {
	return strings.Split(strings.TrimSuffix(view, "\n"), "\n")
}

// Fill pads block with blank inner rows, wrapped through its own Chrome, so
// the bottom border lands on exactly rows rather than short of it. Padding
// is appended to the last item when the block already has items — extra
// footer rows would shrink the item viewport instead — and to the footer
// when it has none.
//
// block.Chrome is the wrapper Fill pads with, so a block built by [Box] pads
// itself; a block with no Chrome is returned unpadded, since there is
// nothing to build the blank row with.
func Fill(block screenlayout.Block, rows int) screenlayout.Block {
	wrap := block.Chrome
	if wrap == nil {
		return block
	}
	used := len(block.Header) + len(block.Footer)
	for _, item := range block.Items {
		used += strings.Count(item, "\n") + 1
	}
	need := rows - used
	if need <= 0 {
		return block
	}
	blank := wrap("")
	if len(block.Items) == 0 {
		blanks := make([]string, need)
		for i := range blanks {
			blanks[i] = blank
		}
		block.Footer = append(blanks, block.Footer...)
		return block
	}
	pad := strings.Repeat("\n"+blank, need)
	block.Items[len(block.Items)-1] += pad
	return block
}
