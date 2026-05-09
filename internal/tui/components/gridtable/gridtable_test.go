package gridtable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapLines(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in    []string
		width int
		want  []string
	}{
		"short fits unchanged": {
			in:    []string{"hello"},
			width: 10,
			want:  []string{"hello"},
		},
		"long wraps at width": {
			in:    []string{"the quick brown fox jumps"},
			width: 10,
			want:  []string{"the quick", "brown fox", "jumps"},
		},
		"empty input renders one blank line": {
			in:    []string{},
			width: 10,
			want:  []string{""},
		},
		"zero width clamps to 1": {
			in:    []string{"abc"},
			width: 0,
			want:  []string{"a", "b", "c"},
		},
		"negative width clamps to 1": {
			in:    []string{"ab"},
			width: -3,
			want:  []string{"a", "b"},
		},
		"multi-line preserves boundaries": {
			in:    []string{"first", "second longer line"},
			width: 8,
			want:  []string{"first", "second", "longer", "line"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := WrapLines(tc.in, tc.width)
			if len(got) != len(tc.want) {
				t.Fatalf("WrapLines(%q, %d) returned %d lines, want %d\ngot: %q\nwant: %q", tc.in, tc.width, len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("line %d = %q, want %q\nfull got: %q", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestWrapLinesPreservesAnsiWidth covers the ANSI-aware width path: a
// styled line whose visible width is below the budget must not be
// re-wrapped because the SGR escape pushes its byte length past it.
func TestWrapLinesPreservesAnsiWidth(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("hello")
	got := WrapLines([]string{styled}, 6)
	if len(got) != 1 {
		t.Fatalf("styled short line should not wrap; got %d lines: %q", len(got), got)
	}
	if got[0] != styled {
		t.Fatalf("styled line was rewritten: got %q want %q", got[0], styled)
	}
}

func TestPadLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		line  string
		width int
		want  string
	}{
		"pads to width":    {line: "hi", width: 5, want: "hi   "},
		"already at width": {line: "hello", width: 5, want: "hello"},
		"wider than width": {line: "exceeded", width: 4, want: "exceeded"},
		"empty pads":       {line: "", width: 3, want: "   "},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := PadLine(tc.line, tc.width)
			if got != tc.want {
				t.Fatalf("PadLine(%q, %d) = %q, want %q", tc.line, tc.width, got, tc.want)
			}
		})
	}
}

// TestPadLinePreservesAnsi covers the ANSI-aware width path: a styled
// line whose escape sequences inflate its byte length must still be
// padded against its visible width, not its byte width.
func TestPadLinePreservesAnsi(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("ab")
	got := PadLine(styled, 5)
	if !strings.HasPrefix(got, styled) {
		t.Fatalf("padded output dropped the prefix styled segment: got %q", got)
	}
	if !strings.HasSuffix(got, "   ") {
		t.Fatalf("padded output should end with 3 spaces (visible width 2 -> 5): got %q", got)
	}
}

func TestRenderEmptyInputs(t *testing.T) {
	border := lipgloss.NewStyle()
	if Render(nil, []int{4}, border) != "" {
		t.Fatalf("nil rows should render empty")
	}
	if Render([][]string{{"a"}}, nil, border) != "" {
		t.Fatalf("empty widths should render empty")
	}
}

// TestRenderSingleRowHasBorders locks the smallest non-trivial layout:
// one row, two cells, two visible borders top and bottom plus dividers.
func TestRenderSingleRowHasBorders(t *testing.T) {
	border := lipgloss.NewStyle()
	out := Render([][]string{{"foo", "bar"}}, []int{4, 4}, border)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("single-row table should render 3 lines (top, content, bottom); got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "┌") || !strings.Contains(lines[0], "┬") || !strings.HasSuffix(lines[0], "┐") {
		t.Fatalf("top border missing junctions: %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "└") || !strings.Contains(lines[2], "┴") || !strings.HasSuffix(lines[2], "┘") {
		t.Fatalf("bottom border missing junctions: %q", lines[2])
	}
	if !strings.Contains(lines[1], "│") {
		t.Fatalf("content row missing column separator: %q", lines[1])
	}
	if !strings.Contains(lines[1], "foo") || !strings.Contains(lines[1], "bar") {
		t.Fatalf("content missing cell text: %q", lines[1])
	}
}

// TestRenderSpannedRowDropsInternalJunction proves the spanned-row
// special case: a single-cell row in a multi-column table covers the
// full width and the dividers above/below it omit the internal
// junction so the span reads as one contiguous block.
func TestRenderSpannedRowDropsInternalJunction(t *testing.T) {
	border := lipgloss.NewStyle()
	out := Render([][]string{
		{"a", "b"},
		{"spanned"},
		{"c", "d"},
	}, []int{4, 4}, border)
	lines := strings.Split(out, "\n")
	// Layout: top, row1, divider1 (above row=normal, below=spanned), span row,
	// divider2 (above=spanned, below=normal), row3, bottom — 7 lines total.
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines for 3-row table, got %d:\n%s", len(lines), out)
	}
	divAbove := lines[2]
	divBelow := lines[4]
	if !strings.Contains(divAbove, "┴") {
		t.Fatalf("divider above span should use ┴ (top-only junction): %q", divAbove)
	}
	if !strings.Contains(divBelow, "┬") {
		t.Fatalf("divider below span should use ┬ (bottom-only junction): %q", divBelow)
	}
	if strings.Contains(divAbove, "┼") || strings.Contains(divBelow, "┼") {
		t.Fatalf("spanned-row dividers must not carry ┼: above=%q below=%q", divAbove, divBelow)
	}
}

// TestRenderWrapsLongCellContent confirms cell content longer than its
// column width is wrapped by WrapLines and rendered as a multi-line cell.
func TestRenderWrapsLongCellContent(t *testing.T) {
	border := lipgloss.NewStyle()
	out := Render([][]string{
		{"the quick brown fox", "ok"},
	}, []int{8, 4}, border)
	lines := strings.Split(out, "\n")
	contentLines := 0
	for _, l := range lines {
		if strings.Contains(l, "│") {
			contentLines++
		}
	}
	if contentLines < 2 {
		t.Fatalf("expected wrapped cell to occupy ≥2 lines, got %d:\n%s", contentLines, out)
	}
}
