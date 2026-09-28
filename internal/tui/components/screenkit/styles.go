package screenkit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles is the theme projection an extracted screen is allowed to see. The
// root Model resolves the full theme; this is the subset every screen surface
// needs to paint panels, rules, cursors, kickers and per-category accents.
//
// It is deliberately a plain value: a screen receives a fresh copy through the
// frame on every render, so a theme swap lands on the next frame without the
// screen holding a reference back into root state.
type Styles struct {
	// Panel is the bordered chrome every screen body sits inside.
	Panel lipgloss.Style
	// Border paints the grid-table rules drawn by gridtable.Render.
	Border lipgloss.Style
	// Separator paints HRule.
	Separator lipgloss.Style
	// Marker paints the selected-row cursor glyph.
	Marker lipgloss.Style

	// Info is the structural tone: kickers, column headers, totals.
	Info lipgloss.Style
	// Hint is the muted tone for secondary copy and scroll indicators.
	Hint lipgloss.Style
	// HintAccent is the single accent reserved for the focal figure or the
	// active chip on a surface.
	HintAccent lipgloss.Style
	// HintBox is Panel's chrome capped to a reading measure. Check the theme
	// before assuming otherwise: it is built from the SAME border, the same
	// border colour and the same Padding(0, 2) as Panel, and the only thing that
	// differs is a Width. It is a NARROW PANEL, not a borderless container, and
	// callers re-clamp that width to `Clamp(AvailableWidth()-8, 32, 60)` so a
	// paragraph of guidance does not run to the full width of a 200-column
	// terminal.
	//
	// # HintBox vs Empty
	//
	// These two were never written down together anywhere except a passing line
	// on the kanban lane painter, and the empty state drifted into four
	// different paints as a result. They are not alternatives — they answer
	// different questions, and a surface can need both:
	//
	//   - Empty is a TONE, and it belongs to the STATE. Through
	//     screenstate.Vazio it paints the fact — "there is nothing here" — under
	//     the screen's kicker. It says nothing about what box the state sits in.
	//   - HintBox is a BOX, and it belongs to the GUIDANCE: the commands to run
	//     and the keys to press that make the missing thing exist. It carries no
	//     kicker and no tone of its own; the lines inside it are Hint and
	//     HintAccent.
	//
	// So the question a caller answers is not "which one", it is WHERE THE
	// GUIDANCE GOES relative to the state, and the answer follows the frame the
	// screen's real body uses:
	//
	//   - A screen whose body is a Panel puts the state and the guidance in ONE
	//     box — the state body first, the teaching lines under it, the whole
	//     thing rendered through HintBox so it keeps the reading measure. Two
	//     stacked boxes is the alternative, and it reads as two unrelated
	//     panels.
	//   - A screen whose body is NOT a Panel — a column, a lane carousel —
	//     keeps the guidance as its own HintBox below that body, because there
	//     is no panel to put it inside. screenstate.Winner answers which state
	//     is on screen, so the box can be appended under one kind only.
	//
	// One corollary: the state's kicker is the state's. A HintBox that paints a
	// kicker of its own is a second empty-state chrome.
	HintBox lipgloss.Style
	// Empty is the empty state's TONE — see the HintBox comment above for how
	// the two divide, and screenstate.tone for why the state panel strips this
	// style's geometry before using it.
	//
	// The geometry it carries (Width + Align(Center)) is the KANBAN LANE's, not
	// a general empty-state's: a kanban lane paints empty through it
	// at the lane's inner width. Any caller that is not a lane takes the colour
	// and drops the width, which screenstate does once for all of them.
	//
	// It is also NOT the tone for a hole inside a body the screen is still
	// painting — task detail's "no blockers", the detail grid's "no
	// description", stats' "no per-model rows". Those screens are in no state at
	// all; their placeholders stay Hint, in place.
	Empty lipgloss.Style
	// Error and Warning carry the semantic badges.
	Error   lipgloss.Style
	Warning lipgloss.Style
	Success lipgloss.Style

	// The badge pills. A screen paints a priority, a count or a token spend by
	// picking one of these — never by assembling a Background/Foreground pair of
	// its own, which is how two surfaces end up disagreeing about what NORMAL
	// looks like.
	//
	// The set is complete on purpose. It was not: BadgeHigh, BadgeComment and
	// BadgeSubtask existed on the root styles and had no projection here, so a
	// caller holding only a Kit could paint a LOW priority but not a HIGH one.
	// A contract that carries three of four priorities is not a contract, and the
	// gap only surfaced when something outside the event loop tried to use it.
	BadgeInfo    lipgloss.Style
	BadgeLow     lipgloss.Style
	BadgeNormal  lipgloss.Style
	BadgeHigh    lipgloss.Style
	BadgeBlocker lipgloss.Style
	BadgeComment lipgloss.Style
	BadgeSubtask lipgloss.Style
	BadgeScope   lipgloss.Style
	BadgeFix     lipgloss.Style
	BadgeActive  lipgloss.Style
	// TokenGreen / TokenYellow / TokenRed are the three bands a token-spend badge
	// resolves to against the configured thresholds.
	TokenGreen      lipgloss.Style
	TokenYellow     lipgloss.Style
	TokenRed        lipgloss.Style
	Card            lipgloss.Style
	CardSelected    lipgloss.Style
	CardArchived    lipgloss.Style
	CommentCard     lipgloss.Style
	SystemEventCard lipgloss.Style
	Input           lipgloss.Style
	FormMultiline   lipgloss.Style
	Cursor          lipgloss.Style

	// Nav and ActiveNav paint inline pickers (the Stats period strip).
	Nav       lipgloss.Style
	ActiveNav lipgloss.Style

	// CategoryTask … CategoryToolCall are the per-event-category accents the
	// Logs inspector paints its TYPE column with. A theme that omits a
	// category token resolves to the generic hint colour upstream, so these
	// are always safe to Render with.
	CategoryTask     lipgloss.Style
	CategoryComment  lipgloss.Style
	CategoryPlan     lipgloss.Style
	CategoryAudit    lipgloss.Style
	CategoryGuard    lipgloss.Style
	CategoryTrick    lipgloss.Style
	CategoryToolCall lipgloss.Style
}

// Kicker renders a section label in dev-editorial style: `// LABEL`. Structural
// labels use the supplied (secondary) style so the primary accent stays
// reserved for active focus.
//
// Exposed as a free function so the root package and every extracted screen
// share one implementation rather than re-deriving the glyph and the casing.
//
// # What a screen's kicker is
//
// The glyph was shared long before the LABEL was, and screenstate made the gap
// visible: it guarantees every state panel carries a kicker, but it cannot
// guarantee the kicker is the one the screen already uses. Four surfaces had
// two identities each. One rule closes it:
//
//	A kicker names the screen's SUBJECT — the noun for the thing the surface
//	shows — and it resolves from ONE tui.kicker.* key per surface. The screen's
//	own header and its screenstate crown read that same key.
//
// The corollaries are the four ways it was already broken:
//
//   - A SUBJECT, never a VIEW. tui.help.*.title is the help overlay's
//     vocabulary — "Task view", "Comment view", "Entity view" — and every
//     kicker beside them is a noun: PLANS, PROJECT, GOAL, TASKS. "View" is a
//     help panel leaking into a section label, so a state that borrows a help
//     title renames the screen for the duration of the state.
//   - A TRANSLATED subject. `For(string(descriptor.Kind))` and
//     `fmt.Sprintf("%s · %s", kind.String(), slug)` paint a Go identifier, so
//     they render `// LAWS` in all 21 language packs. A parameterised subject
//     maps its parameter to a KEY, never to the raw value.
//   - `·` carries METADATA about that subject and nothing else: a count
//     (`// PLANS · 12`, see KickerCount), an id (`// TASK · #42`), a current
//     value (`// THEME · CURRENT: OMACON`). It never joins a screen name to a
//     sub-screen name — `// SETTINGS · GENERAL` reads as a malformed count.
//   - ONE key per surface. Home paints `// PROJECTS · N` above its cards, so a
//     state on that surface is crowned PROJECTS too — not HOME, not the help
//     overlay's title for the route.
//
// A parameterised subject also settles what the state MESSAGE has to carry:
// nothing about which catalog it is. If the kicker says LAWS, then "Could not
// load this catalog." is already complete.
func Kicker(style lipgloss.Style, label string) string {
	return style.Render("// " + strings.ToUpper(Sanitize(label)))
}

// KickerCount renders `// LABEL · N` — a kicker with a trailing count.
func KickerCount(style lipgloss.Style, label string, count int) string {
	return style.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(Sanitize(label)), count))
}

// Kicker renders a structural section label in the info tone.
func (s Styles) Kicker(label string) string { return Kicker(s.Info, label) }

// KickerCount renders a structural section label with a trailing count.
func (s Styles) KickerCount(label string, count int) string {
	return KickerCount(s.Info, label, count)
}

// FocusKicker renders `▸ LABEL` in HintAccent — the focused section marker
// Task Detail and Studio use. The ▸ is chrome, not catalog copy.
func FocusKicker(style lipgloss.Style, label string) string {
	return style.Render("▸ " + strings.ToUpper(Sanitize(label)))
}

// FocusKickerCount renders `▸ LABEL · N`.
func FocusKickerCount(style lipgloss.Style, label string, count int) string {
	return style.Render(fmt.Sprintf("▸ %s · %d", strings.ToUpper(Sanitize(label)), count))
}

func (s Styles) FocusKicker(label string) string {
	return FocusKicker(s.HintAccent, label)
}

func (s Styles) FocusKickerCount(label string, count int) string {
	return FocusKickerCount(s.HintAccent, label, count)
}

// SectionKicker is `▸ LABEL` when focused and `// LABEL` otherwise.
func (s Styles) SectionKicker(label string, focused bool) string {
	if focused {
		return s.FocusKicker(label)
	}
	return s.Kicker(label)
}

// SectionKickerCount is `▸ LABEL · N` when focused and `// LABEL · N` otherwise.
func (s Styles) SectionKickerCount(label string, count int, focused bool) string {
	if focused {
		return s.FocusKickerCount(label, count)
	}
	return s.KickerCount(label, count)
}

// PanelFlush is the panel chrome with its padding removed — the border and
// nothing inside it, so a grid of already-padded cells sits flush against the
// frame instead of paying for the panel's reading gutter a second time.
//
// It is a method rather than a second field because it is DERIVED: a theme that
// re-paints Panel re-paints this with it, which is exactly what a screen
// spelling `Styles.Panel.Padding(0, 0)` for itself could not promise.
func (s Styles) PanelFlush() lipgloss.Style { return s.Panel.Padding(0, 0) }

// Accent names one of the per-category tones the Styles carry, so a caller can
// SELECT a tone without holding the style that paints it.
//
// The split is deliberate. Deciding that a tool-call event and a hook event
// share an accent is knowledge about events, and it belongs to the surface that
// knows what an event is; deciding which colour that accent resolves to is
// knowledge about the theme, and it belongs here. Before this existed the
// screen owned both halves and had to name lipgloss.Style in its own signature
// to hand the second one back.
type Accent int

const (
	// AccentNone resolves to the neutral hint tone. It is the zero value, so a
	// caller that cannot classify its row still gets a legible cell rather than
	// a default-black glyph.
	AccentNone Accent = iota
	AccentTask
	AccentComment
	AccentPlan
	AccentAudit
	AccentGuard
	AccentTrick
	AccentToolCall
)

// Accent resolves a named accent to the style that paints it. Unknown values —
// an accent added to the vocabulary before a theme carries a token for it —
// fall back to Hint for the same reason AccentNone does.
func (s Styles) Accent(accent Accent) lipgloss.Style {
	switch accent {
	case AccentTask:
		return s.CategoryTask
	case AccentComment:
		return s.CategoryComment
	case AccentPlan:
		return s.CategoryPlan
	case AccentAudit:
		return s.CategoryAudit
	case AccentGuard:
		return s.CategoryGuard
	case AccentTrick:
		return s.CategoryTrick
	case AccentToolCall:
		return s.CategoryToolCall
	}
	return s.Hint
}
