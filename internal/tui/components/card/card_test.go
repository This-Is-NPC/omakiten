package card

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// plain is a theme with borders but no colour: the tests are about GEOMETRY,
// and a card's geometry is what a border costs and where the text lands.
func plain() screenkit.Styles {
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	return screenkit.Styles{
		Card:         box,
		CardSelected: box,
		CardArchived: box,
		Marker:       lipgloss.NewStyle(),
		Info:         lipgloss.NewStyle(),
		BadgeInfo:    lipgloss.NewStyle(),
	}
}

func painter() Painter { return Painter{Styles: plain()} }

// content strips the border rows and columns so an assertion can name the text
// the card holds rather than the box around it.
func content(rendered string) []string {
	rows := strings.Split(rendered, "\n")
	if len(rows) < 3 {
		return nil
	}
	inner := make([]string, 0, len(rows)-2)
	for _, row := range rows[1 : len(rows)-1] {
		inner = append(inner, strings.Trim(row, "│ "))
	}
	return inner
}

func TestTheTitleHangsUnderThePrefix(t *testing.T) {
	got := content(painter().Render(Spec{
		ID: 412, Title: "resolve the layout exactly once per keystroke",
		BoxWidth: 30, InnerWidth: 26,
	}))
	want := []string{
		"#412 resolve the layout",
		"exactly once per",
		"keystroke",
	}
	// The wrapped rows are indented under the prefix in the rendered card; the
	// helper trims that, so this asserts the WRAP POINT — every row after the
	// first is budgeted as if the prefix were still there.
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("card content = %q, want %q", got, want)
	}

	rendered := painter().Render(Spec{
		ID: 412, Title: "resolve the layout exactly once per keystroke",
		BoxWidth: 30, InnerWidth: 26,
	})
	// BoxWidth feeds the lipgloss style, which sizes content + padding and adds
	// the border outside it, so a card declared at 30 occupies 32 columns. Every
	// row occupies the same number of them.
	for i, row := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(row); w != 32 {
			t.Errorf("row %d is %d columns wide, want 30 + the two border columns", i, w)
		}
	}
}

func TestAnUnbreakableTitleIsCutRatherThanOverflowed(t *testing.T) {
	// The defect this package closes: one of the two task-card copies packed
	// words without hard-wrapping, so a title with no spaces painted straight
	// through the card edge.
	spec := Spec{
		ID:       7,
		Title:    strings.Repeat("x", 60),
		BoxWidth: 24, InnerWidth: 20,
	}
	for i, row := range strings.Split(painter().Render(spec), "\n") {
		if w := lipgloss.Width(row); w != 26 {
			t.Fatalf("row %d is %d columns wide, want 24 + border — the token escaped the box", i, w)
		}
	}
}

func TestTheMeasurerRunsTheSameLayoutTheRendererDoes(t *testing.T) {
	p := painter()
	specs := []Spec{
		{ID: 1, Title: "short", BoxWidth: 30, InnerWidth: 26},
		{ID: 22, Title: "a title long enough to wrap across several rows of a narrow lane",
			BoxWidth: 24, InnerWidth: 20},
		{Title: "no id prefix at all", BoxWidth: 30, InnerWidth: 26},
		{ID: 3, Title: "with metadata", Meta: []string{"@howl", "", "2 hours ago"},
			BoxWidth: 30, InnerWidth: 26},
		{ID: 4, Title: "badged", Badges: []string{"[HIGH]", "[2 blockers]", "[1 comment]"},
			BoxWidth: 24, InnerWidth: 20},
		{ID: 5, Title: strings.Repeat("y", 40), BoxWidth: 18, InnerWidth: 14},
		{ID: 6, Title: "", BoxWidth: 30, InnerWidth: 26},
	}
	for i, spec := range specs {
		drawn := strings.Count(p.Render(spec), "\n") + 1
		if got := p.Height(spec); got != drawn {
			t.Errorf("spec %d: Height = %d but Render drew %d rows", i, got, drawn)
		}
	}
}

// The chevron is Cursor, not Selected. Two surfaces disagree on purpose: the
// board draws both, the settings grid draws the border only.
func TestTheChevronFollowsCursorAndNotSelection(t *testing.T) {
	p := painter()
	if rows := content(p.Render(Spec{ID: 9, Title: "t", Selected: true, Cursor: true, BoxWidth: 24, InnerWidth: 20})); rows[0] != "› #9 t" {
		t.Errorf("cursor row = %q, want the chevron before the id", rows[0])
	}
	if rows := content(p.Render(Spec{ID: 9, Title: "t", Selected: true, BoxWidth: 24, InnerWidth: 20})); rows[0] != "#9 t" {
		t.Errorf("selected-without-cursor row = %q, want no chevron", rows[0])
	}
	if rows := content(p.Render(Spec{ID: 9, Title: "t", BoxWidth: 24, InnerWidth: 20})); rows[0] != "#9 t" {
		t.Errorf("idle row = %q, want no chevron and no padding standing in for one", rows[0])
	}

	// The chevron costs the title two columns, so a cursor card can wrap one row
	// further. A measurer that ignored the prefix would report the same height.
	long := "a title that only just fits on one row here"
	idle := p.Height(Spec{ID: 9, Title: long, BoxWidth: 24, InnerWidth: 20})
	if cursor := p.Height(Spec{ID: 9, Title: long, Cursor: true, BoxWidth: 24, InnerWidth: 20}); cursor < idle {
		t.Errorf("cursor card is %d rows and idle is %d — the chevron cannot make a card shorter", cursor, idle)
	}
}

// A card with no id is the project and entity case; it must not leave the gap
// where the prefix would have been.
func TestACardWithoutAnIDStartsAtTheEdge(t *testing.T) {
	rows := content(painter().Render(Spec{Title: "omakiten", BoxWidth: 30, InnerWidth: 26}))
	if rows[0] != "omakiten" {
		t.Errorf("first row = %q, want the title flush", rows[0])
	}
}

func TestMetadataIsTruncatedRatherThanWrapped(t *testing.T) {
	// Metadata that wraps is metadata taking rows from the title. A card is a
	// fixed budget in a column, so the secondary line loses.
	spec := Spec{
		Title: "t", Meta: []string{"a-very-long-path/that/will/not/fit/in/the/card"},
		BoxWidth: 24, InnerWidth: 20,
	}
	rows := content(painter().Render(spec))
	if len(rows) != 2 {
		t.Fatalf("card has %d content rows (%q), want the title plus exactly one meta row", len(rows), rows)
	}
	if !strings.HasSuffix(rows[1], "…") {
		t.Errorf("meta row = %q, want it cut with an ellipsis", rows[1])
	}
}

func TestTheCacheAndTheNilCacheRenderTheSameCard(t *testing.T) {
	spec := Spec{ID: 3, Title: "cached", BoxWidth: 30, InnerWidth: 26}
	uncached := Painter{Styles: plain()}
	cached := Painter{Styles: plain(), Cache: NewCache()}
	if cached.Render(spec) != uncached.Render(spec) {
		t.Error("the cached painter drew a different card")
	}
	// Second call comes off the cache; it must still be the same card.
	if cached.Render(spec) != uncached.Render(spec) {
		t.Error("the cached painter drew a different card on the cache hit")
	}
	if len(cached.Cache) == 0 {
		t.Error("nothing was memoised, so the cache is not being consulted")
	}
}

// Two variants at the SAME width must not collide in the cache. Without the
// variant in the key, whichever card rendered first would hand its border to
// every card after it — a regression this repo has already shipped once.
func TestTheCacheKeepsTheVariantsApartAtOneWidth(t *testing.T) {
	// Differentiated by BORDER GLYPH rather than colour: a test binary has no
	// colour profile, so a tone-only difference renders identically and would
	// make this pass for the wrong reason.
	styles := plain()
	styles.Card = styles.Card.Border(lipgloss.NormalBorder())
	styles.CardSelected = styles.CardSelected.Border(lipgloss.ThickBorder())
	styles.CardArchived = styles.CardArchived.Border(lipgloss.DoubleBorder())
	p := Painter{Styles: styles, Cache: NewCache()}

	base := Spec{ID: 1, Title: "same title", BoxWidth: 24, InnerWidth: 20}
	variants := map[string]Spec{
		"plain":    base,
		"selected": func(s Spec) Spec { s.Selected = true; return s }(base),
		"archived": func(s Spec) Spec { s.Archived = true; return s }(base),
	}

	// Two passes: the first fills the cache, the second reads it. A key that
	// dropped the variant serves the first-rendered style to everyone, which
	// only shows on the second pass.
	seen := map[string]string{}
	assertVariantCache(t, p, variants, seen)

	// Accent differs from plain only in border COLOUR, so it cannot be told
	// apart by rendering here — but it must still take its own cache slot, or a
	// plain card rendered first would hand it the untinted border.
	accent := base
	accent.Accent = true
	p.Render(accent)
	if len(p.Cache) != len(variants)+1 {
		t.Errorf("cache holds %d entries, want one per variant (%d)", len(p.Cache), len(variants)+1)
	}
}

func assertVariantCache(t *testing.T, p Painter, variants map[string]Spec, seen map[string]string) {
	for pass := 0; pass < 2; pass++ {
		for name, spec := range variants {
			rendered := p.Render(spec)
			for other, earlier := range seen {
				if other != name && earlier == rendered {
					t.Fatalf("pass %d: %q and %q rendered identically — the cache key lost the variant", pass, name, other)
				}
			}
			if earlier, ok := seen[name]; ok && earlier != rendered {
				t.Fatalf("pass %d: %q changed on the cache hit", pass, name)
			}
			seen[name] = rendered
		}
	}
}

// A degenerate box width reaches lipgloss as `Width(n)`, and lipgloss reads a
// non-positive n as "no constraint" — so the failure mode is not a panic, it is
// a card that quietly paints at the NATURAL width of its title inside a lane
// that has none to spare.
//
// What the floor buys is that the card folds instead of running free. It does
// not buy an exact width: below a certain point the content itself is the
// constraint — an id prefix and a badge do not break — so the assertion is the
// property that matters, not a number the test would be pinning to whatever
// the last run happened to produce.
func TestADegenerateBoxWidthFoldsInsteadOfPaintingUnconstrained(t *testing.T) {
	const title = "a title far wider than the box it was given"
	for _, boxWidth := range []int{0, -1, -40} {
		spec := Spec{ID: 412, Title: title, BoxWidth: boxWidth, InnerWidth: boxWidth}
		rendered := painter().Render(spec)
		widest := 0
		for _, line := range strings.Split(rendered, "\n") {
			if w := lipgloss.Width(line); w > widest {
				widest = w
			}
		}
		if widest >= lipgloss.Width(title) {
			t.Errorf("box width %d painted %d cells — the title never folded:\n%s", boxWidth, widest, rendered)
		}
		// Height runs the same line builder Render does, and that has to hold at
		// the floor as well: a column budgeting against it gets what it gets.
		if got, want := painter().Height(spec), lipgloss.Height(rendered); got != want {
			t.Errorf("box width %d: Height reported %d, Render painted %d", boxWidth, got, want)
		}
	}
}

func TestCardSanitizesPersistedTitleAndMetadataAtCompactAndWideWidths(t *testing.T) {
	const hostile = "task \x1b[31mred\x1b]0;owned\a\x009b\x9b31m\x9d0;owned\x07漢字"
	for _, width := range []int{12, 40} {
		rendered := painter().Render(Spec{
			Title:      hostile,
			Meta:       []string{hostile},
			BoxWidth:   width,
			InnerWidth: width - 4,
		})
		if strings.IndexFunc(strings.ReplaceAll(rendered, "\n", ""), unicode.IsControl) >= 0 {
			t.Fatalf("width %d retained a terminal control: %q", width, rendered)
		}
		if width == 40 && !strings.Contains(rendered, "漢字") {
			t.Fatalf("width %d lost harmless Unicode: %q", width, rendered)
		}
	}
}
