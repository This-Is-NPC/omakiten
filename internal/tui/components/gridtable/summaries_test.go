package gridtable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRowsBuildsKickerHeader(t *testing.T) {
	t.Parallel()
	kicker := func(s string) string { return "// " + strings.ToUpper(s) }
	rows := Rows(kicker, "totals", [2]string{"tasks", "12"}, [2]string{"tags", "3"})
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want header + 2 fields", len(rows))
	}
	if len(rows[0]) != 1 || renderedText(rows[0][0]) != "// TOTALS" {
		t.Fatalf("header row = %v, want a single spanned cell", rows[0])
	}
	if renderedText(rows[1][0]) != "// TASKS" || renderedText(rows[1][1]) != "12" {
		t.Fatalf("field row = %v", rows[1])
	}
}

func TestRowsHeaderSpansTheFullGrid(t *testing.T) {
	t.Parallel()
	kicker := func(s string) string { return "// " + strings.ToUpper(s) }
	out := Summaries(40, lipgloss.NewStyle(), Options{LabelWidth: 12, ValueWidth: 20},
		Rows(kicker, "resume", [2]string{"type", "comments_tagged"}))
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Fatalf("table too short:\n%s", out)
	}
	header := lines[1]
	if strings.Count(header, "│") != 2 {
		t.Fatalf("header should be one continuous cell, got %d verticals in %q", strings.Count(header, "│"), header)
	}
	if !strings.Contains(header, "// RESUME") {
		t.Fatalf("header missing title:\n%s", out)
	}
	if strings.Contains(header, "  //") || strings.Contains(header, "//      ") {
		t.Fatalf("header padded the title into the label column:\n%s", header)
	}
	div := lines[2]
	if !strings.Contains(div, "┬") {
		t.Fatalf("divider under spanned header should use ┬, got %q", div)
	}
	if strings.Contains(div, "┼") {
		t.Fatalf("divider under spanned header still splits two columns: %q", div)
	}
}

func TestSummariesResponsivePolicy(t *testing.T) {
	t.Parallel()
	left := RawRows([][]string{{"// A", ""}, {"// ONE", "1"}})
	right := RawRows([][]string{{"// B", ""}, {"// TWO", "2"}})
	opts := Options{LabelWidth: 13, ValueWidth: 27, SideBySide: true, MergeNarrow: true}
	border := lipgloss.NewStyle()

	wide := Summaries(196, border, opts, left, right)
	if lines := strings.Split(wide, "\n"); len(lines) > 6 {
		t.Fatalf("wide layout should sit tables side by side, got %d rows", len(lines))
	}

	stacked := Summaries(56, border, opts, left, right)
	if !strings.Contains(stacked, "\n\n") {
		t.Fatal("stacked layout should separate tables with a blank row")
	}

	merged := Summaries(26, border, opts, left, right)
	if strings.Contains(merged, "\n\n") {
		t.Fatal("narrow layout with MergeNarrow should emit a single table")
	}
	if w := Width(26, opts, left, right); w > 26 {
		t.Fatalf("Width(%d) = %d, want ≤ available so the table knows how to shrink", 26, w)
	}

	opts.MergeNarrow = false
	unmerged := Summaries(26, border, opts, left, right)
	if !strings.Contains(unmerged, "\n\n") {
		t.Fatal("narrow layout without MergeNarrow should keep tables separate")
	}
}

func TestSummariesFitsBelowNaturalWidth(t *testing.T) {
	t.Parallel()
	// The defect #2442's sibling: a fixed label floor left the table wider
	// than the terminal. Summaries must shrink so every painted line fits.
	table := RawRows([][]string{{"// TOTALS", ""}, {"// TASKS", "12"}, {"// COMMENTS", "3"}})
	opts := Options{LabelWidth: 13, ValueWidth: 27, MergeNarrow: true}
	available := 18
	out := Summaries(available, lipgloss.NewStyle(), opts, table)
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > available {
			t.Fatalf("line %d width %d > available %d:\n%q", i, got, available, line)
		}
	}
	if got := Width(available, opts, table); got > available {
		t.Fatalf("Width = %d > available %d", got, available)
	}
}

func TestWidthMatchesSummaries(t *testing.T) {
	t.Parallel()
	table := RawRows([][]string{{"// A", ""}, {"// ONE", "value"}})
	opts := Options{LabelWidth: 13, ValueWidth: 27, MergeNarrow: true}
	border := lipgloss.NewStyle()
	for _, available := range []int{200, 60, 30, 18, 12} {
		out := Summaries(available, border, opts, table)
		widest := 0
		for _, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > widest {
				widest = w
			}
		}
		if got := Width(available, opts, table); got != widest {
			t.Fatalf("available %d: Width = %d, Summaries widest = %d", available, got, widest)
		}
	}
}

func TestMaxLabelWidthIgnoresSpannedRows(t *testing.T) {
	t.Parallel()
	tables := [][][]Cell{
		RawRows([][]string{{"spanned"}, {"// SHORT", "1"}}),
		RawRows([][]string{{"// A LONGER LABEL", "2"}}),
	}
	if got, want := MaxLabelWidth(tables), lipgloss.Width("// A LONGER LABEL"); got != want {
		t.Fatalf("MaxLabelWidth = %d, want %d", got, want)
	}
	if got := MaxLabelWidth(nil); got != 0 {
		t.Fatalf("MaxLabelWidth(nil) = %d, want 0", got)
	}
}

func TestSummariesAutoExpandsColumns(t *testing.T) {
	t.Parallel()
	wide := RawRows([][]string{{"// A VERY LONG KICKER LABEL", ""}, {"// PATH", "/a/very/long/configuration/path.yaml"}})
	opts := Options{LabelWidth: 13, ValueWidth: 20, Auto: true}
	out := Summaries(116, lipgloss.NewStyle(), opts, wide)
	if !strings.Contains(out, "/a/very/long/configuration/path.yaml") {
		t.Fatalf("auto sizing must fit the value on one row:\n%s", out)
	}
	if !strings.Contains(out, "// A VERY LONG KICKER LABEL") {
		t.Fatalf("auto sizing must fit the widest kicker:\n%s", out)
	}
}

// Width and Summaries share columnSizes so a budget asked before a paint cannot
// disagree with the paint. That guarantee has to survive a degenerate budget
// too, which is where the two used to take different branches.
func TestADegenerateBudgetKeepsTheMeasurementAndThePaintAgreeing(t *testing.T) {
	tables := [][][]Cell{RawRows([][]string{{"kicker"}, {"label", "value"}})}
	for _, available := range []int{0, -1, -80} {
		widths := ColumnWidths(available, Options{LabelWidth: 13, ValueWidth: 27}, tables...)
		if len(widths) != 2 || widths[0] < 1 || widths[1] < 1 {
			t.Errorf("available %d produced widths %v", available, widths)
		}
		if got := Width(available, Options{LabelWidth: 13, ValueWidth: 27}, tables...); got < 1 {
			t.Errorf("available %d measured %d columns", available, got)
		}
	}
}
