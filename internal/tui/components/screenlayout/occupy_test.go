package screenlayout

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func pipeColumn(line string) int {
	plain := ansi.Strip(line)
	at := strings.IndexRune(plain, '├')
	if at < 0 {
		return -1
	}
	return lipgloss.Width(plain[:at])
}

func TestOccupyLineTruncatesAndPads(t *testing.T) {
	if got := occupyLine("abcdef", 4, true); lipgloss.Width(got) != 4 {
		t.Fatalf("truncate: width %d, want 4 (%q)", lipgloss.Width(got), got)
	}
	if got := occupyLine("ab", 6, true); got != "ab    " {
		t.Fatalf("pad: %q, want %q", got, "ab    ")
	}
	if got := occupyLine("ab", 6, false); got != "ab" {
		t.Fatalf("no pad: %q, want %q", got, "ab")
	}
}

func TestJoinPaintedColumnsKeepsAShortHintOutOfTheNextColumn(t *testing.T) {
	// The Studio defect: "▼ 11 below" is shorter than the list, and
	// lipgloss.Width() on the whole block did not pad that row, so the
	// inspector's ├ sat on the same cells as the hint.
	painted := []paintedColumn{
		{width: 10, lines: []string{"XXXXXXXXXX", "▼ 11 below"}},
		{width: 10, lines: []string{"├────────┤", "├────────┤"}},
	}
	joined := joinPaintedColumns(painted, 2)
	if len(joined) != 2 {
		t.Fatalf("joined %d lines, want 2", len(joined))
	}
	var x int
	for i, line := range joined {
		at := pipeColumn(line)
		if at < 0 {
			t.Fatalf("line %d has no inspector pipe: %q", i, ansi.Strip(line))
		}
		if i == 0 {
			x = at
			continue
		}
		if at != x {
			t.Fatalf("inspector pipe drifted: line 0 at %d, line %d at %d\n%s", x, i, at, ansi.Strip(line))
		}
		if at < 10 {
			t.Fatalf("inspector pipe started inside the list column at %d: %q", at, ansi.Strip(line))
		}
		plain := ansi.Strip(line)
		pipe := strings.IndexRune(plain, '├')
		if strings.Contains(plain[pipe:], "▼") {
			t.Fatalf("hint bled into the inspector column: %q", plain)
		}
	}
}

func TestSideBySideOverflowsMeasuresPaintedWidthNotDeclaredMins(t *testing.T) {
	laid := []resolved{
		{measured: measured{width: 20, spec: Spec{ID: "left"}}, lines: []string{strings.Repeat("L", 40)}},
		{measured: measured{width: 20, spec: Spec{ID: "right"}}, lines: []string{strings.Repeat("R", 40)}},
	}
	columns := [][]int{{0}, {1}}
	if !sideBySideOverflows(laid, columns, 2, 50) {
		t.Fatal("40+2+40 > 50 must overflow")
	}
	if sideBySideOverflows(laid, columns, 2, 82) {
		t.Fatal("40+2+40 <= 82 must fit")
	}
}

func TestClampViewCapsWidthAndRows(t *testing.T) {
	got := clampView([]string{strings.Repeat("x", 80), "ok", "drop-me"}, 4, 2)
	if len(got) != 2 {
		t.Fatalf("clamped to %d rows, want 2", len(got))
	}
	if lipgloss.Width(got[0]) > 4 {
		t.Fatalf("line 0 is %d cells, want ≤ 4", lipgloss.Width(got[0]))
	}
}

func TestSideBySideMustRestackWhenPaintedColumnsMissTheBox(t *testing.T) {
	specs := []Spec{
		{ID: "l", MinWidth: 20, Column: true, ColumnGap: 2},
		{ID: "r", MinWidth: 20, Column: true, ColumnGap: 2},
	}
	laid := []resolved{
		{measured: measured{width: 20, spec: specs[0]}, lines: []string{strings.Repeat("L", 40)}},
		{measured: measured{width: 20, spec: specs[1]}, lines: []string{strings.Repeat("R", 40)}},
	}
	columns := [][]int{{0}, {1}}
	if !sideBySideMustRestack(laid, specs, columns, Box{Width: 50, Rows: 10}) {
		t.Fatal("unfitted 40+2+40 into 50 must restack")
	}
	if sideBySideMustRestack(laid, specs, columns, Box{Width: 82, Rows: 10}) {
		t.Fatal("unfitted 40+2+40 into 82 must stay side by side")
	}
}
