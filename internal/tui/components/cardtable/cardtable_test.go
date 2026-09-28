package cardtable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColsFitsCellsWithAOneColumnGutter(t *testing.T) {
	if got := Cols(89, 28); got != 3 {
		t.Errorf("Cols(89, 28) = %d, want 3 — (89+1)/(28+1)", got)
	}
	if got := Cols(30, 28); got != 1 {
		t.Errorf("Cols(30, 28) = %d, want 1", got)
	}
	if got := Cols(10, 28); got != 1 {
		t.Errorf("Cols(10, 28) = %d, want 1 — narrower than one cell still shows one column", got)
	}
}

func TestColsIsAtLeastOne(t *testing.T) {
	if got := Cols(0, 28); got != 1 {
		t.Errorf("Cols(0, 28) = %d, want 1", got)
	}
	if got := Cols(-4, 28); got != 1 {
		t.Errorf("Cols(-4, 28) = %d, want 1", got)
	}
}

func TestRowJoinsWithAOneSpaceGutter(t *testing.T) {
	got := Row([]string{"AA", "BB"})
	if got != "AA BB" {
		t.Errorf("Row = %q, want %q", got, "AA BB")
	}
}

func TestRowIsEmptyWhenThereAreNoCards(t *testing.T) {
	if got := Row(nil); got != "" {
		t.Errorf("Row(nil) = %q, want empty", got)
	}
	if got := Row([]string{}); got != "" {
		t.Errorf("Row(empty) = %q, want empty", got)
	}
}

func TestRowTopAlignsUnevenCards(t *testing.T) {
	tall := "X\nY"
	got := Row([]string{tall, "Z"})
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("joined height = %d, want 2 (the taller card)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "X") || !strings.Contains(lines[0], "Z") {
		t.Errorf("top row = %q, want X beside Z", lines[0])
	}
}

func TestRowHeightIsTheTallestCard(t *testing.T) {
	if got := RowHeight(nil); got != 1 {
		t.Errorf("RowHeight(nil) = %d, want 1", got)
	}
	if got := RowHeight([]string{"a", "b\nc\nd"}); got != 3 {
		t.Errorf("RowHeight = %d, want 3", got)
	}
}

func TestLayoutSplitsIntoRowsOfCols(t *testing.T) {
	cards := []string{"A", "B", "C", "D", "E"}
	rows, heights := Layout(cards, 2)
	if len(rows) != 3 || len(heights) != 3 {
		t.Fatalf("Layout len = %d/%d, want 3 rows", len(rows), len(heights))
	}
	if rows[0] != "A B" || rows[1] != "C D" || rows[2] != "E" {
		t.Errorf("rows = %q", rows)
	}
	for i, h := range heights {
		if h != 1 {
			t.Errorf("heights[%d] = %d, want 1", i, h)
		}
	}
}

func TestLayoutOneColumnIsEachCardAlone(t *testing.T) {
	cards := []string{"A", "B"}
	rows, heights := Layout(cards, 1)
	if len(rows) != 2 || rows[0] != "A" || rows[1] != "B" {
		t.Errorf("one-column Layout = %q", rows)
	}
	if len(heights) != 2 {
		t.Errorf("heights len = %d, want 2", len(heights))
	}
}

func TestLayoutEmptyAndNonPositiveCols(t *testing.T) {
	rows, heights := Layout(nil, 3)
	if len(rows) != 0 || len(heights) != 0 {
		t.Errorf("empty Layout = %q / %v", rows, heights)
	}
	rows, _ = Layout([]string{"A", "B"}, 0)
	if len(rows) != 2 {
		t.Errorf("cols 0 treated as 1: got %d rows, want 2", len(rows))
	}
}

func TestGutterIsOneColumn(t *testing.T) {
	left := lipgloss.NewStyle().Width(4).Height(1).Render("aa")
	right := lipgloss.NewStyle().Width(4).Height(1).Render("bb")
	joined := Row([]string{left, right})
	want := lipgloss.Width(left) + 1 + lipgloss.Width(right)
	if got := lipgloss.Width(strings.Split(joined, "\n")[0]); got != want {
		t.Errorf("guttered row is %d columns, want %d (left + 1 + right)", got, want)
	}
}
