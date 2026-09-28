package tokenstrip

import (
	"strings"

	"omakiten/internal/tui/components/screenkit"
)

// Policy is what a strip does when its tokens do not fit the width.
type Policy int

const (
	// Wrap continues onto another row. Nothing is lost; the strip gets taller.
	// This is the pill line on a card and the key footer.
	Wrap Policy = iota
	// Drop keeps one row and removes tokens until it fits, farthest from
	// [Options.Keep] first. This is a choice row: a chip you cannot see is
	// better than a strip that runs off the panel, and the chip you are ON must
	// survive.
	Drop
)

// Options is everything about a strip that is not the tokens themselves.
type Options struct {
	// Sep goes between tokens. Empty means a single space.
	Sep string
	// Width is the column budget. Zero means unbounded, and no policy runs.
	Width int
	// Policy is what happens when the tokens do not fit. Ignored at Width 0.
	Policy Policy

	// BandGap puts a blank row between wrapped bands.
	//
	// Pills carry a background, so two bands stacked directly read as one block
	// of colour rather than as two rows of pills. A single band never pays for
	// it: one line of badges costs one row, which is what a card budgets.
	BandGap bool
	// Align positions each wrapped row inside Width. Empty means left.
	Align string

	// Lead is painted before the first token and is never dropped — the strip's
	// kicker, which is how a reader knows what the row is a choice OF.
	Lead string
	// Trail is painted after the last token and is dropped FIRST under Drop:
	// it is advisory, so it is the cheapest thing to lose.
	Trail string
	// Keep is the token index that must survive Drop. Negative means none, and
	// then dropping walks from the right.
	Keep int
}

const (
	// AlignLeft is the default; AlignCenter and AlignRight pad a wrapped row
	// inside the budget.
	AlignLeft   = "left"
	AlignCenter = "center"
	AlignRight  = "right"
)

// Render packs tokens and paints them.
func Render(tokens []string, opts Options) string {
	return pack(tokens, opts)
}

// Height is the rows Render would occupy, blank band rows included.
//
// It runs the same pack rather than predicting it. The board asks for this once
// per card per keystroke and the answer has to be the number the paint produces,
// not a number derived alongside it.
func Height(tokens []string, opts Options) int {
	packed := pack(tokens, opts)
	if packed == "" {
		return 0
	}
	return screenkit.BlockRows(packed)
}

func pack(tokens []string, opts Options) string {
	tokens = nonEmpty(tokens)
	if opts.Sep == "" {
		opts.Sep = " "
	}
	if len(tokens) == 0 && opts.Lead == "" && opts.Trail == "" {
		return ""
	}
	if opts.Policy == Drop {
		return dropToFit(tokens, opts)
	}
	return wrapToFit(tokens, opts)
}

// wrapToFit is the packer: greedy, in order, one row at a time.
//
// A token wider than the budget takes a row of its own rather than being cut,
// because a clipped token reads as a different token — a truncated pill is a
// pill with another label on it.
func wrapToFit(tokens []string, opts Options) string {
	head := joinLead(opts)
	if opts.Width <= 0 {
		return head + strings.Join(tokens, opts.Sep) + tailOf(opts)
	}

	sepW := screenkit.VisibleWidth(opts.Sep)
	var rows []string
	var row []string
	rowW := screenkit.VisibleWidth(head)
	if head != "" {
		row = append(row, head)
	}
	for _, tok := range tokens {
		w := screenkit.VisibleWidth(tok)
		add := w
		if len(row) > 0 {
			add += sepW
		}
		if len(row) > 0 && rowW+add > opts.Width {
			rows = append(rows, strings.Join(row, opts.Sep))
			row, rowW = nil, 0
			add = w
		}
		row = append(row, tok)
		rowW += add
	}
	if trail := tailOf(opts); trail != "" {
		row = append(row, trail)
	}
	if len(row) > 0 {
		rows = append(rows, strings.Join(row, opts.Sep))
	}
	return assemble(rows, opts)
}

// assemble joins the packed rows, inserting the band gap and aligning each row.
func assemble(rows []string, opts Options) string {
	for i, row := range rows {
		rows[i] = alignRow(row, opts)
	}
	gap := "\n"
	if opts.BandGap && len(rows) > 1 {
		gap = "\n\n"
	}
	return strings.Join(rows, gap)
}

func alignRow(row string, opts Options) string {
	if opts.Width <= 0 || opts.Align == "" || opts.Align == AlignLeft {
		return row
	}
	pad := opts.Width - screenkit.VisibleWidth(row)
	if pad <= 0 {
		return row
	}
	if opts.Align == AlignCenter {
		left := pad / 2
		return strings.Repeat(" ", left) + row + strings.Repeat(" ", pad-left)
	}
	return strings.Repeat(" ", pad) + row
}

// dropToFit keeps one row and removes tokens until it fits.
//
// Order of loss, cheapest first: the trailing hint, then tokens by distance
// from [Options.Keep] (farthest first, right-hand side breaking ties), then the
// kept token itself truncates. The lead never goes — a row of choices with no
// label is a row of unexplained words.
func dropToFit(tokens []string, opts Options) string {
	paint := func(toks []string, trail bool) string {
		parts := make([]string, 0, len(toks)+2)
		if opts.Lead != "" {
			parts = append(parts, opts.Lead)
		}
		parts = append(parts, toks...)
		if trail && opts.Trail != "" {
			parts = append(parts, "  "+opts.Trail)
		}
		return strings.Join(parts, opts.Sep)
	}

	out := paint(tokens, true)
	if opts.Width <= 0 || screenkit.VisibleWidth(out) <= opts.Width {
		return out
	}
	if out = paint(tokens, false); screenkit.VisibleWidth(out) <= opts.Width {
		return out
	}

	live := append([]string(nil), tokens...)
	keep := opts.Keep
	for {
		idx := farthestFrom(live, keep)
		if idx < 0 {
			break
		}
		if idx < keep {
			keep--
		}
		live = append(live[:idx], live[idx+1:]...)
		if out = paint(live, false); screenkit.VisibleWidth(out) <= opts.Width {
			return out
		}
	}
	return truncateLast(paint(live, false), opts)
}

// farthestFrom is the next token to lose: the one furthest from the kept index,
// preferring the right-hand side so a strip shrinks from its tail.
func farthestFrom(tokens []string, keep int) int {
	best, bestDist := -1, -1
	for i := range tokens {
		if i == keep {
			continue
		}
		dist := i
		if keep >= 0 {
			dist = abs(i - keep)
		}
		if dist > bestDist || (dist == bestDist && i > best) {
			best, bestDist = i, dist
		}
	}
	return best
}

// truncateLast is the last resort: everything droppable is gone and the row is
// still too wide, so the surviving token is cut.
func truncateLast(row string, opts Options) string {
	parts := strings.Split(row, opts.Sep)
	target := len(parts) - 1
	if opts.Lead != "" && len(parts) > 1 {
		target = 1
	}
	if target < 0 {
		return row
	}
	prefix := strings.Join(parts[:target], opts.Sep)
	suffix := ""
	if target+1 < len(parts) {
		suffix = opts.Sep + strings.Join(parts[target+1:], opts.Sep)
	}
	room := opts.Width - screenkit.VisibleWidth(prefix) - screenkit.VisibleWidth(suffix)
	if target > 0 {
		room -= screenkit.VisibleWidth(opts.Sep)
	}
	parts[target] = screenkit.Truncate(parts[target], max(room, 1))
	return strings.Join(parts, opts.Sep)
}

func joinLead(opts Options) string {
	if opts.Lead == "" {
		return ""
	}
	return opts.Lead
}

func tailOf(opts Options) string {
	if opts.Trail == "" {
		return ""
	}
	return opts.Trail
}

func nonEmpty(tokens []string) []string {
	out := tokens[:0:0]
	for _, t := range tokens {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
