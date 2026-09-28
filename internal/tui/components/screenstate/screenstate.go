// Package screenstate owns the three states a screen can be in before it has a
// body to paint: it is loading, it failed, or it is empty.
//
// # Current consumers and contract
//
// Home, Description, and Project Resume use this component for their loading,
// failure, and empty states. Each surface supplies its own screen identity and
// operator-facing message; this package supplies precedence and composition.
//
// Failure detail is control-free plain text: controls are sanitized, line
// breaks are normalized to one readable logical detail line, and the complete
// detail is capped first at 960 Unicode code points and then at 240 visible
// cells, with each ceiling including its ellipsis. Wrapping occurs only after
// sanitization, normalization, and both caps.
//
// # The precedence lives here
//
// Failed > Loading > Vazio, resolved by [Resolve] / [Winner]. A screen declares
// which of the three it is in and hands all its candidates over in whatever
// order reads best at the callsite; the order it writes them in cannot change
// what paints. That is the point: a screen that hand-orders `if` statements is
// a screen that can get the order wrong, and eight of them currently do.
//
// # The kicker is required, structurally
//
// A [State] is only reachable through an [Identity], and an [Identity] is only
// reachable through [For], which takes the kicker. There is no exported painter
// that accepts a bare State: [Resolve] is the only door out, and it skips any
// candidate that is not [State.Live] — which a hand-written `State{}` literal
// never is. So "the kicker is required" is enforced by the types rather than by
// this comment. The reasoning is the one already written down next door about
// the badge contract in screenkit/styles.go: a contract that carries three of
// four priorities is not a contract, and a state panel that carries no screen
// identity is not a state panel.
//
// # No spinner
//
// Loading is STATIC. There is no tea.Tick, no frame counter and no command in
// the event loop: a screen that is waiting on a query paints one line saying so
// and stops. Nor is there a recovery hint ("press r to reload") — the host
// already paints a persistent footer carrying this screen's key bindings, so an
// in-panel hint would be the second copy of chrome that already exists.
//
// The package is a leaf: it takes a screenkit.Kit and returns a string. It
// imports nothing from internal/tui and nothing from any screen.
package screenstate

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// Kind is which of the three states a screen is in.
//
// Vazio keeps its Portuguese name on purpose — `Empty` is already taken twice
// over in this tree (screenkit.Styles.Empty, columnframe.Model.EmptyLine) and
// the ambiguity between "the style" and "the state" is exactly what made the
// empty state paint four different ways.
type Kind int

const (
	// Loading — the screen has asked for its data and has not been answered.
	Loading Kind = iota
	// Failed — the request came back with an error, or the dependency the
	// screen needs is not there at all. See [Identity.Unavailable] for the
	// second half of that: not every broken dependency produces an error value.
	Failed
	// Vazio — the request succeeded and there is nothing to show.
	Vazio
)

// rank is the precedence order, lowest wins: Failed > Loading > Vazio.
//
// A failure outranks a load because a screen that is both is a screen whose
// load has already finished badly, and telling the operator it is still working
// is a lie. A load outranks empty because "nothing here" is only true once the
// answer is in — the emptiness of an unanswered screen is the emptiness of a
// question, not of a result.
func (k Kind) rank() int {
	switch k {
	case Failed:
		return 0
	case Loading:
		return 1
	default:
		return 2
	}
}

// State is one candidate state, already translated.
//
// The fields are readable so a caller can inspect the winner — a screen that
// appends teaching guidance under only one kind needs to know which kind won —
// but they are not sufficient to build one: see [Identity].
type State struct {
	// Kicker is the screen's identity — the `// PLANS` above the message. It
	// is required for all three kinds; a state without one does not paint.
	//
	// This package guarantees a kicker EXISTS. It cannot guarantee it is the
	// one the screen already uses, and four surfaces today label their header
	// differently from the help title a state would naturally reach for. The
	// rule for which label to hand [For] is written down next door, on
	// screenkit.Kicker.
	Kicker string
	// Message is the one line the operator reads, resolved through the catalog
	// by the caller. This package never touches i18n.
	Message string
	// Detail is the optional secondary line, always in the Hint tone. Failure
	// details are normalized into inert, bounded plain text by [Identity.Failed]
	// before they reach the renderer; empty-state guidance is kept as supplied.
	Detail string

	kind Kind
	live bool
}

// Kind reports which of the three this candidate is.
func (s State) Kind() Kind { return s.kind }

// Live reports whether the screen is actually in this state AND is allowed to
// paint it. A candidate built by an [Identity] with no kicker is never live:
// an unattributed panel is the exact failure this package exists to stop, so it
// is refused rather than painted anonymously.
func (s State) Live() bool { return s.live && s.Kicker != "" }

// Identity is a screen's name, bound once. It is the only constructor of a
// paintable [State] — see the package comment.
type Identity struct{ kicker string }

// For binds a screen's identity. The kicker is the label, not the rendered
// glyph: the `// ` prefix and the upper-casing belong to screenkit.Kicker and
// are applied at paint time.
func For(kicker string) Identity {
	return Identity{kicker: strings.TrimSpace(screenkit.Sanitize(kicker))}
}

// Loading builds the loading candidate, live only when the screen really is
// loading. The flag is a parameter rather than the caller's `if` so the caller
// never orders the three states itself.
func (i Identity) Loading(loading bool, message string) State {
	return State{Kicker: i.kicker, Message: message, kind: Loading, live: loading}
}

// Failed builds the failure candidate, live exactly when err is non-nil. Its
// detail is control-free plain text: controls are sanitized, line breaks become
// one readable logical detail line, and the complete detail is capped first at
// 960 Unicode code points and then at 240 visible cells, with each ceiling
// including its ellipsis, before any caller-requested wrapping. The
// operator-facing copy comes in as message, so untrusted error text cannot
// control or flood the terminal.
func (i Identity) Failed(err error, message string) State {
	if err == nil {
		return State{Kicker: i.kicker, kind: Failed}
	}
	return State{Kicker: i.kicker, Message: message, Detail: failureDetail(err.Error()), kind: Failed, live: true}
}

const (
	maxFailureDetailCodePoints = 960
	maxFailureDetailCells      = 240
)

func failureDetail(detail string) string {
	// Preserve a visible boundary where a control separated two fragments.
	// ESC and the 8-bit CSI/OSC introducers are left intact so
	// SanitizeMultiline can recognize and remove their complete sequences.
	var separated strings.Builder
	separated.Grow(len(detail))
	for _, r := range detail {
		separated.WriteRune(r)
		if unicode.IsControl(r) && r != '\x1b' && r != '\u009b' && r != '\u009d' {
			separated.WriteByte(' ')
		}
	}

	safe := screenkit.SanitizeMultiline(separated.String())
	normalized := strings.Join(strings.Fields(safe), " ")
	resourceBounded := truncateCodePoints(normalized, maxFailureDetailCodePoints)
	return screenkit.Truncate(resourceBounded, maxFailureDetailCells)
}

// truncateCodePoints caps retained failure-detail content by Unicode code
// points, including the ellipsis. The bound complements terminal-cell width:
// combining marks, variation selectors, and joiners can consume storage and
// rendering work without consuming a visible cell.
func truncateCodePoints(s string, limit int) string {
	if limit <= 0 {
		return ""
	}

	count := 0
	boundary := len(s)
	for index := range s {
		if count == limit-1 {
			boundary = index
		}
		count++
		if count > limit {
			return s[:boundary] + "…"
		}
	}
	return s
}

// Unavailable builds a failure candidate for a broken dependency that reports
// itself as a bool rather than as an error.
//
// Three surfaces need this and none of them has an error value to hand: Stats,
// Insights and Logs each ask their repository "are you wired up" and get back a
// plain bool, so a missing dependency produces nothing to carry. Until this
// existed, the only constructor that fit a bool was [Identity.Vazio], which
// would paint a broken dependency in the muted empty tone beside genuine
// emptiness — and the two are not the same fact. A missing metrics repository
// is a dependency that is not there; empty is "the query returned zero rows".
// An operator reading both could tell neither apart.
//
// It is a second constructor rather than a nil-tolerant [Identity.Failed] on
// purpose. Failed is live exactly when err != nil, and that is what lets every
// screen write `id.Failed(s.err, …)` unconditionally; a Failed that also went
// live on a nil error would paint a failure on every healthy screen in the
// tree. Detail stays empty here because there is nothing to paste into a bug
// report — the whole point is that there is no error value.
//
// The kicker guarantee is untouched. This is a method on [Identity], and
// [Identity] is still only reachable through [For], so the new door leads out
// of the same room as the other three.
func (i Identity) Unavailable(unavailable bool, message string) State {
	return State{Kicker: i.kicker, Message: message, kind: Failed, live: unavailable}
}

// Vazio builds the empty candidate, live only when the screen really is empty.
// hint is the line that teaches the interface, and may be blank.
//
// Empty is a RESULT: the request succeeded and the answer was nothing. A
// dependency that never answered is [Identity.Unavailable], not this.
func (i Identity) Vazio(empty bool, message, hint string) State {
	return State{Kicker: i.kicker, Message: message, Detail: hint, kind: Vazio, live: empty}
}

// Winner picks the live candidate with the highest precedence — Failed >
// Loading > Vazio — without painting it. ok is false when the screen is in none
// of the three and should paint its real body.
//
// Exposed alongside Resolve because a caller can need the decision rather than
// the pixels: guidance that belongs under the empty state only has to ask which
// state is on screen. The alternative is the precedence re-spelled as a
// composite condition at the callsite — `!loading && err == nil && len(x) == 0`,
// which one screen writes today — and that is a second copy of the ordering this
// package exists to hold once.
func Winner(candidates ...State) (State, bool) {
	best, found := State{}, false
	for _, candidate := range candidates {
		if !candidate.Live() {
			continue
		}
		if !found || candidate.kind.rank() < best.kind.rank() {
			best, found = candidate, true
		}
	}
	return best, found
}

// Resolve picks the winning state and paints it, folded to width.
//
// ok is false when no candidate is live — the screen paints its own body. The
// returned string is the state BODY, without chrome: the caller wraps it in
// whatever it wraps its real body in (kit.Panel for most surfaces, a layout
// Block item for a screen whose body is not a panel), so the state and the body
// cannot disagree about the screen's frame.
//
// width is the content width the state may fold into — kit.PanelContentWidth()
// for a panelled screen, the canvas width for a section. A non-positive width
// means "do not fold": the caller's own box owns the clamp.
func Resolve(kit screenkit.Kit, width int, candidates ...State) (string, bool) {
	state, ok := Winner(candidates...)
	if !ok {
		return "", false
	}
	return render(kit, width, state), true
}

// render is the composition, identical for all three kinds apart from the tone
// the message is painted in: kicker, blank row, message, and the detail when
// there is one.
//
// Unexported on purpose. An exported painter taking a bare State would be a way
// to render a state with no kicker, which is the thing the package refuses.
func render(kit screenkit.Kit, width int, state State) string {
	rows := []string{kit.Styles.Kicker(state.Kicker), "", tone(kit, state.kind).Render(state.Message)}
	if state.Detail != "" {
		rows = append(rows, kit.Styles.Hint.Render(state.Detail))
	}
	// Zero is not degenerate here — it is the documented "do not fold" of
	// WrapAt, and callers that have no budget to impose pass it deliberately
	// (see TestEveryLiveStateCarriesItsKickerWhicheverKindItIs). What is
	// degenerate is a NEGATIVE width, which a section arrives at by subtracting
	// chrome from a box the arranger squeezed to nothing. Fold that back onto
	// the sentinel rather than inventing a one-column fold nobody asked for.
	if width < 0 {
		width = 0
	}
	return screenkit.WrapAt(strings.Join(rows, "\n"), width)
}

// tone is the message style per kind. The detail line is always Hint, for all
// three, so it is not selected here.
//
// Vazio takes the empty TONE off Styles.Empty and drops its GEOMETRY. That
// style is the kanban lane's empty-line style — `Width(columnWidth).Align(
// Center)` — so rendering a panel message through it as-is squeezes the whole
// panel down to one lane's width and centres the text inside it. The colour is
// the shared thing; the column width is the lane's, and a state panel is not a
// lane. Folding is this package's job (see render) and the caller's box owns
// the rest, so both rules come off here rather than being fought at every
// callsite.
func tone(kit screenkit.Kit, kind Kind) lipgloss.Style {
	switch kind {
	case Failed:
		return kit.Styles.Error
	case Vazio:
		return kit.Styles.Empty.UnsetWidth().UnsetAlign()
	default:
		return kit.Styles.Hint
	}
}
