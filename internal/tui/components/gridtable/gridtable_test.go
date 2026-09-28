package gridtable

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FitWidths shrinks natural column widths so their sum fits available.
func TestFitWidths(t *testing.T) {
	t.Parallel()
	natural := []int{26, 8, 8, 9, 8, 7}
	fitted := FitWidths(natural, 40, 3)
	sum := 0
	for i, w := range fitted {
		sum += w
		if w < 3 {
			t.Fatalf("column %d width %d below min", i, w)
		}
		if w > natural[i] {
			t.Fatalf("column %d grew from %d to %d", i, natural[i], w)
		}
	}
	if sum > 40 {
		t.Fatalf("sum %d > available 40: %v", sum, fitted)
	}
	// Room enough: unchanged.
	wide := FitWidths(natural, 200, 3)
	for i := range natural {
		if wide[i] != natural[i] {
			t.Fatalf("wide FitWidths changed column %d: %v", i, wide)
		}
	}
}

func TestFormatRowAlignsAndTruncates(t *testing.T) {
	t.Parallel()
	row := FormatRow([]string{"abcdefghij", "1", "ok"}, []int{6, 3, 4})
	if lipgloss.Width(row) != 6+1+3+1+4 {
		t.Fatalf("row width %d, want 15: %q", lipgloss.Width(row), row)
	}
	if !strings.Contains(row, "…") {
		t.Fatalf("long first cell should truncate with ellipsis: %q", row)
	}
}

// TestWrapLines covers the plain wrapping path used by bordered cells.
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

func TestRenderRawCellsStripSGRWhileTrustedCellsPreserveStyle(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("trusted")
	raw := Render([][]string{{styled, "hostile\x1b]0;owned\a"}}, []int{10, 10}, lipgloss.NewStyle())
	if strings.Contains(raw, "\x1b[") {
		t.Fatalf("raw render retained caller SGR: %q", raw)
	}
	trusted := RenderCells([][]Cell{{Styled(styled), Raw("hostile\x1b]0;owned\a")}}, []int{10, 10}, lipgloss.NewStyle())
	if !strings.Contains(trusted, styled) {
		t.Fatalf("trusted render dropped framework SGR: got %q, want styled cell %q", trusted, styled)
	}
	if strings.Contains(trusted, "owned") {
		t.Fatalf("render retained OSC payload: %q", trusted)
	}
}

func TestRawBoundaryHandlesAdversarialControlsAndWidths(t *testing.T) {
	t.Parallel()
	const hostile = "A\x1b[8mhidden\x1b[31mred\x1b[0mB\x1b]0;owned\aC\x1bPprivate\x1b\\D\x90dcs\x9cE\x00\x7f\u009b8mF\u009dtitle\aG漢字"
	for _, width := range []int{1, 8, 24, 80} {
		out := Render([][]string{{hostile}}, []int{width}, lipgloss.NewStyle())
		if strings.ContainsAny(out, "\x1b\x00\x7f\u0090\u009b\u009d") {
			t.Fatalf("width %d retained terminal control: %q", width, out)
		}
		if strings.Contains(out, "owned") || strings.Contains(out, "private") || strings.Contains(out, "dcs") {
			t.Fatalf("width %d retained control payload: %q", width, out)
		}
		if !strings.Contains(out, "漢") {
			t.Fatalf("width %d lost harmless Unicode: %q", width, out)
		}
	}
}

func TestRowsSanitizeBeforeTrustedStyling(t *testing.T) {
	t.Parallel()
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	rows := Rows(func(s string) string { return style.Render(s) }, "title\x1b[8mowned\x1b[0m", [2]string{"label\u009b31m", "value"})
	out := RenderCells(rows, []int{12, 12}, lipgloss.NewStyle())
	if strings.Contains(out, "\x1b[8m") || strings.Contains(out, "\x1b]") || strings.Contains(out, "\u009b") || strings.Contains(out, "\u009d") {
		t.Fatalf("Rows trusted styling retained hostile terminal data: %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "titleowned") || !strings.Contains(ansi.Strip(out), "label") {
		t.Fatalf("Rows lost printable data while sanitizing: %q", ansi.Strip(out))
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

// separatorGlyphs are the box-drawing runes that only ever appear on a
// horizontal rule — the top border, the bottom border and the dividers
// between rows. A reported row offset that lands on a line carrying any
// of them is pointing at chrome instead of content.
const separatorGlyphs = "┌┐└┘├┤┬┴┼─"

// TestRenderWithLayoutOffsetsLandOnContentRows is the regression test for the
// Studio Flow cursor defect: callers used to map an input row to a rendered
// line with a constant formula (`2 + row`, or the `5 + 2r` that replaces it),
// which is only ever right when every cell happens to be one line tall.
//
// The table below is deliberately built at a width where two of its cells wrap
// to 2+ lines, so every constant formula is wrong for at least one row. The
// assertion is the one that survives wrapping: the offset the renderer itself
// reports must land on a CONTENT line holding that row's own text, never on a
// separator.
func TestRenderWithLayoutOffsetsLandOnContentRows(t *testing.T) {
	t.Parallel()

	border := lipgloss.NewStyle()
	rows := [][]string{
		{"SPANNED KICKER"},
		{"from \\ to", "backlog-intake-triage"},
		{"alpha", "open"},
		{"beta", "guarded:2"},
	}
	widths := []int{9, 12}

	out, layout := RenderWithLayout(rows, widths, border)
	lines := strings.Split(out, "\n")

	assertLayoutShape(t, rows, widths, border, out, lines, layout)

	// Anti-vacuity: the point of the test is the wrapped case, so prove at
	// least one row really did grow past a single line before trusting the
	// offsets it reports.
	assertWrapped(t, layout, out)

	// The constant formula the defect used: had it still been right here, the
	// test would not distinguish a reported offset from a guessed one.
	for row := range rows {
		if layout.RowOffsets[row] == 1+2*row {
			continue
		}
		t.Logf("row %d offset %d diverges from the constant 1+2r formula (%d) — as it must once a row wraps", row, layout.RowOffsets[row], 1+2*row)
	}

	assertReportedRows(t, layout, lines, out)
}

func assertLayoutShape(t *testing.T, rows [][]string, widths []int, border lipgloss.Style, out string, lines []string, layout Layout) {
	if got := Render(rows, widths, border); got != out {
		t.Fatalf("Render and RenderWithLayout disagree; the additive API changed the rendered bytes\nRender:\n%s\nRenderWithLayout:\n%s", got, out)
	}
	if layout.Lines != len(lines) {
		t.Fatalf("layout.Lines = %d, want %d", layout.Lines, len(lines))
	}
	if len(layout.RowOffsets) != len(rows) || len(layout.RowHeights) != len(rows) {
		t.Fatalf("layout reports %d offsets / %d heights for %d rows", len(layout.RowOffsets), len(layout.RowHeights), len(rows))
	}
}

func assertWrapped(t *testing.T, layout Layout, out string) {
	for _, height := range layout.RowHeights {
		if height >= 2 {
			return
		}
	}
	t.Fatalf("no row wrapped to 2+ lines; the fixture no longer exercises the defect\n%s", out)
}

func assertReportedRows(t *testing.T, layout Layout, lines []string, out string) {
	wants := []string{"SPANNED KICKER", "from", "alpha", "beta"}
	for row := range layout.RowOffsets {
		assertReportedRow(t, row, wants[row], layout.RowOffsets[row], layout.RowHeights[row], lines, out)
	}
}

func assertReportedRow(t *testing.T, row int, want string, offset, height int, lines []string, out string) {
	if offset < 0 || offset >= len(lines) {
		t.Fatalf("row %d offset %d is outside the %d rendered lines", row, offset, len(lines))
	}
	line := lines[offset]
	if strings.ContainsAny(line, separatorGlyphs) {
		t.Errorf("row %d offset %d lands on a separator, not a content row: %q\n%s", row, offset, line, out)
	}
	if !strings.Contains(line, want) {
		t.Errorf("row %d offset %d = %q, want a line containing %q\n%s", row, offset, line, want, out)
	}
	for extra := 1; extra < height; extra++ {
		continuation := offset + extra
		if continuation >= len(lines) {
			t.Fatalf("row %d height %d overruns the %d rendered lines", row, height, len(lines))
		}
		if strings.ContainsAny(lines[continuation], separatorGlyphs) {
			t.Errorf("row %d continuation line %d lands on a separator: %q\n%s", row, continuation, lines[continuation], out)
		}
	}
}

// TestRenderWithLayoutEmptyInputs pins the degenerate contract: the additive
// entry point returns the same empty string Render does, with a zero layout no
// caller can misread as a real offset.
func TestRenderWithLayoutEmptyInputs(t *testing.T) {
	t.Parallel()

	border := lipgloss.NewStyle()
	for name, tc := range map[string]struct {
		rows   [][]string
		widths []int
	}{
		"nil rows":      {rows: nil, widths: []int{4}},
		"empty widths":  {rows: [][]string{{"a"}}, widths: nil},
		"both are zero": {rows: nil, widths: nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, layout := RenderWithLayout(tc.rows, tc.widths, border)
			if out != "" {
				t.Fatalf("expected empty render, got %q", out)
			}
			if layout.Lines != 0 || len(layout.RowOffsets) != 0 || len(layout.RowHeights) != 0 {
				t.Fatalf("expected a zero layout, got %+v", layout)
			}
			if _, ok := layout.RowLine(0); ok {
				t.Fatal("RowLine reported ok on a zero layout")
			}
		})
	}
}

// TestLayoutRowLineRejectsOutOfRange proves the accessor is the bounds check
// callers would otherwise write themselves — the mapping arithmetic this whole
// change exists to delete.
func TestLayoutRowLineRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	_, layout := RenderWithLayout([][]string{{"a", "b"}, {"c", "d"}}, []int{4, 4}, lipgloss.NewStyle())
	for _, row := range []int{-1, 2, 99} {
		if line, ok := layout.RowLine(row); ok {
			t.Fatalf("RowLine(%d) = (%d, true), want ok=false", row, line)
		}
	}
	for row := 0; row < 2; row++ {
		line, ok := layout.RowLine(row)
		if !ok {
			t.Fatalf("RowLine(%d) reported ok=false for an in-range row", row)
		}
		if line != layout.RowOffsets[row] {
			t.Fatalf("RowLine(%d) = %d, want %d", row, line, layout.RowOffsets[row])
		}
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

func TestRenderAndDetailStripTerminalControlsBeforeSizing(t *testing.T) {
	t.Parallel()
	const hostile = "name \x1b[31mred\x1b]0;owned\a\x00\x9b31m\x9d0;owned\a\u0085漢字"
	for name, got := range map[string]string{
		"compact table": Render([][]string{{hostile, "value"}}, []int{8, 8}, lipgloss.NewStyle()),
		"wide table":    Render([][]string{{hostile, hostile}}, []int{32, 32}, lipgloss.NewStyle()),
		"detail row":    NewDetail(24, lipgloss.NewStyle()).Row(hostile, hostile).View(lipgloss.NewStyle()),
	} {
		t.Run(name, func(t *testing.T) {
			plain := ansi.Strip(got)
			for _, r := range plain {
				if r != '\n' && unicode.IsControl(r) {
					t.Fatalf("render retained control U+%04X: %q", r, got)
				}
			}
			if !strings.Contains(plain, "漢字") || strings.Contains(plain, "owned") {
				t.Fatalf("render lost harmless Unicode or retained OSC payload: %q", plain)
			}
		})
	}

	multiline := NewDetail(24, lipgloss.NewStyle()).Span(Raw("first\nsecond")).View(lipgloss.NewStyle())
	if !strings.Contains(multiline, "first") || !strings.Contains(multiline, "second") {
		t.Fatalf("detail span did not preserve multiline semantics: %q", multiline)
	}
}
