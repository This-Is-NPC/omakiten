package card

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
)

// DefaultLineLimit is how many body rows a comment shows before it is folded
// behind a "more lines" hint.
//
// It lives here because it is a property of the card, not of a screen: the two
// screens that fold comments were carrying the same 6 under different names, and
// two constants agreeing today is not the same as one constant.
const DefaultLineLimit = 6

// moreFmt resolves the folded-body hint. Callers pass a catalog value via
// MoreFmt (tui.event.more_lines_fmt); empty keeps the English fallback through
// tr so headless tests without a catalog still assert readable copy.
func moreFmt(format string) string {
	if format != "" {
		return format
	}
	return tr(nil, "tui.event.more_lines_fmt", "↩ %d more lines — enter opens")
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

// Comment is a comment somebody wrote: an attributed header, a body that folds
// when it is long, and the tags it carries.
type Comment struct {
	// Author is the attribution shown first, in the accent tone.
	Author string
	// Timestamp follows the author after a separator. Blank drops the separator
	// with it rather than leaving a dangling middot.
	Timestamp string
	// Body is the raw comment text, wrapped to the card and folded past
	// LineLimit.
	Body string
	// EmptyText replaces the body when there is none. Resolved by the caller —
	// this package holds no catalog.
	EmptyText string
	// Tags are raw persisted labels. The card sanitizes and paints them through
	// tokenstrip before packing them onto as many rows as they need.
	Tags []string

	// LineLimit folds the body past this many rows; zero means
	// [DefaultLineLimit], negative means never fold.
	LineLimit int
	// MoreFmt is the folded-body hint, with one %d for the hidden row count.
	// Empty falls back to the package default.
	MoreFmt string

	// Focused tints the border with the accent — the feed's cursor.
	Focused bool
	// Unframed skips the card box so a parent section can paint the only
	// border, with the kicker rule joining the sides the way a gridtable does.
	Unframed bool
	// Width is the width handed to the box style: content plus padding, border
	// outside it. Unframed, it is the content width the body wraps to.
	Width int
}

// System is an event the app recorded: one line, no body, no tags.
type System struct {
	// Label is the already-resolved sentence — "task moved backlog → dev".
	Label     string
	Timestamp string
	Focused   bool
	// Unframed skips the card box so a parent section paints the only border.
	Unframed bool
	Width    int
}

// Comment renders an authored comment.
func (p Painter) Comment(c Comment) string {
	content := p.Styles.HintAccent.Render(screenkit.Sanitize(c.Author))
	if stamp := strings.TrimSpace(screenkit.Sanitize(c.Timestamp)); stamp != "" {
		content += p.Styles.Hint.Render(" · " + stamp)
	}
	content += "\n" + p.commentBody(c)

	inner := ContentWidth(c.Width)
	if c.Unframed {
		inner = max(minContentWidth, c.Width)
	}
	if len(c.Tags) > 0 {
		tags := make([]string, len(c.Tags))
		for i, tag := range c.Tags {
			tags[i] = tokenstrip.Tag(p.Styles, tag)
		}
		content += "\n" + tokenstrip.Pills(tags, inner)
	}
	if c.Unframed {
		return content
	}
	return p.eventBox(p.Styles.CommentCard, c.Width, c.Focused).Render(content)
}

// System renders a recorded event. The label is muted rather than accented
// because the feed's attention belongs to what a person said, not to what the
// app noticed.
func (p Painter) System(s System) string {
	tone := p.Styles.Hint
	if s.Unframed && s.Focused {
		tone = p.Styles.HintAccent
	}
	line := tone.Render(screenkit.Sanitize(s.Label))
	if stamp := strings.TrimSpace(screenkit.Sanitize(s.Timestamp)); stamp != "" {
		line += p.Styles.Hint.Render(" · " + stamp)
	}
	inner := ContentWidth(s.Width)
	if s.Unframed {
		inner = max(minContentWidth, s.Width)
	}
	body := strings.Join(gridtable.WrapLines([]string{line}, inner), "\n")
	if s.Unframed {
		return body
	}
	return p.eventBox(p.Styles.SystemEventCard, s.Width, s.Focused).Render(body)
}

// commentBody wraps the comment and folds it, or says there is nothing to show.
func (p Painter) commentBody(c Comment) string {
	text := strings.TrimSpace(screenkit.SanitizeMultiline(c.Body))
	if text == "" {
		return p.Styles.Hint.Render(screenkit.Sanitize(c.EmptyText))
	}
	inner := ContentWidth(c.Width)
	if c.Unframed {
		inner = max(minContentWidth, c.Width)
	}
	lines := gridtable.WrapLines(strings.Split(text, "\n"), inner)

	limit := c.LineLimit
	if limit == 0 {
		limit = DefaultLineLimit
	}
	if limit < 0 || len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	// The hint REPLACES no line: it is appended after the visible ones, so a
	// folded card is limit+1 rows and the reader can see how much was kept.
	format := moreFmt(screenkit.Sanitize(c.MoreFmt))
	hidden := len(lines) - limit
	return strings.Join(append(lines[:limit:limit], p.Styles.Hint.Render(fmt.Sprintf(format, hidden))), "\n")
}

// eventBox is the bordered frame, tinted when the feed's cursor is on it.
func (p Painter) eventBox(style lipgloss.Style, width int, focused bool) lipgloss.Style {
	// ContentWidth already floors what goes INSIDE the box at 8; the box itself
	// was unguarded, so a feed squeezed to zero handed lipgloss a negative
	// Width, which it reads as "unconstrained" and paints at the natural width
	// of the longest comment. Floor here too — and at the SAME floor the content
	// uses, not at 1: flooring the box lower than ContentWidth floors the body
	// makes the box narrower than what goes in it, and lipgloss resolves that
	// disagreement by growing the box back to the content. The two ends of one
	// width have to share one number.
	style = style.Width(max(minBoxWidth, width))
	if focused {
		style = style.BorderForeground(p.Styles.HintAccent.GetForeground())
	}
	return style
}

// minContentWidth is the narrowest body this card will fold text into, and
// minBoxWidth is the box that holds exactly that plus its two border columns.
//
// They are stated as a pair because they are one decision seen from two sides:
// the body floors at minContentWidth, so a box floored any lower is a box
// narrower than its own contents, which lipgloss silently resolves in the
// content's favour.
const (
	minContentWidth = 8
	minBoxWidth     = minContentWidth + 2
)

// ContentWidth is the room inside a card of the given box width.
//
// Exported because a caller that measures a card before rendering it — a feed
// deciding how many fit — must use the same subtraction the card does, and the
// two screens that fold comments were each doing their own `width-2`.
func ContentWidth(width int) int {
	if inner := width - 2; inner > minContentWidth {
		return inner
	}
	return minContentWidth
}
