package screenkit

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// TruncatePath keeps the TAIL, because the tail is the part a reader can act on:
// `/home/howl/Proj…` identifies nothing, `…/person/omakiten` names the project.
func TestTruncatePathKeepsTheEndAndStaysInsideTheBudget(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
		want  string
	}{
		{"fits untouched", "/a/b", 10, "/a/b"},
		{"drops leading segments on a separator", "/alpha/bravo/charlie", 12, "…/charlie"},
		{"too narrow for anything", "/alpha/bravo", 3, "…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TruncatePath(c.value, c.width); got != c.want {
				t.Errorf("TruncatePath(%q, %d) = %q, want %q", c.value, c.width, got, c.want)
			}
		})
	}
}

// A wide glyph is two cells, so a cut that counts runes can render at twice its
// budget, and a cut that slices bytes can split a rune in half.
func TestTruncatePathCutsOnCellBoundariesNotBytes(t *testing.T) {
	path := "/home/user/" + strings.Repeat("漢", 30)
	for _, width := range []int{6, 9, 12, 15} {
		got := TruncatePath(path, width)
		switch {
		case !utf8.ValidString(got):
			t.Errorf("width %d produced invalid UTF-8: %q", width, got)
		case lipgloss.Width(got) > width:
			t.Errorf("width %d rendered %d cells: %q", width, lipgloss.Width(got), got)
		case !strings.HasPrefix(got, "…"):
			t.Errorf("width %d = %q, want the head marked as dropped", width, got)
		}
	}
}

// The two measurements exist so a caller above this package never imports
// lipgloss to ask a question about a string. What they have to get right is the
// two cases a byte count gets wrong: styled input, whose escapes are stored and
// not drawn, and wide glyphs, which are one rune and two cells.
func TestVisibleWidthMeasuresCellsAndNotBytes(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#39FF14")).Render("abc")
	for _, c := range []struct {
		name  string
		block string
		want  int
	}{
		{"plain", "abc", 3},
		{"styled counts only what is drawn", styled, 3},
		{"a wide glyph is two cells", "漢", 2},
		{"the widest line wins", "a\nbbbb\ncc", 4},
		{"nothing is zero", "", 0},
	} {
		if got := VisibleWidth(c.block); got != c.want {
			t.Errorf("%s: VisibleWidth(%q) = %d, want %d", c.name, c.block, got, c.want)
		}
	}
}

func TestBlockRowsCountsTerminalRows(t *testing.T) {
	for _, c := range []struct {
		name  string
		block string
		want  int
	}{
		{"one line", "abc", 1},
		{"a trailing newline is an empty row", "abc\n", 2},
		{"three rows", "a\nb\nc", 3},
		{"the empty block still occupies its row", "", 1},
	} {
		if got := BlockRows(c.block); got != c.want {
			t.Errorf("%s: BlockRows(%q) = %d, want %d", c.name, c.block, got, c.want)
		}
	}
}

func TestTruncateStaysInsideTheBudget(t *testing.T) {
	for _, c := range []struct {
		value string
		width int
	}{{"abcdef", 1}, {"abcdef", 4}, {"ok", 4}, {"abcdef", 0}, {"漢漢漢", 4}} {
		got := Truncate(c.value, c.width)
		if c.width > 0 && lipgloss.Width(got) > c.width {
			t.Errorf("Truncate(%q, %d) = %q at %d cells", c.value, c.width, got, lipgloss.Width(got))
		}
		if c.width <= 0 && got != "" {
			t.Errorf("Truncate(%q, %d) = %q, want nothing", c.value, c.width, got)
		}
	}
}
