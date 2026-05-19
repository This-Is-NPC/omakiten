package tui

import "strings"

// columnSpec is the surface-agnostic input to renderColumnFrame.
// Both the kanban board cell and the plan network's per-wave column
// build a spec, then defer the header / rule / scroll-window sandwich
// to one helper so the chrome contract lives in a single place. New
// column surfaces only need to build a spec — they inherit the same
// scrolling behaviour and row sequencing for free.
//
// Caller responsibilities:
//   - Header, Rule, EmptyLine are pre-rendered strings (style applied
//     by the caller). The helper does not know which palette a given
//     surface uses.
//   - Cards are pre-rendered card strings with matching CardHeights
//     (terminal-row count per card). Pre-measuring lets the scroll
//     window slice items without re-rendering them.
//   - ScrollOffset / Viewport drive renderScrollWindowSplit. Pass
//     Viewport <= 0 to skip the slice and dump every card.
type columnSpec struct {
	Header       string
	Rule         string
	EmptyLine    string
	Cards        []string
	CardHeights  []int
	ScrollOffset int
	Viewport     int
}

// renderColumnFrame returns the column body: kicker, separator rule,
// then either the empty-state line or the (optionally scrolled) card
// stack. Cards are passed in pre-rendered so the helper stays
// content-agnostic — board cells inject their bucket cards, plan
// columns inject their wave cards, and any future surface can supply
// whatever cards make sense for it.
func (m Model) renderColumnFrame(spec columnSpec) string {
	rows := []string{spec.Header, spec.Rule}
	if len(spec.Cards) == 0 {
		if spec.EmptyLine != "" {
			rows = append(rows, spec.EmptyLine)
		}
		return strings.Join(rows, "\n")
	}
	if spec.Viewport <= 0 {
		rows = append(rows, spec.Cards...)
		return strings.Join(rows, "\n")
	}
	rows = append(rows, m.renderScrollWindowSplit(spec.Cards, spec.CardHeights, spec.ScrollOffset, spec.Viewport)...)
	return strings.Join(rows, "\n")
}
