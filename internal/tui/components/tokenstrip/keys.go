// Keys are the strip's binding tokens — the footer that tells the reader which
// keys do something here.
package tokenstrip

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// Key is one binding in a footer row.
type Key struct {
	Key     string
	Label   string
	Primary bool
}

// KeyStyles carries the two text treatments a footer uses. Primary is reserved
// for the highest-priority keys; Secondary paints labels, separators and every
// other key.
type KeyStyles struct {
	Primary   lipgloss.Style
	Secondary lipgloss.Style
	Separator string
	// MaxPrimaries caps how many keys keep the accent. Past the cap a key is
	// still painted, in Secondary — an accent on everything is an accent on
	// nothing.
	MaxPrimaries int
	Align        string
}

// FromStyles projects the screenkit theme onto the two footer treatments.
// Colours only — HintAccent / Border may grow geometry later, and a footer
// key must not inherit a border or a width. Widget → tokens is the legal
// direction; screenkit must not import this package.
func FromStyles(s screenkit.Styles) KeyStyles {
	return KeyStyles{
		Primary:   lipgloss.NewStyle().Foreground(s.HintAccent.GetForeground()).Bold(true),
		Secondary: lipgloss.NewStyle().Foreground(s.Border.GetForeground()),
	}
}

// Keys paints one footer row, unwrapped.
func Keys(keys []Key, styles KeyStyles) string {
	tokens, packed := keyStrip(keys, styles, 0)
	return Render(tokens, packed)
}

// KeysWrapped paints the footer, wrapping across rows when width is positive.
// A binding wider than the width takes a row of its own rather than being cut.
func KeysWrapped(keys []Key, styles KeyStyles, width int) string {
	tokens, packed := keyStrip(keys, styles, width)
	return Render(tokens, packed)
}

func keyStrip(keys []Key, styles KeyStyles, width int) ([]string, Options) {
	styles = normalizeKeyStyles(styles)
	live, accent := liveKeys(keys, styles.MaxPrimaries)
	return keyTokens(live, accent, styles), Options{
		Sep:    styles.Secondary.Render(styles.Separator),
		Width:  width,
		Policy: Wrap,
		Align:  styles.Align,
	}
}

// liveKeys drops the blank bindings and decides, in declaration order, which of
// the survivors still fit under MaxPrimaries and therefore keep the accent.
func liveKeys(keys []Key, budget int) (live []Key, accent []bool) {
	live = make([]Key, 0, len(keys))
	accent = make([]bool, 0, len(keys))
	for _, key := range keys {
		key.Key = screenkit.Sanitize(key.Key)
		key.Label = screenkit.Sanitize(key.Label)
		if strings.TrimSpace(key.Key) == "" {
			continue
		}
		primary := key.Primary && budget > 0
		if primary {
			budget--
		}
		live = append(live, key)
		accent = append(accent, primary)
	}
	return live, accent
}

// keyTokens paints the strip's tokens with three Render calls instead of up to
// two per binding.
//
// A token is a key in one of two tones plus an optional label in the second, so
// the strip is three columns: accented keys, plain keys, labels. Each column is
// gathered, painted once — see paintColumn — and handed back out in
// declaration order. The old shape also paid for a Secondary key paint it then
// threw away whenever the binding turned out to be primary.
func keyTokens(live []Key, accent []bool, styles KeyStyles) []string {
	if len(live) == 0 {
		return nil
	}
	primaries, secondaries, labels := keyColumns(live, accent)
	paintedPrimary := paintColumn(styles.Primary, primaries)
	paintedSecondary := paintColumn(styles.Secondary, secondaries)
	paintedLabels := paintColumn(styles.Secondary, labels)
	tokens := make([]string, len(live))
	nextPrimary, nextSecondary, nextLabel := 0, 0, 0
	for i, k := range live {
		if accent[i] {
			tokens[i] = paintedPrimary[nextPrimary]
			nextPrimary++
		} else {
			tokens[i] = paintedSecondary[nextSecondary]
			nextSecondary++
		}
		if k.Label != "" {
			tokens[i] += paintedLabels[nextLabel]
			nextLabel++
		}
	}
	return tokens
}

// keyColumns gathers the strip's three tone columns. Each is sized for the
// whole strip rather than counted first: three allocations either way, and the
// count pass would be a second walk over the same bindings.
func keyColumns(live []Key, accent []bool) (primaries, secondaries, labels []string) {
	primaries = make([]string, 0, len(live))
	secondaries = make([]string, 0, len(live))
	labels = make([]string, 0, len(live))
	for i, k := range live {
		if accent[i] {
			primaries = append(primaries, k.Key)
		} else {
			secondaries = append(secondaries, k.Key)
		}
		if k.Label != "" {
			labels = append(labels, " "+k.Label)
		}
	}
	return primaries, secondaries, labels
}

// paintColumn paints every entry through style in ONE Render call, in place.
//
// lipgloss resolves a style into terminal codes per Render call and then
// applies them line by line, so a column painted as one block carries exactly
// the codes N separate calls would have produced — at one resolution for the
// whole strip instead of one per token. The block form also right-pads every
// line out to the widest one, because lipgloss aligns any multi-line render;
// that pad is the one thing to undo, which is why entries here are keys and
// labels that never end in a space.
//
// The painted lines are written back over entries rather than returned in a new
// slice, and a column of one skips the block entirely: a strip is a handful of
// tokens, so the allocations around the paint are the same order as the paint.
func paintColumn(style lipgloss.Style, entries []string) []string {
	switch len(entries) {
	case 0:
		return nil
	case 1:
		entries[0] = style.Render(entries[0])
		return entries
	}
	block := style.Render(strings.Join(entries, "\n"))
	for i := range entries {
		line := block
		if cut := strings.IndexByte(block, '\n'); cut >= 0 {
			line, block = block[:cut], block[cut+1:]
		}
		entries[i] = strings.TrimRight(line, " ")
	}
	return entries
}

func normalizeKeyStyles(styles KeyStyles) KeyStyles {
	styles.Separator = screenkit.Sanitize(styles.Separator)
	if styles.Separator == "" {
		styles.Separator = "  "
	}
	if styles.MaxPrimaries <= 0 {
		styles.MaxPrimaries = 3
	}
	if styles.Align == "" {
		styles.Align = AlignLeft
	}
	return styles
}
