package tokenstrip

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// Text resolves an i18n key to the active locale's string.
//
// Injected rather than imported so the package has no opinion about where the
// catalog lives. `Model.t` and `Kit.T` both satisfy it as-is.
type Text func(key string) string

// ForColor maps a config-driven colour token to the pill style that paints it.
//
// The accepted tokens are the four theme semantic names — `error`, `warning`,
// `success`, `info` — so `config.{priorities,severities}[].color` stays a stable
// enum and a theme author edits palette tokens in one place. An unknown or empty
// token falls back to the neutral info pill: a renderer must never emit an
// unstyled badge, because an unstyled badge looks like body text.
func ForColor(s screenkit.Styles, color string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(color)) {
	case "error":
		return s.BadgeHigh
	case "warning":
		return s.BadgeFix
	case "success":
		return s.BadgeNormal
	}
	return s.BadgeInfo
}

// Pill renders one config-coloured badge: the label uppercased for visual
// weight, painted in the tone the colour token resolves to.
//
// Priority and severity are the same call. They were two functions that
// differed only in which table the caller looked the definition up in.
//
// Empty when value is blank, so a caller can drop the badge instead of
// rendering an empty pill — two spaces of background that read as a bug.
func Pill(s screenkit.Styles, color, value string) string {
	value = screenkit.Sanitize(value)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return ForColor(s, color).Render(strings.ToUpper(value))
}

// Count renders `N thing` in the given tone, picking the singular or plural key
// by n. Empty at zero: a card says nothing about the blockers it does not have.
func Count(style lipgloss.Style, t Text, n int, singularKey, pluralKey string) string {
	if n <= 0 {
		return ""
	}
	key := pluralKey
	if n == 1 {
		key = singularKey
	}
	return style.Render(fmt.Sprintf("%d %s", n, screenkit.Sanitize(t(key))))
}

// Counts is the blocker / comment / subtask trio every task card carries.
//
// It is a struct rather than three arguments because the ORDER is part of the
// contract — a reader scanning a column expects the same pill in the same
// position on every card — and an order that lives in three call sites is an
// order that holds until someone appends to the wrong one.
type Counts struct {
	Blockers int
	Comments int
	Subtasks int
}

// Render returns the non-empty count pills in card order.
func (c Counts) Render(s screenkit.Styles, t Text) []string {
	out := make([]string, 0, 3)
	for _, pill := range []string{
		Count(s.BadgeBlocker, t, c.Blockers, "tui.badge.blocker", "tui.badge.blockers"),
		Count(s.BadgeComment, t, c.Comments, "tui.badge.comment", "tui.badge.comments"),
		Count(s.BadgeSubtask, t, c.Subtasks, "tui.badge.subtask", "tui.badge.subtasks"),
	} {
		if pill != "" {
			out = append(out, pill)
		}
	}
	return out
}

// CountBand renders a count whose TONE rises with the number: nothing pending
// is calm, one is ordinary, more than one wants attention.
//
// It is a different shape from [Count], which paints every count in the tone its
// caller picked. Here the number is the signal, so the pill changes colour
// rather than the caller changing style — which is what the home screen was
// doing by hand across three branches.
//
// The label is already resolved, the same way [Scope] takes one: the caller owns
// what the thing is called.
func CountBand(s screenkit.Styles, n int, label string) string {
	style := s.BadgeBlocker
	switch {
	case n <= 0:
		style = s.BadgeLow
	case n == 1:
		style = s.BadgeNormal
	}
	return style.Render(fmt.Sprintf("%d %s", n, screenkit.Sanitize(label)))
}

// Label renders a neutral pill around text the caller has already formatted.
//
// Distinct from [Tag], which owns the `#` glyph. A project label shown uppercase
// and a task tag shown with a hash are two vocabularies on purpose; naming both
// here is what stops a screen from reaching into the style table to get one.
func Label(s screenkit.Styles, text string) string {
	return s.BadgeInfo.Render(screenkit.Sanitize(text))
}

// Spend renders the token-spend pill in one of three bands.
//
// It was `badge.Token`, renamed because this package now also holds the key
// bindings a footer paints and two things called Token in one package is one
// thing nobody can name.
//
// The thresholds are exclusive lower bounds and arrive from
// `config.TokenBadgeThresholds`, so a project that runs a tighter context budget
// re-bands every badge in the app by editing YAML.
func Spend(s screenkit.Styles, t Text, tokens, yellow, red int) string {
	label := screenkit.Sanitize(fmt.Sprintf(t("tui.badge.tokens_fmt"), tokens))
	switch {
	case tokens > red:
		return s.TokenRed.Render(label)
	case tokens > yellow:
		return s.TokenYellow.Render(label)
	default:
		return s.TokenGreen.Render(label)
	}
}

// Active marks a catalog entry wired into the active bundle. The Settings view
// lists every preset's entities; this lets the user scan a column for the subset
// actually in force.
func Active(s screenkit.Styles, t Text) string {
	return s.BadgeActive.Render(screenkit.Sanitize(t("tui.badge.active")))
}

// Custom marks a user-owned override, at the same visual weight as the other
// scope-style badges so the column stays scannable.
func Custom(s screenkit.Styles, t Text) string {
	return s.BadgeInfo.Render(screenkit.Sanitize(t("tui.badge.custom")))
}

// Fix marks an entry carrying a warning the user has to resolve.
func Fix(s screenkit.Styles, t Text) string {
	return s.BadgeFix.Render(screenkit.Sanitize(t("tui.badge.fix")))
}

// Tag renders the `#label` pill a tag attached to a task or a comment gets.
//
// The glyph is part of the pill, not the caller's string, because a tag that
// reads `#urgent` on one card and `URGENT` on another is two vocabularies.
func Tag(s screenkit.Styles, label string) string {
	return s.BadgeInfo.Render("#" + screenkit.Sanitize(label))
}

// Scope paints an already-resolved binding label (GLOBAL, PROJECT, PERSONA).
//
// The label is resolved by the caller because the scope enum belongs to the
// domain and this package does not import it.
func Scope(s screenkit.Styles, label string) string {
	label = screenkit.Sanitize(label)
	if strings.TrimSpace(label) == "" {
		return ""
	}
	return s.BadgeScope.Render(label)
}

// PillOptions is the pill family's view of a strip: a width budget and nothing
// else. Pills never drop and never align — a card that cannot show a badge
// shows it on the next row.
func PillOptions(maxWidth int) Options {
	return Options{Width: maxWidth, Policy: Wrap, BandGap: true}
}

// Pills packs the card's badge line.
//
// Was `badge.Wrap`. Every badge is kept and nothing is truncated: a badge wider
// than the budget takes a row of its own, because a clipped pill reads as a
// different pill.
func Pills(pills []string, maxWidth int) string {
	return Render(pills, PillOptions(maxWidth))
}

// PillRows is the height [Pills] renders at, blank band rows included.
//
// Was `badge.Lines`. The board asks for it once per card per keystroke, before
// it has the width to render with, and it must equal what the paint produces.
func PillRows(pills []string, maxWidth int) int {
	return Height(pills, PillOptions(maxWidth))
}
