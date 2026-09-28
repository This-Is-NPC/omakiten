// Package lane paints one kanban column: a bordered box, a kicker, a rule,
// then a Cards body or the empty-lane line.
//
// It is an organism over list.Cards and panel.Frame — not a domain type.
// Screens still paint each card (via card.Painter) and style the header;
// this package only joins what it is handed. It holds no config or domain.
package lane

import (
	"strings"

	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenkit"
)

// Spec is one lane: the chrome around a stack of already-painted cards.
type Spec struct {
	// Header is the already-styled kicker, typically `// BACKLOG · N`.
	Header string
	// Inner is the content width handed to panel.Frame — the same number the
	// board uses for columnInner. The border sits outside it.
	Inner int
	// Height is the box's content height (header + rule + body). Zero keeps
	// the content-sized default.
	Height int
	// EmptyText replaces the body when Cards is empty. Styled with
	// Styles.Empty (never Hint) so a vacant lane stays centred. Blank draws
	// no body row.
	EmptyText string
	// Cards is the stacked body. The caller already set items, viewport,
	// cursor and WithText; this package does not own scroll state.
	Cards list.Cards
}

// Render draws one bordered lane: header, rule, then the Cards body or the
// empty-lane line. Empty lists draw EmptyText (Styles.Empty, never Hint) and
// skip View; a blank View is not a body row.
func Render(kit screenkit.Kit, spec Spec) string {
	var cards []string
	if spec.Cards.Len() > 0 {
		if body := spec.Cards.View(kit.Styles.Hint); body != "" {
			cards = []string{body}
		}
	}
	frame := Paint(kit, PaintSpec{
		Header:    spec.Header,
		Inner:     spec.Inner,
		EmptyText: spec.EmptyText,
		Cards:     cards,
	})
	lines := make([]string, 0, len(frame.Header)+len(frame.Items)+len(frame.Footer))
	lines = append(lines, frame.Header...)
	lines = append(lines, frame.Items...)
	lines = append(lines, frame.Footer...)
	return strings.Join(lines, "\n")
}

// PaintSpec is one lane whose WINDOW belongs to an arranger.
//
// It is [Spec] less the two things an arranger owns and a lane must not hold a
// second copy of: the scroll model and the box height. The cards arrive already
// painted, one string per card, because one card is one cursor position and the
// arranger steps cards rather than terminal rows.
type PaintSpec struct {
	// Header is the already-styled kicker, the same contract as Spec.Header.
	Header string
	// Inner is the content width. The border sits outside it.
	Inner int
	// EmptyText replaces the body when Cards is empty, styled with Styles.Empty
	// exactly as Render styles it. Blank draws no body row.
	EmptyText string
	// Cards are the painted cards, one entry per card, each any number of rows.
	Cards []string
}

// Frame is a painted lane split so an arranger can window the cards while the
// top border, the kicker and the rule stay pinned above them and the bottom
// border stays pinned below.
//
// The three slices are [screenlayout.Block]'s three fields, in its order, and
// Chrome is its hook: hand it to Block.Chrome and the scroll hints the arranger
// injects into the box get the lane's sides instead of landing bare between
// them.
type Frame struct {
	Header []string
	Items  []string
	Footer []string
	// Chrome boxes one row the arranger injected. It is WrapLine, so the sides
	// and the border colour are the ones every other row of this lane already has.
	Chrome func(string) string
}

// Paint draws the lane through the same Box as Render and returns it split.
// There is no Height: the rows are the arranger's, and a lane that sized itself
// would be that number's second author.
func Paint(kit screenkit.Kit, spec PaintSpec) Frame {
	header, footer, wrap := panel.Frame(kit.Styles.Border, spec.Inner, spec.Header)
	body := spec.Cards
	if len(body) == 0 && spec.EmptyText != "" {
		body = []string{kit.Styles.Empty.Width(spec.Inner).Render(spec.EmptyText)}
	}
	items := make([]string, len(body))
	for i, card := range body {
		items[i] = wrap(card)
	}
	return Frame{
		Header: header,
		Items:  items,
		Footer: footer,
		Chrome: panel.WrapLine(kit.Styles.Border, spec.Inner),
	}
}
