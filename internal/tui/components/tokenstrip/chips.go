package tokenstrip

import (
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// Chip is one labelled choice.
type Chip struct {
	Label  string
	Active bool
}

// ChipStyles are the tones a chip row paints with.
type ChipStyles struct {
	Kicker   lipgloss.Style
	Active   lipgloss.Style
	Inactive lipgloss.Style
	Hint     lipgloss.Style
	Sep      lipgloss.Style
}

// ChipOptions is the chip-specific part of a strip.
type ChipOptions struct {
	Kicker string
	Hint   string
	// Sep is the string between chips. Logs uses a single space; Stats uses a
	// middot. Empty defaults to one space.
	Sep string
	// BracketActive wraps the active chip in "[ label ]" (Logs). Stats leaves
	// the label bare and relies on the accent tone alone.
	BracketActive bool
	Width         int
}

// Chips paints a chip row, dropping to fit.
func Chips(chips []Chip, styles ChipStyles, opts ChipOptions) string {
	tokens, packed := chipStrip(chips, styles, opts)
	return Render(tokens, packed)
}

// chipStrip is the one place a chip becomes a token, so the paint and the
// measurement cannot build the row differently.
func chipStrip(chips []Chip, styles ChipStyles, opts ChipOptions) ([]string, Options) {
	opts.Kicker = screenkit.Sanitize(opts.Kicker)
	opts.Hint = screenkit.Sanitize(opts.Hint)
	opts.Sep = screenkit.Sanitize(opts.Sep)
	sep := opts.Sep
	if sep == "" {
		sep = " "
	}
	tokens, keep := chipTokens(chips, styles, opts.BracketActive)
	packed := Options{
		Sep:    styles.Sep.Render(sep),
		Width:  opts.Width,
		Policy: Drop,
		Keep:   keep,
	}
	if opts.Kicker != "" {
		packed.Lead = styles.Kicker.Render(opts.Kicker)
	}
	if opts.Hint != "" {
		packed.Trail = styles.Hint.Render(opts.Hint)
	}
	return tokens, packed
}

// chipTokens paints a chip row with two Render calls instead of one per chip,
// and reports which token Drop must keep.
//
// A chip carries one of two tones, so the row is two columns: the chosen chips
// and every other one. Each tone's labels are gathered into their own column
// and painted once — see paintColumn — then handed back out in declaration
// order. Gathering rather than masking is what keeps this cheaper than the
// per-chip paint it replaces: a column is exactly as tall as the chips it
// paints, so the row still costs n painted lines, at two style resolutions
// instead of n.
//
// Both columns share one scratch slice — the chosen chips at the front, the
// rest behind them — because a chip row is short enough that the bookkeeping
// around the paint is the same order of cost as the paint.
func chipTokens(chips []Chip, styles ChipStyles, bracket bool) (tokens []string, keep int) {
	if len(chips) == 0 {
		return nil, -1
	}
	chosen, keep := 0, -1
	for i, c := range chips {
		if c.Active {
			chosen++
			keep = i
		}
	}
	scratch := make([]string, len(chips))
	active, inactive := scratch[:0:chosen], scratch[chosen:chosen]
	for _, c := range chips {
		c.Label = screenkit.Sanitize(c.Label)
		switch {
		case !c.Active:
			inactive = append(inactive, c.Label)
		case bracket:
			active = append(active, "[ "+c.Label+" ]")
		default:
			active = append(active, c.Label)
		}
	}
	active = paintColumn(styles.Active, active)
	inactive = paintColumn(styles.Inactive, inactive)
	tokens = make([]string, len(chips))
	next, other := 0, 0
	for i, c := range chips {
		if c.Active {
			tokens[i] = active[next]
			next++
			continue
		}
		tokens[i] = inactive[other]
		other++
	}
	return tokens, keep
}
