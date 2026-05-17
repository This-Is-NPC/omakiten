package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestMaxLabelVisibleWidthScansAllTables guards the Auto-sizing helper
// used by renderSummaryTables: when two tables share the same panel
// the label column must grow to the widest label across both so the
// rows stay vertically aligned (otherwise each table would auto-size
// independently and rows in the second table would sit at a different
// label column width than rows in the first).
func TestMaxLabelVisibleWidthScansAllTables(t *testing.T) {
	short := []string{"// A", "v"}
	long := []string{"// CLI LANGUAGE", "en"}
	spanned := []string{"// HEADER"}

	got := maxLabelVisibleWidth([][][]string{
		{short, spanned},
		{long},
	})
	want := lipgloss.Width("// CLI LANGUAGE")
	if got != want {
		t.Fatalf("maxLabelVisibleWidth = %d, want %d (longest across both tables)", got, want)
	}
}

// TestMaxLabelVisibleWidthIgnoresSpannedRows pins that single-cell
// rows (kicker-only spans like the table header `// RUNTIME`) are not
// counted toward the label column width — they render across both
// columns and would inflate the label column needlessly.
func TestMaxLabelVisibleWidthIgnoresSpannedRows(t *testing.T) {
	rows := [][]string{
		{"// EVEN-LONGER-SPAN-HEADER"},
		{"// A", "v"},
	}
	got := maxLabelVisibleWidth([][][]string{rows})
	want := lipgloss.Width("// A")
	if got != want {
		t.Fatalf("maxLabelVisibleWidth = %d, want %d (spanned row must not bound the label column)", got, want)
	}
}
