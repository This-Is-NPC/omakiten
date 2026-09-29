// Package card renders the bordered boxes a surface selects: the kanban Spec
// (Render / Height) and the two activity-feed bodies (Comment / System).
//
// They share a Painter and a theme. They are not one Spec — a comment folds a
// body and a system event is one line, which is a different shape from a
// wrapped title plus badges.
//
// The kanban card is the canonical Spec instance, and four surfaces had written
// it: the board's card, the SAME card reimplemented inside task detail's
// sub-task lanes, the project card on home and the entity card in settings.
// They agreed on the shape — a wrapped title, optional metadata rows, a badge
// line, a border that changes with selection — and disagreed on the details,
// which is the only way that arrangement ever ends.
//
// One of the two task-card copies also hard-wrapped an overlong title token and
// the other did not, so the same title painted inside the box on the board and
// through the edge in task detail.
//
// Height is derived from the SAME line list Render joins, so a caller that asks
// how tall a card will be gets the number the card actually is. The board asks
// exactly that, for every card in the focused column, on every keystroke — the
// measurer that used to answer carried a documented accuracy caveat at narrow
// widths because it re-derived the layout instead of running it.
package card

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
)

// Spec is one card: what goes in it and what state it is in.
//
// It carries no styles and no layout math. The widths arrive from whatever
// resolved the surrounding column — a lane, a grid cell, the arranger — because
// a card is never the thing that decides how wide it is.
type Spec struct {
	// ID prefixes the title line as `#<id> `. Zero means no prefix, which is
	// what the project and entity cards want.
	ID int64
	// Title wraps within the content width, hanging under the prefix.
	Title string
	// Meta are plain-text rows between the title and the badges — `@assignee`, a
	// project's slug and path. Each is sanitized and truncated rather than
	// wrapped, then painted in the hint tone; empty entries are dropped.
	Meta []string
	// Badges are pre-rendered pills, packed onto as many rows as they need.
	Badges []string

	// Selected swaps the border to the focused accent. Archived dims and
	// strikes through. Accent tints the border without claiming the cursor —
	// the plan network's critical-path hint.
	Selected bool
	Archived bool
	Accent   bool

	// Cursor prepends the `›` glyph, and is separate from Selected because two
	// surfaces disagree on purpose. A board lane sits next to accent-tinted
	// neighbours, so the border alone does not say which card the cursor is on
	// and the glyph earns its two columns. The settings grid has no accent
	// cards and marks selection with the border only. Conflating the two would
	// have silently added a chevron to one of them.
	Cursor bool

	// BoxWidth is the width handed to the box style, which in lipgloss covers
	// the content and the padding but NOT the border — a card declared at 30
	// occupies 32 terminal columns. Callers derive it from the column they sit
	// in and have done so since before this package existed; the meaning is
	// preserved here rather than corrected, because changing it would shift
	// every card in the app by two columns.
	//
	// InnerWidth is the content width inside the padding: what the title wraps
	// to and what the badges pack into.
	BoxWidth   int
	InnerWidth int
}

// Painter renders cards in one theme.
//
// Cache is optional. A nil cache renders correctly and sizes a fresh style per
// call; a surface that repaints a whole column on every keystroke shares one so
// the width-sized variants are resolved once. The zero Painter is usable.
type Painter struct {
	Styles screenkit.Styles
	Cache  StyleCache
}

// Render returns the card as a bordered block.
func (p Painter) Render(spec Spec) string {
	return p.box(spec).Render(strings.Join(p.lines(spec), "\n"))
}

// Fit sizes the card, including its border and padding, inside width.
func (p Painter) Fit(spec Spec, width int) Spec {
	box := p.box(spec)
	spec.InnerWidth = max(1, width-box.GetHorizontalFrameSize())
	spec.BoxWidth = max(1, width-box.GetHorizontalBorderSize())
	return spec
}

// Height is the rendered row count, borders included.
//
// It runs the same line builder Render does rather than predicting it. That
// costs the string building a pure count would skip, and buys the guarantee
// that the number a column budgets against is the number it gets.
func (p Painter) Height(spec Spec) int {
	// +2 for the top and bottom border: every variant draws a single row on
	// each side.
	return len(p.lines(spec)) + 2
}

// lines is the card's content, one entry per rendered row. The ONE derivation
// both Render and Height read.
func (p Painter) lines(spec Spec) []string {
	prefix := p.prefix(spec)
	width := max(spec.InnerWidth-lipgloss.Width(prefix), 1)

	wrapped := screenkit.WrapWords(screenkit.Sanitize(spec.Title), width, width)
	lines := make([]string, 0, len(wrapped)+len(spec.Meta)+1)
	for i, part := range wrapped {
		if i == 0 {
			lines = append(lines, prefix+part)
			continue
		}
		// Subsequent lines hang under the prefix rather than starting at the
		// card edge, so the title reads as one block instead of wrapping around
		// the id.
		lines = append(lines, strings.Repeat(" ", lipgloss.Width(prefix))+part)
	}
	metaLines := make([]string, 0, len(spec.Meta))
	for _, meta := range spec.Meta {
		meta = screenkit.Sanitize(meta)
		if meta == "" {
			continue
		}
		metaLines = append(metaLines, screenkit.Truncate(meta, spec.InnerWidth))
	}
	if len(metaLines) > 0 {
		lines = append(lines, strings.Split(p.Styles.Hint.Render(strings.Join(metaLines, "\n")), "\n")...)
	}
	if badges := tokenstrip.Pills(spec.Badges, spec.InnerWidth); badges != "" {
		lines = append(lines, strings.Split(badges, "\n")...)
	}
	return lines
}

// prefix is the cursor chevron plus the `#id ` marker, either of which may be
// absent. Measured on the STYLED string, so the hanging indent stays honest
// about a glyph the theme painted.
func (p Painter) prefix(spec Spec) string {
	prefix := ""
	if spec.Cursor {
		prefix = p.Styles.Marker.Render("›") + " "
	}
	if spec.ID != 0 {
		prefix += fmt.Sprintf("#%d ", spec.ID)
	}
	return prefix
}

// box picks the border variant.
//
// The precedence is selected > archived > accent > default. Selection wins
// because the cursor must always be findable. Archived is next because the user
// opted into seeing them and expects them dimmed. Accent is only a hint, so it
// loses to both — and it paints from the secondary tone rather than the primary
// one, or a critical-path ring would be indistinguishable from the cursor.
func (p Painter) box(spec Spec) lipgloss.Style {
	// A degenerate box width is not hypothetical: the arranger hands a column
	// whatever survives the split, and a nested grid can take that to zero
	// before a screen has any say. lipgloss treats a negative Width as "no
	// constraint", so an unguarded card would silently paint at its natural
	// width — wider than the lane that asked for it, which is the overdraw the
	// fit gate exists to catch. One column is the honest floor: the border
	// still owes its two, and the caller sees a sliver rather than a lie.
	spec.BoxWidth = max(1, spec.BoxWidth)
	switch {
	case spec.Selected:
		return p.Cache.sized(kindSelected, p.Styles.CardSelected, spec.BoxWidth)
	case spec.Archived:
		return p.Cache.sized(kindArchived, p.Styles.CardArchived, spec.BoxWidth)
	case spec.Accent:
		return p.Cache.sized(kindAccent,
			p.Styles.Card.BorderForeground(p.Styles.Info.GetForeground()), spec.BoxWidth)
	default:
		return p.Cache.sized(kindPlain, p.Styles.Card, spec.BoxWidth)
	}
}

type kind int

const (
	kindPlain kind = iota
	kindSelected
	kindArchived
	kindAccent
)

type cacheKey struct {
	kind  kind
	width int
}

// StyleCache memoises box styles by variant and width.
//
// lipgloss.Style.Width returns a fresh copy each call, which on a long column is
// one throwaway per card per keystroke. A nil StyleCache is valid and simply
// does not cache — reading a nil map is legal, so only the write is guarded.
type StyleCache map[cacheKey]lipgloss.Style

// NewCache returns a cache ready to share across renders. Hold one per theme.
func NewCache() StyleCache { return StyleCache{} }

func (c StyleCache) sized(k kind, base lipgloss.Style, width int) lipgloss.Style {
	key := cacheKey{kind: k, width: width}
	if cached, ok := c[key]; ok {
		return cached
	}
	sized := base.Width(width)
	if c != nil {
		c[key] = sized
	}
	return sized
}
