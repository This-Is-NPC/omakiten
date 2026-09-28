// This test lives in the EXTERNAL test package, and has to.
//
// The harness it builds its frames from — screentest, over screenfixture — now
// imports THIS package, because a fixture scenario declares which state kind it
// records and that declaration is typed rather than a string. An in-package
// test would close that loop into an import cycle; an external test package is
// a separate package and does not.
//
// Nothing was given up to move it. Every identifier below was already exported:
// the package deliberately has no back door to a paintable State, so an
// external test exercises exactly the surface a screen does and no more.
package screenstate_test

import (
	"errors"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screens/screentest"
)

// kitAt is a real styled Kit at a terminal size — the same projection the
// screens paint with, so a test that says "the empty tone does not drag the
// panel down to one lane's width" is testing the live styles rather than a
// convenient stub.
func kitAt(t *testing.T, width, height int) screenkit.Kit {
	t.Helper()
	return screentest.FrameAt(t, width, height).Kit()
}

func resolved(t *testing.T, kit screenkit.Kit, width int, candidates ...screenstate.State) string {
	t.Helper()
	body, ok := screenstate.Resolve(kit, width, candidates...)
	if !ok {
		t.Fatalf("no candidate resolved; got %q", body)
	}
	return screentest.StripANSI(body)
}

// ---- precedence -------------------------------------------------------------

// The whole point of the package: the screen writes its candidates in whatever
// order reads best and the order does not decide anything.
func TestFailedBeatsLoadingBeatsVazioInEitherWritingOrder(t *testing.T) {
	id := screenstate.For("plans")
	failed := id.Failed(errors.New("connection refused"), "Could not load this screen.")
	loading := id.Loading(true, "Loading plans…")
	vazio := id.Vazio(true, "No plans yet.", "")

	cases := []struct {
		name       string
		candidates []screenstate.State
		want       screenstate.Kind
	}{
		{"all three, failure last", []screenstate.State{vazio, loading, failed}, screenstate.Failed},
		{"all three, failure first", []screenstate.State{failed, loading, vazio}, screenstate.Failed},
		{"loading and empty", []screenstate.State{vazio, loading}, screenstate.Loading},
		{"empty and empty only", []screenstate.State{vazio}, screenstate.Vazio},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			state, ok := screenstate.Winner(testCase.candidates...)
			if !ok {
				t.Fatal("no winner, want one")
			}
			if state.Kind() != testCase.want {
				t.Fatalf("winner kind = %d, want %d", state.Kind(), testCase.want)
			}
		})
	}
}

// A candidate that is not live is not a candidate. This is what lets a screen
// Dead candidates are skipped; an all-dead set resolves to no state.
func TestDeadCandidatesAreSkippedAndAnAllDeadSetResolvesToNoState(t *testing.T) {
	id := screenstate.For("plans")
	all := []screenstate.State{
		id.Failed(nil, "Could not load this screen."),
		id.Loading(false, "Loading plans…"),
		id.Vazio(false, "No plans yet.", ""),
	}
	if state, ok := screenstate.Winner(all...); ok {
		t.Fatalf("winner = %+v, want none — the screen should paint its own body", state)
	}
	if body, ok := screenstate.Resolve(kitAt(t, 80, 24), 60, all...); ok || body != "" {
		t.Fatalf("Resolve = (%q, %v), want (\"\", false)", body, ok)
	}
	// One live candidate among dead ones still wins.
	live := append(append([]screenstate.State(nil), all...), id.Loading(true, "Loading plans…"))
	if state, ok := screenstate.Winner(live...); !ok || state.Kind() != screenstate.Loading {
		t.Fatalf("winner = %+v ok=%v, want the loading candidate", state, ok)
	}
}

// ---- composition ------------------------------------------------------------

func TestEachKindPaintsKickerBlankMessageAndDetail(t *testing.T) {
	kit := kitAt(t, 120, 40)
	id := screenstate.For("plans")
	cases := []struct {
		name       string
		state      screenstate.State
		wantRows   int
		wantSecond string
	}{
		{"loading has no detail row", id.Loading(true, "Loading plans…"), 3, "Loading plans…"},
		{"failed carries the diagnostic detail", id.Failed(errors.New("connection refused"), "Could not load this screen."), 4, "Could not load this screen."},
		{"vazio carries the teaching hint", id.Vazio(true, "No plans yet.", "Create one with `okt plan create`."), 4, "No plans yet."},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rows := strings.Split(resolved(t, kit, 0, testCase.state), "\n")
			if len(rows) != testCase.wantRows {
				t.Fatalf("rows = %d (%q), want %d", len(rows), rows, testCase.wantRows)
			}
			if rows[0] != "// PLANS" {
				t.Fatalf("row 0 = %q, want the kicker `// PLANS`", rows[0])
			}
			if strings.TrimSpace(rows[1]) != "" {
				t.Fatalf("row 1 = %q, want the blank spacer", rows[1])
			}
			if strings.TrimSpace(rows[2]) != testCase.wantSecond {
				t.Fatalf("row 2 = %q, want %q", rows[2], testCase.wantSecond)
			}
		})
	}
}

func TestDetailRowIsOmittedWhenThereIsNoDetail(t *testing.T) {
	kit := kitAt(t, 120, 40)
	id := screenstate.For("plans")
	withHint := resolved(t, kit, 0, id.Vazio(true, "No plans yet.", "Create one."))
	without := resolved(t, kit, 0, id.Vazio(true, "No plans yet.", ""))
	if strings.Count(withHint, "\n") != 3 {
		t.Fatalf("with a hint =\n%q\nwant four rows", withHint)
	}
	if strings.Count(without, "\n") != 2 {
		t.Fatalf("without a hint =\n%q\nwant three rows — no trailing blank where the detail was", without)
	}
}

func TestIdentitySanitizesDynamicKicker(t *testing.T) {
	state := screenstate.For("project 漢字 \x1b[31mred\x1b]0;owned\a C1\u009b31m\u009d").Loading(true, "loading")
	body, ok := screenstate.Resolve(screenkit.Kit{}, 0, state)
	if !ok {
		t.Fatal("sanitized identity did not resolve")
	}
	if strings.Contains(body, "owned") || strings.Contains(body, "31m") {
		t.Fatalf("state retained terminal payload: %q", body)
	}
	if !strings.Contains(body, "漢字") {
		t.Fatalf("state lost harmless Unicode: %q", body)
	}
}

func TestFailedDetailIsSanitizedNormalizedAndBoundedBeforeWrapping(t *testing.T) {
	const safePrefix = "diagnostic-id=abc123"
	hostile := safePrefix + "\x1b[31m red\x1b[0m\x1b]2;owned\a" +
		"\r\nsecond\tfragment\x00nul\x7fdel\u0085c1 " + strings.Repeat("界", 200)

	// A zero-value style set emits no framework ANSI, so every control in this
	// sink output would have come from the hostile error itself.
	body, ok := screenstate.Resolve(screenkit.Kit{}, 0,
		screenstate.For("plans").Failed(errors.New(hostile), "Could not load this screen."))
	if !ok {
		t.Fatal("hostile failure did not resolve")
	}
	for _, r := range body {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("rendered failure retained control U+%04X in %q", r, body)
		}
	}
	if strings.Contains(body, "[31m") || strings.Contains(body, "]2;owned") || strings.Contains(body, "owned") {
		t.Fatalf("rendered failure retained ANSI/OSC payload: %q", body)
	}

	rows := strings.Split(body, "\n")
	if len(rows) != 4 {
		t.Fatalf("unwrapped failure has %d rows (%q), want one normalized detail row", len(rows), rows)
	}
	if rows[2] != "Could not load this screen." {
		t.Fatalf("operator message = %q, want it preserved", rows[2])
	}
	detail := rows[3]
	if !strings.HasPrefix(detail, safePrefix) {
		t.Fatalf("truncation discarded useful diagnostic prefix: %q", detail)
	}
	if !strings.Contains(detail, "red second fragment nul del c1") {
		t.Fatalf("control-separated fragments were not normalized to readable spacing: %q", detail)
	}
	if got := lipgloss.Width(detail); got > 240 {
		t.Fatalf("detail is %d visible cells, exceeds 240: %q", got, detail)
	}
	if !strings.HasSuffix(detail, "…") {
		t.Fatalf("oversized detail was not visibly truncated: %q", detail)
	}
}

func TestFailedDetailHasFiniteUnicodeCodePointCeiling(t *testing.T) {
	const maxFailureDetailCodePoints = 960
	const safePrefix = "failure-id=zero-width "
	zeroWidthFlood := strings.Repeat("\u0301\uFE0F\u200D", maxFailureDetailCodePoints*8)
	state := screenstate.For("plans").Failed(
		errors.New(safePrefix+zeroWidthFlood),
		"Could not load this screen.",
	)

	for render := 1; render <= 2; render++ {
		detail := state.Detail
		if !utf8.ValidString(detail) {
			t.Fatalf("render %d produced invalid UTF-8", render)
		}
		if got := utf8.RuneCountInString(detail); got != maxFailureDetailCodePoints {
			t.Fatalf("render %d retained %d Unicode code points, want the finite ceiling %d", render, got, maxFailureDetailCodePoints)
		}
		if got := lipgloss.Width(detail); got >= 240 {
			t.Fatalf("render %d hostile detail is %d visible cells, want a below-240-cell resource-bound case", render, got)
		}
		if !strings.HasPrefix(detail, safePrefix) || !strings.HasSuffix(detail, "…") {
			t.Fatalf("render %d lost the useful prefix or truncation marker: %q", render, detail)
		}
		state = screenstate.For("plans").Failed(errors.New(safePrefix+zeroWidthFlood), "Could not load this screen.")
	}
}

func TestFailedDetailUnicodeCodePointCeilingEdges(t *testing.T) {
	const maxFailureDetailCodePoints = 960
	cases := map[string]struct {
		input         string
		wantRunes     int
		wantTruncated bool
	}{
		"exact ceiling is retained": {
			input:     "x" + strings.Repeat("\u0301", maxFailureDetailCodePoints-1),
			wantRunes: maxFailureDetailCodePoints,
		},
		"one beyond ceiling is truncated within ceiling": {
			input:         "x" + strings.Repeat("\u0301", maxFailureDetailCodePoints),
			wantRunes:     maxFailureDetailCodePoints,
			wantTruncated: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			detail := screenstate.For("plans").Failed(errors.New(tc.input), "failed").Detail
			if got := utf8.RuneCountInString(detail); got != tc.wantRunes {
				t.Fatalf("detail has %d Unicode code points, want %d", got, tc.wantRunes)
			}
			if got := strings.HasSuffix(detail, "…"); got != tc.wantTruncated {
				t.Fatalf("ellipsis present = %v, want %v", got, tc.wantTruncated)
			}
			if !utf8.ValidString(detail) {
				t.Fatal("secondary truncation produced invalid UTF-8")
			}
		})
	}
}

func TestFailedDetailTruncationRespectsWideGlyphBoundariesThenWraps(t *testing.T) {
	state := screenstate.For("plans").Failed(
		errors.New("wide-prefix "+strings.Repeat("界", 200)),
		"Could not load this screen.",
	)
	unwrapped := resolved(t, screenkit.Kit{}, 0, state)
	detail := strings.Split(unwrapped, "\n")[3]
	if got := lipgloss.Width(detail); got > 240 || got < 238 {
		t.Fatalf("wide detail width = %d, want a boundary-safe result within the 240-cell budget: %q", got, detail)
	}
	if !strings.HasPrefix(detail, "wide-prefix ") || !strings.HasSuffix(detail, "…") {
		t.Fatalf("wide detail lost its useful prefix or ellipsis: %q", detail)
	}
	if strings.ContainsRune(detail, '\uFFFD') {
		t.Fatalf("wide glyph was split into a replacement rune: %q", detail)
	}

	wrapped := resolved(t, screenkit.Kit{}, 17, state)
	for i, row := range strings.Split(wrapped, "\n") {
		if got := lipgloss.Width(row); got > 17 {
			t.Fatalf("wrapped row %d is %d cells wide, exceeds 17: %q", i, got, row)
		}
	}
}

func TestFailedWithoutAnErrorIsNotLiveEvenWithAMessage(t *testing.T) {
	if state := screenstate.For("plans").Failed(nil, "Could not load this screen."); state.Live() {
		t.Fatalf("a nil error produced a live failure: %+v", state)
	}
}

// ---- the kicker contract ----------------------------------------------------

// A state with no kicker does not paint. Enforced rather than documented: the
// only exported painter is Resolve, and Resolve skips a candidate that is not
// Live, so there is no exported call that renders an unattributed panel.
func TestAStateWithNoKickerNeverPaints(t *testing.T) {
	kit := kitAt(t, 80, 24)
	blank := screenstate.For("   ")
	for name, state := range map[string]screenstate.State{
		"loading": blank.Loading(true, "Loading plans…"),
		"failed":  blank.Failed(errors.New("boom"), "Could not load this screen."),
		"vazio":   blank.Vazio(true, "No plans yet.", ""),
	} {
		if state.Live() {
			t.Errorf("%s: a kickerless state reports Live", name)
		}
		if body, ok := screenstate.Resolve(kit, 60, state); ok {
			t.Errorf("%s: painted without a kicker:\n%s", name, body)
		}
	}
	// A hand-written literal is the other way in, and it is closed too: the
	// package's own liveness marker is unexported, so a State a caller assembled
	// itself is inert whatever it carries.
	handmade := screenstate.State{Kicker: "plans", Message: "Loading plans…"}
	if handmade.Live() {
		t.Fatal("a State literal built outside the package reports Live")
	}
	if _, ok := screenstate.Resolve(kit, 60, handmade); ok {
		t.Fatal("a State literal built outside the package painted")
	}
}

func TestEveryLiveStateCarriesItsKickerWhicheverKindItIs(t *testing.T) {
	kit := kitAt(t, 120, 40)
	id := screenstate.For("comment view")
	for name, state := range map[string]screenstate.State{
		"loading": id.Loading(true, "Loading comment…"),
		"failed":  id.Failed(errors.New("boom"), "Could not load this screen."),
		"vazio":   id.Vazio(true, "Comment not found.", ""),
	} {
		body := resolved(t, kit, 0, state)
		if !strings.HasPrefix(body, "// COMMENT VIEW\n") {
			t.Errorf("%s state is unattributed:\n%s", name, body)
		}
	}
}

// ---- geometry ---------------------------------------------------------------

// 60x24 is the narrowest terminal the repo targets. Two things have to hold
// there: nothing paints past the fold, and the empty tone does not drag the
// body down to the kanban lane's fixed width — Styles.Empty is
// Width(columnWidth).Align(Center), which is the trap this component absorbs.
func TestNarrowTerminalFoldsInsteadOfOverflowing(t *testing.T) {
	kit := kitAt(t, 60, 24)
	width := kit.PanelContentWidth()
	if width <= 0 {
		t.Fatalf("panel content width = %d at 60 columns", width)
	}
	id := screenstate.For("plans")
	long := errors.New("dial tcp 127.0.0.1:5432: connect: connection refused after 3 attempts")
	cases := map[string]screenstate.State{
		"failed":  id.Failed(long, "Could not load this screen."),
		"loading": id.Loading(true, "Loading plans…"),
		"vazio":   id.Vazio(true, "No plans yet.", "Create one with `okt plan create <slug> --name \"...\"`."),
	}
	for name, state := range cases {
		body := resolved(t, kit, width, state)
		for i, row := range strings.Split(body, "\n") {
			if got := lipgloss.Width(row); got > width {
				t.Errorf("%s row %d is %d cells wide, past the %d-column budget: %q", name, i, got, width, row)
			}
		}
		if lines := strings.Count(body, "\n") + 1; lines < 3 {
			t.Errorf("%s folded away to %d rows:\n%s", name, lines, body)
		}
	}
}

// The long error has to actually fold, or the width assertion above is passing
// on content that never needed folding.
func TestTheNarrowCaseIsNotVacuous(t *testing.T) {
	kit := kitAt(t, 60, 24)
	state := screenstate.For("plans").Failed(errors.New("dial tcp 127.0.0.1:5432: connect: connection refused after 3 attempts"), "Could not load this screen.")
	unfolded := resolved(t, kit, 0, state)
	folded := resolved(t, kit, kit.PanelContentWidth(), state)
	if strings.Count(folded, "\n") <= strings.Count(unfolded, "\n") {
		t.Fatalf("the detail did not fold at 60 columns:\nunfolded:\n%s\nfolded:\n%s", unfolded, folded)
	}
}

// The empty state is the one that carried a geometry it had no business
// carrying. A Vazio body must be no wider than a Loading body of the same copy.
func TestTheEmptyToneDoesNotImposeTheKanbanLaneWidth(t *testing.T) {
	kit := kitAt(t, 120, 40)
	id := screenstate.For("plans")
	message := "No plans yet."
	vazio := resolved(t, kit, 0, id.Vazio(true, message, ""))
	loading := resolved(t, kit, 0, id.Loading(true, message))
	if vazio != loading {
		t.Fatalf("the empty tone changed the geometry:\nvazio:\n%q\nloading:\n%q", vazio, loading)
	}
	if strings.Contains(vazio, message+" ") {
		t.Fatalf("the empty body is padded out to a fixed width:\n%q", vazio)
	}
}
