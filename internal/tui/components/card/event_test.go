package card

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// eventPlain gives every feed card a border and no colour: these tests are about
// GEOMETRY and folding, and a test binary has no colour profile anyway.
func eventPlain() screenkit.Styles {
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	return screenkit.Styles{
		CommentCard:     box,
		SystemEventCard: box.Border(lipgloss.ThickBorder()),
		HintAccent:      lipgloss.NewStyle(),
		Hint:            lipgloss.NewStyle(),
		BadgeInfo:       lipgloss.NewStyle(),
	}
}

func eventPainter() Painter { return Painter{Styles: eventPlain()} }

// eventRows strips the border so an assertion can name the content.
func eventRows(rendered string) []string {
	all := strings.Split(rendered, "\n")
	if len(all) < 3 {
		return nil
	}
	out := make([]string, 0, len(all)-2)
	for _, row := range all[1 : len(all)-1] {
		out = append(out, strings.TrimSpace(strings.Trim(row, "│┃ ")))
	}
	return out
}

func TestTheHeaderDropsTheSeparatorWithTheTimestamp(t *testing.T) {
	p := eventPainter()
	withStamp := eventRows(p.Comment(Comment{Author: "user", Timestamp: "2026-04-11 08:14", Body: "hi", Width: 40}))
	if withStamp[0] != "user · 2026-04-11 08:14" {
		t.Errorf("header = %q, want author and stamp", withStamp[0])
	}
	// A blank stamp must not leave a dangling middot — the bug a caller writes
	// when it concatenates the separator unconditionally.
	without := eventRows(p.Comment(Comment{Author: "user", Body: "hi", Width: 40}))
	if without[0] != "user" {
		t.Errorf("header with no stamp = %q, want just the author", without[0])
	}
}

func TestALongBodyFoldsAndSaysHowMuchIsHidden(t *testing.T) {
	body := strings.Repeat("line\n", 20)
	got := eventRows(eventPainter().Comment(Comment{
		Author: "a", Body: body, Width: 40, MoreFmt: "↩ %d more",
	}))
	// header + DefaultLineLimit rows + the hint.
	if len(got) != 1+DefaultLineLimit+1 {
		t.Fatalf("folded card has %d content rows (%q), want %d", len(got), got, DefaultLineLimit+2)
	}
	if want := "↩ 14 more"; got[len(got)-1] != want {
		t.Errorf("fold hint = %q, want %q", got[len(got)-1], want)
	}
}

func TestTheFoldLimitIsConfigurableAndCanBeTurnedOff(t *testing.T) {
	body := strings.Repeat("line\n", 20)
	p := eventPainter()

	short := eventRows(p.Comment(Comment{Author: "a", Body: body, Width: 40, LineLimit: 2, MoreFmt: "+%d"}))
	if len(short) != 1+2+1 {
		t.Errorf("LineLimit 2 gave %d rows, want 4", len(short))
	}
	// Negative means never fold: the comment-detail screen shows the whole body.
	full := eventRows(p.Comment(Comment{Author: "a", Body: body, Width: 40, LineLimit: -1, MoreFmt: "+%d"}))
	if len(full) != 1+20 {
		t.Errorf("LineLimit -1 gave %d rows, want the whole body", len(full))
	}
	if strings.Contains(strings.Join(full, "\n"), "+") {
		t.Error("an unfolded card still drew the fold hint")
	}
}

func TestAnEmptyBodySaysSoInsteadOfLeavingAGap(t *testing.T) {
	got := eventRows(eventPainter().Comment(Comment{Author: "a", Body: "   ", EmptyText: "no comment", Width: 40}))
	if len(got) != 2 || got[1] != "no comment" {
		t.Errorf("empty card = %q, want the header and the empty text", got)
	}
}

func TestEveryRowFitsTheDeclaredWidth(t *testing.T) {
	p := eventPainter()
	for _, width := range []int{12, 20, 40, 80} {
		for name, rendered := range map[string]string{
			"comment": p.Comment(Comment{
				Author: "someone-with-a-long-name", Timestamp: "2026-04-11 08:14",
				Body:  "a body long enough to need wrapping at every one of these widths",
				Tags:  []string{"[tui]", "[layout]", "[regression]"},
				Width: width, MoreFmt: "+%d",
			}),
			"system": p.System(System{
				Label: "task moved from backlog to development", Timestamp: "2026-04-11 08:20", Width: width,
			}),
		} {
			for i, row := range strings.Split(rendered, "\n") {
				// Width feeds the box style, which sizes content + padding and
				// adds the border outside it.
				if got := lipgloss.Width(row); got != width+2 {
					t.Errorf("%s at width %d: row %d is %d columns", name, width, i, got)
				}
			}
		}
	}
}

// ContentWidth is exported so a caller measuring a card uses the same
// subtraction the card does. Both screens were doing their own.
func TestContentWidthHasAFloor(t *testing.T) {
	cases := map[int]int{40: 38, 12: 10, 10: 8, 4: 8, 0: 8}
	for width, want := range cases {
		if got := ContentWidth(width); got != want {
			t.Errorf("ContentWidth(%d) = %d, want %d", width, got, want)
		}
	}
}

func TestTheTwoKindsUseTheirOwnBorder(t *testing.T) {
	p := eventPainter()
	// Compared by RUNE: the box-drawing corners share a UTF-8 lead byte, so a
	// byte comparison here would pass without proving anything.
	corner := func(rendered string) rune { return []rune(rendered)[0] }
	if corner(p.Comment(Comment{Author: "a", Body: "b", Width: 30})) ==
		corner(p.System(System{Label: "c", Width: 30})) {
		t.Error("comment and system cards drew the same border — the feed cannot tell them apart")
	}
}

// ContentWidth floored what goes inside the card; the box around it did not, so
// a feed squeezed to nothing painted a border at the natural width of its
// longest comment. Both ends of the same width now share one floor.
func TestADegenerateFeedWidthDoesNotPaintTheCardAtItsNaturalWidth(t *testing.T) {
	for _, width := range []int{0, -1, -30} {
		rendered := eventPainter().Comment(Comment{
			Author: "howl", Timestamp: "2026-08-12",
			Body:  "a comment body long enough that an unconstrained box would be obvious",
			Width: width,
		})
		// Style.Width sizes content plus padding; the border sits outside it, so
		// the floored box occupies minBoxWidth plus its two border columns.
		budget := max(width, minBoxWidth) + 2
		for i, line := range strings.Split(rendered, "\n") {
			if w := lipgloss.Width(line); w > budget {
				t.Errorf("width %d row %d is %d cells, past the %d the floor promises: %q", width, i, w, budget, line)
			}
		}
	}
}

func TestEventSanitizesEveryPersistedFieldAtCompactAndWideWidths(t *testing.T) {
	const hostile = "event \x1b[31mred\x1b]0;owned\a\x00\x9b31m\x9d0;owned\x07漢字"
	for _, width := range []int{12, 60} {
		comment := eventPainter().Comment(Comment{
			Author:    hostile,
			Timestamp: hostile,
			Body:      hostile + "\nsecond line",
			EmptyText: hostile,
			Tags:      []string{hostile},
			MoreFmt:   hostile + " %d",
			LineLimit: 1,
			Width:     width,
		})
		system := eventPainter().System(System{Label: hostile, Timestamp: hostile, Width: width})
		for name, rendered := range map[string]string{"comment": comment, "system": system} {
			if strings.IndexFunc(strings.ReplaceAll(rendered, "\n", ""), unicode.IsControl) >= 0 {
				t.Fatalf("%s at width %d retained a terminal control: %q", name, width, rendered)
			}
			if !strings.Contains(rendered, "漢字") {
				t.Fatalf("%s at width %d lost harmless Unicode: %q", name, width, rendered)
			}
		}
	}
}
