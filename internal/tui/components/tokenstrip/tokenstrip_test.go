package tokenstrip

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"omakiten/internal/tui/components/screenkit"
)

// plain is a Styles whose pills add no ANSI and no padding, so a test can
// assert on the text a caller would read rather than on escape sequences.
func plain() screenkit.Styles {
	s := lipgloss.NewStyle()
	return screenkit.Styles{
		BadgeHigh: s, BadgeFix: s, BadgeNormal: s, BadgeInfo: s, BadgeLow: s,
		BadgeBlocker: s, BadgeComment: s, BadgeSubtask: s, BadgeScope: s, BadgeActive: s,
		TokenGreen: s, TokenYellow: s, TokenRed: s,
	}
}

func chipStyles() ChipStyles {
	s := lipgloss.NewStyle()
	return ChipStyles{Kicker: s, Active: s, Inactive: s, Hint: s, Sep: s}
}

func keyStyles() KeyStyles {
	s := lipgloss.NewStyle()
	return KeyStyles{Primary: s, Secondary: s}
}

func TestFromStylesMapsHintAccentAndBorder(t *testing.T) {
	primary := lipgloss.NewStyle().Foreground(lipgloss.Color("#39FF14")).Padding(0, 2)
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("#494543")).Border(lipgloss.NormalBorder())
	got := FromStyles(screenkit.Styles{HintAccent: primary, Border: border})
	if got.Primary.GetForeground() != primary.GetForeground() {
		t.Fatalf("Primary colour = %v, want HintAccent", got.Primary.GetForeground())
	}
	if !got.Primary.GetBold() {
		t.Fatal("Primary must be bold")
	}
	if got.Secondary.GetForeground() != border.GetForeground() {
		t.Fatalf("Secondary colour = %v, want Border", got.Secondary.GetForeground())
	}
	if got.Secondary.GetBorderStyle() != (lipgloss.Border{}) {
		t.Fatal("Secondary must not inherit Border geometry")
	}
	if got.Primary.GetHorizontalPadding() != 0 {
		t.Fatal("Primary must not inherit HintAccent padding")
	}
}

// ---- the packer ------------------------------------------------------------

// The break points are the whole contract: one column of separator between
// tokens, and a token that does not fit starts a row instead of being cut.
func TestTheWrapBreaksOnlyWhenTheNextTokenWouldOverflow(t *testing.T) {
	cases := []struct {
		name   string
		tokens []string
		width  int
		bands  []string
	}{
		{"everything on one row", []string{"aa", "bb"}, 20, []string{"aa bb"}},
		// "aaa bbb" is exactly 7 cells. A packer that charged the separator
		// twice, or measured before adding it, would break here.
		{"exact fit stays on one row", []string{"aaa", "bbb"}, 7, []string{"aaa bbb"}},
		{"one column short breaks", []string{"aaa", "bbb"}, 6, []string{"aaa", "bbb"}},
		{"a token wider than the budget gets its own row rather than a cut",
			[]string{"ab", "wwwwwwwwww", "cd"}, 5, []string{"ab", "wwwwwwwwww", "cd"}},
		{"packing is greedy, not balanced",
			[]string{"aa", "bb", "cc", "dd"}, 8, []string{"aa bb cc", "dd"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Bands are joined by a blank row: pills carry a background, and two
			// bands stacked directly read as one block of colour.
			want := strings.Join(c.bands, "\n\n")
			if got := Pills(c.tokens, c.width); got != want {
				t.Errorf("Pills(%q, %d) = %q, want %q", c.tokens, c.width, got, want)
			}
		})
	}
}

// The measurer and the painter disagreeing is the defect this package exists to
// make unrepresentable — a card reserves rows from one and paints with the
// other. Asserted over a width sweep rather than a hand-picked case.
func TestTheMeasurerCountsExactlyWhatThePainterDraws(t *testing.T) {
	tokens := []string{"aa", "bbbb", "c", "ddddddddddddddd", "ee", "f"}
	for width := 1; width <= 40; width++ {
		painted := Pills(tokens, width)
		drawn := 0
		if painted != "" {
			drawn = strings.Count(painted, "\n") + 1
		}
		if got := PillRows(tokens, width); got != drawn {
			t.Errorf("width %d: PillRows = %d but Pills drew %d rows", width, got, drawn)
		}
	}
	if got := PillRows(nil, 20); got != 0 {
		t.Errorf("PillRows(nil) = %d, want 0 — no tokens is no rows, not an empty row", got)
	}
}

// One band is the board's card, and it must not pay for a separator it has no
// second band to be separated from.
func TestASingleBandCostsExactlyOneRow(t *testing.T) {
	if got := PillRows([]string{"FIX"}, 200); got != 1 {
		t.Errorf("PillRows = %d, want 1", got)
	}
	if got := Pills([]string{"FIX"}, 200); strings.Contains(got, "\n") {
		t.Errorf("Pills emitted more than one row: %q", got)
	}
}

// ---- chips: the Drop policy ------------------------------------------------

// A chip row keeps one row and loses tokens instead. What it may never lose is
// the chip the reader is standing on.
func TestChipsDropTheFarthestAndNeverTheActiveOne(t *testing.T) {
	chips := []Chip{
		{Label: "all"}, {Label: "errors", Active: true}, {Label: "guards"}, {Label: "tools"},
	}
	for width := 6; width <= 40; width++ {
		out := Chips(chips, chipStyles(), ChipOptions{Width: width, Hint: "· filter"})
		if strings.Contains(out, "\n") {
			t.Fatalf("width %d: a chip row must stay one row:\n%s", width, out)
		}
		if got := lipgloss.Width(out); got > width {
			t.Errorf("width %d: painted %d cells: %q", width, got, out)
		}
		if !strings.Contains(out, "errors") && !strings.Contains(out, "…") {
			t.Errorf("width %d dropped the active chip: %q", width, out)
		}
	}
}

// The hint is advisory, so it is the first thing to go — before any chip.
func TestChipsLoseTheHintBeforeAChip(t *testing.T) {
	chips := []Chip{{Label: "all"}, {Label: "errors", Active: true}}
	full := Chips(chips, chipStyles(), ChipOptions{Hint: "· filter"})
	narrow := Chips(chips, chipStyles(), ChipOptions{Hint: "· filter", Width: lipgloss.Width(full) - 2})
	if strings.Contains(narrow, "filter") {
		t.Errorf("the hint survived a budget that could not hold it: %q", narrow)
	}
	for _, label := range []string{"all", "errors"} {
		if !strings.Contains(narrow, label) {
			t.Errorf("chip %q was dropped before the hint: %q", label, narrow)
		}
	}
}

func TestChipsSanitizeLabelsAndAdvisoryText(t *testing.T) {
	hostile := "filter 漢字 \x1b[31mred\x1b]0;owned\a C1\u009b31m\u009d"
	got := Chips([]Chip{{Label: hostile, Active: true}}, chipStyles(), ChipOptions{
		Kicker: hostile, Hint: hostile, Sep: hostile, Width: 120,
	})
	if strings.Contains(got, "owned") {
		t.Fatalf("chips retained OSC payload: %q", got)
	}
	if strings.IndexFunc(got, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("chips retained terminal control: %q", got)
	}
	if !strings.Contains(got, "漢字") {
		t.Fatalf("chips lost harmless Unicode: %q", got)
	}
}

// ---- keys -------------------------------------------------------------------

func TestKeysWrapStaysInsideTheWidth(t *testing.T) {
	keys := []Key{
		{Key: "j/k", Label: "move", Primary: true},
		{Key: "enter", Label: "open", Primary: true},
		{Key: "esc", Label: "back"},
		{Key: "?", Label: "help"},
	}
	for width := 10; width <= 80; width += 5 {
		painted := KeysWrapped(keys, keyStyles(), width)
		for i, row := range strings.Split(painted, "\n") {
			if got := lipgloss.Width(row); got > width {
				t.Errorf("width %d row %d is %d cells: %q", width, i, got, row)
			}
		}
	}
}

// A blank key is not a binding. It used to be filtered in one family only.
func TestABlankKeyIsNotPainted(t *testing.T) {
	got := Keys([]Key{{Key: " ", Label: "ghost"}, {Key: "q", Label: "quit"}}, keyStyles())
	if strings.Contains(got, "ghost") {
		t.Errorf("a key with no spelling was painted: %q", got)
	}
}

func TestKeysSanitizeConfigDerivedKeysAndLabelsAtCompactAndWideWidths(t *testing.T) {
	const hostile = "label \x1b[31mred\x1b]0;owned\a\x00\x9b31m\x9d0;owned\x07漢字"
	for _, width := range []int{10, 48} {
		got := KeysWrapped([]Key{{Key: hostile, Label: hostile}, {Key: "q", Label: "quit"}}, keyStyles(), width)
		if strings.IndexFunc(strings.ReplaceAll(got, "\n", ""), unicode.IsControl) >= 0 {
			t.Fatalf("width %d retained a terminal control: %q", width, got)
		}
		if width == 48 && !strings.Contains(got, "漢字") {
			t.Fatalf("width %d lost harmless Unicode: %q", width, got)
		}
	}
	if got := Keys([]Key{{Key: "\x1b[31m\x00\x9b", Label: "ghost"}, {Key: "q", Label: "quit"}}, keyStyles()); strings.Contains(got, "ghost") {
		t.Fatalf("sanitized-empty key kept its label: %q", got)
	}
}

// ---- pills: the constructors ----------------------------------------------

func TestAPillIsEmptyWhenItHasNothingToSay(t *testing.T) {
	s := plain()
	if got := Pill(s, "error", "  "); got != "" {
		t.Errorf("Pill with a blank value = %q, want empty — an empty pill is two cells of background that read as a bug", got)
	}
	if got := Scope(s, ""); got != "" {
		t.Errorf("Scope with no label = %q, want empty", got)
	}
	if got := Count(lipgloss.NewStyle(), func(k string) string { return k }, 0, "s", "p"); got != "" {
		t.Errorf("Count at zero = %q, want empty — a card says nothing about the blockers it does not have", got)
	}
}

func TestPillLeavesSanitizePersistedLabelsWithoutDroppingUnicode(t *testing.T) {
	const hostile = "label \x1b[31mred\x1b]0;owned\a\x00\x9b31m\x9d0;owned\x07漢字"
	styles := plain()
	constructors := map[string]string{
		"pill":       Pill(styles, "info", hostile),
		"count band": CountBand(styles, 2, hostile),
		"label":      Label(styles, hostile),
		"tag":        Tag(styles, hostile),
		"scope":      Scope(styles, hostile),
		"count":      Count(lipgloss.NewStyle(), func(string) string { return hostile }, 1, "one", "many"),
	}
	for name, rendered := range constructors {
		if strings.IndexFunc(rendered, unicode.IsControl) >= 0 {
			t.Errorf("%s retained a terminal control: %q", name, rendered)
		}
		if !strings.Contains(rendered, "漢字") {
			t.Errorf("%s lost harmless Unicode: %q", name, rendered)
		}
	}
	if got := Scope(styles, "\x1b[31m\x00\x9b"); got != "" {
		t.Fatalf("scope made empty by sanitization = %q, want empty", got)
	}
}

// The colour token is a stable enum from YAML, and an unknown one must still
// paint something: an unstyled badge reads as body text.
func TestAnUnknownColourTokenFallsBackRatherThanPaintingNothing(t *testing.T) {
	s := plain()
	for _, token := range []string{"", "chartreuse", "ERROR", " warning "} {
		if got := Pill(s, token, "high"); got != "HIGH" {
			t.Errorf("Pill(%q) = %q, want the uppercased label", token, got)
		}
	}
}

func TestCountsKeepCardOrderAndSkipTheEmptyOnes(t *testing.T) {
	tr := func(k string) string { return k }
	got := Counts{Blockers: 2, Subtasks: 1}.Render(plain(), tr)
	if len(got) != 2 {
		t.Fatalf("got %d pills, want 2 — the zero comment count must not take a slot: %q", len(got), got)
	}
	if !strings.HasPrefix(got[0], "2 ") || !strings.HasPrefix(got[1], "1 ") {
		t.Errorf("card order broken: %q", got)
	}
}

// TestPaintColumnMatchesPerEntryRender is the proof behind the one-Render
// column: painting a block and splitting it must produce, byte for byte, what
// painting each entry separately produced. It runs across colour profiles
// because the goldens only ever see Ascii, and the profile is exactly what
// decides whether a style emits terminal codes at all.
func TestPaintColumnMatchesPerEntryRender(t *testing.T) {
	defer lipgloss.SetColorProfile(termenv.Ascii)
	entries := []string{"q", "", "↑/↓", " move the cursor", "enter"}
	styles := map[string]lipgloss.Style{
		"bare":       lipgloss.NewStyle(),
		"foreground": lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		"accent":     lipgloss.NewStyle().Foreground(lipgloss.Color("#39FF14")).Bold(true),
	}
	profiles := map[string]termenv.Profile{
		"truecolor": termenv.TrueColor,
		"ansi":      termenv.ANSI,
		"ascii":     termenv.Ascii,
	}
	for profileName, profile := range profiles {
		for styleName, style := range styles {
			t.Run(profileName+"/"+styleName, func(t *testing.T) {
				assertPaintColumn(t, profile, style, entries)
			})
		}
	}
	if got := paintColumn(lipgloss.NewStyle(), nil); got != nil {
		t.Errorf("paintColumn(nil) = %q, want nil", got)
	}
}

func assertPaintColumn(t *testing.T, profile termenv.Profile, style lipgloss.Style, entries []string) {
	lipgloss.SetColorProfile(profile)
	want := make([]string, len(entries))
	for i, entry := range entries {
		want[i] = style.Render(entry)
	}
	got := paintColumn(style, entries)
	if len(got) != len(want) {
		t.Fatalf("paintColumn returned %d entries, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}
