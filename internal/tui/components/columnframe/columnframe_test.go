package columnframe

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/cardlist"
)

func fakeItems(n int, height int) []cardlist.Item {
	out := make([]cardlist.Item, n)
	for i := 0; i < n; i++ {
		lines := make([]string, height)
		lines[0] = "card #" + strconv.Itoa(i)
		for j := 1; j < height; j++ {
			lines[j] = "  body"
		}
		out[i] = cardlist.Item{Content: strings.Join(lines, "\n"), Height: height}
	}
	return out
}

func TestViewEmptyListRendersHeaderRuleEmptyState(t *testing.T) {
	m := Model{
		Header:    "# kicker",
		Rule:      "──",
		EmptyLine: "no items",
		List:      cardlist.New().WithViewport(10),
	}
	got := m.View(lipgloss.NewStyle())
	want := "# kicker\n──\nno items"
	if got != want {
		t.Fatalf("empty-state View =\n%q\nwant:\n%q", got, want)
	}
}

func TestViewEmptyListWithoutEmptyLineRendersHeaderRuleOnly(t *testing.T) {
	m := Model{
		Header: "# kicker",
		Rule:   "──",
		List:   cardlist.New().WithViewport(10),
	}
	got := m.View(lipgloss.NewStyle())
	want := "# kicker\n──"
	if got != want {
		t.Fatalf("empty-no-emptyline View =\n%q\nwant:\n%q", got, want)
	}
}

func TestViewFitsAllItemsBetweenHeaderAndBottom(t *testing.T) {
	m := Model{
		Header:    "# header",
		Rule:      "──",
		EmptyLine: "(empty)",
		List:      cardlist.New().WithViewport(30).WithItems(fakeItems(3, 4)),
	}
	got := m.View(lipgloss.NewStyle())
	if !strings.HasPrefix(got, "# header\n──\n") {
		t.Fatalf("View missing header+rule prefix: %q", got)
	}
	if !strings.Contains(got, "card #0") {
		t.Fatalf("View dropped first card: %q", got)
	}
	if !strings.Contains(got, "card #2") {
		t.Fatalf("View dropped last card: %q", got)
	}
	if strings.Contains(got, "▲") || strings.Contains(got, "▼") {
		t.Fatalf("View on fits-all rendered hints: %q", got)
	}
	if strings.Contains(got, "(empty)") {
		t.Fatalf("View on non-empty list rendered empty-line: %q", got)
	}
}

// TestViewGoldenScrollAcrossCursorPositions pins the exact rendered
// output at every cursor position for a fixed dataset. Any slice-
// math regression in cardlist or columnframe diffs visibly in this
// golden table.
func TestViewGoldenScrollAcrossCursorPositions(t *testing.T) {
	const viewport = 12
	items := fakeItems(5, 4)

	type row struct {
		name        string
		mutations   func(cardlist.Model) cardlist.Model
		wantTopHint bool
		wantBotHint bool
	}
	rows := []row{
		{
			name:        "cursor at first",
			mutations:   func(m cardlist.Model) cardlist.Model { return m.JumpFirst() },
			wantTopHint: false,
			wantBotHint: true,
		},
		{
			name:        "cursor at last",
			mutations:   func(m cardlist.Model) cardlist.Model { return m.JumpLast() },
			wantTopHint: true,
			wantBotHint: false,
		},
		{
			// Cursor at index 2 with 5×height-4 cards + viewport=12:
			// scroll advances to 1 so the cursor fits inside the
			// HintsSplit slice (cards 1 and 2 visible). Both hints
			// fire because items remain hidden above (item 0) and
			// below (items 3, 4).
			name:        "cursor in middle",
			mutations:   func(m cardlist.Model) cardlist.Model { return m.JumpFirst().MoveCursor(2) },
			wantTopHint: true,
			wantBotHint: true,
		},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{
				Header: "# kicker",
				Rule:   "──",
				List:   tc.mutations(cardlist.New().WithViewport(viewport).WithItems(items)),
			}
			got := m.View(lipgloss.NewStyle())
			hasTop := strings.Contains(got, "▲")
			hasBot := strings.Contains(got, "▼")
			if hasTop != tc.wantTopHint {
				t.Errorf("top hint = %v, want %v\noutput:\n%s", hasTop, tc.wantTopHint, got)
			}
			if hasBot != tc.wantBotHint {
				t.Errorf("bottom hint = %v, want %v\noutput:\n%s", hasBot, tc.wantBotHint, got)
			}
			// Cursor must always end up inside the rendered slice —
			// the very property the columnframe + cardlist pairing
			// guarantees structurally.
			activeIdx := m.List.Cursor()
			active := items[activeIdx].Content
			activeFirstLine := strings.SplitN(active, "\n", 2)[0]
			if !strings.Contains(got, activeFirstLine) {
				t.Errorf("cursor card %q missing from rendered output:\n%s", activeFirstLine, got)
			}
		})
	}
}

func TestViewWithVariableHeightItems(t *testing.T) {
	items := []cardlist.Item{
		{Content: "card-a\nline-a", Height: 2},
		{Content: "card-b", Height: 1},
		{Content: "card-c\nl1\nl2\nl3", Height: 4},
		{Content: "card-d", Height: 1},
	}
	m := Model{
		Header: "h",
		Rule:   "r",
		List:   cardlist.New().WithViewport(20).WithItems(items),
	}
	got := m.View(lipgloss.NewStyle())
	for _, want := range []string{"card-a", "card-b", "card-c", "card-d"} {
		if !strings.Contains(got, want) {
			t.Fatalf("View dropped item %q:\n%s", want, got)
		}
	}
}
