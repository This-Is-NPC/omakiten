package selectlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenkit"
)

func plainKit() screenkit.Kit {
	return screenkit.Kit{Styles: screenkit.Styles{
		Border:     lipgloss.NewStyle(),
		Separator:  lipgloss.NewStyle(),
		Hint:       lipgloss.NewStyle(),
		HintAccent: lipgloss.NewStyle(),
		Cursor:     lipgloss.NewStyle(),
	}}
}

func sampleRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		if i == 0 {
			rows[i] = Row{Left: "01 // BACKLOG · 19", Right: "-> dev"}
			continue
		}
		rows[i] = Row{Left: "  #self-branch", Right: "-> dev"}
	}
	return rows
}

func TestRenderHeightIsContentPlusBorders(t *testing.T) {
	kit := plainKit()
	spec := Spec{Kicker: "// WORKFLOW", Rows: sampleRows(3), Width: 40, Cursor: 0}
	got := strings.Count(Render(kit, spec), "\n") + 1
	// kicker + rule + 3 rows + 2 borders
	if want := panel.FixedBoxHeight(1 + 1 + 3); got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
}

func TestHeightHoldsTheBox(t *testing.T) {
	kit := plainKit()
	spec := Spec{Kicker: "// WORKFLOW", Rows: sampleRows(20), Width: 40, Height: 10, Cursor: 0}
	got := strings.Count(Render(kit, spec), "\n") + 1
	if got != 10 {
		t.Fatalf("Height=10 painted %d rows", got)
	}
}

func TestChevronOnlyOnCursor(t *testing.T) {
	kit := plainKit()
	out := Render(kit, Spec{
		Kicker: "// COMMANDS",
		Rows:   []Row{{Left: "01 // a"}, {Left: "02 // b"}, {Left: "03 // c"}},
		Width:  32,
		Cursor: 1,
	})
	var chevrons int
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "›") {
			chevrons++
			if !strings.Contains(line, "02 // b") {
				t.Fatalf("chevron landed on the wrong row: %q", line)
			}
		}
	}
	if chevrons != 1 {
		t.Fatalf("chevron count = %d, want 1\n%s", chevrons, out)
	}
}

func TestNoOverdraw(t *testing.T) {
	kit := plainKit()
	const width = 40
	out := Render(kit, Spec{
		Kicker: "// WORKFLOW · omakase · clean",
		Rows:   []Row{{Left: "01 // BACKLOG · 19", Right: "-> review"}, {Left: "  #self-branch", Right: "-> review"}},
		Width:  width,
		Cursor: 0,
	})
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got != width {
			t.Fatalf("row %d width = %d, want %d: %q", i, got, width, line)
		}
	}
}

func TestEmptyPaintsPlaceholderInsideTheBox(t *testing.T) {
	kit := plainKit()
	out := Render(kit, Spec{Kicker: "// HOOKS", Empty: "(empty)", Width: 28, Cursor: -1})
	if !strings.Contains(out, "(empty)") {
		t.Fatalf("missing empty copy: %q", out)
	}
	if !strings.Contains(out, "┌") || !strings.Contains(out, "└") {
		t.Fatalf("empty list dropped the box: %q", out)
	}
	if strings.Contains(out, "›") {
		t.Fatalf("empty list painted a chevron: %q", out)
	}
}

func TestPaintFrameJoinsToRender(t *testing.T) {
	kit := plainKit()
	spec := Spec{Kicker: "// PERSONAS", Subtitle: "// PERSONAS · 2", Rows: sampleRows(2), Width: 36, Cursor: 0}
	if got, want := blockView(Paint(kit, spec)), Render(kit, spec); got != want {
		t.Fatalf("Paint.View drifted from Render\nPaint:\n%s\nRender:\n%s", got, want)
	}
}

func TestPaintSplitsChromeFromItems(t *testing.T) {
	kit := plainKit()
	frame := Paint(kit, Spec{
		Kicker:   "// WORKFLOW",
		Subtitle: "4 buckets · 7 guards · clean",
		Rows:     sampleRows(3),
		Width:    40,
		Cursor:   0,
	})
	if len(frame.Header) != 4 { // top, kicker, subtitle, rule
		t.Fatalf("Header rows = %d, want 4: %#v", len(frame.Header), frame.Header)
	}
	if len(frame.Items) != 3 {
		t.Fatalf("Items = %d, want 3", len(frame.Items))
	}
	if len(frame.Footer) != 1 {
		t.Fatalf("Footer rows = %d, want 1", len(frame.Footer))
	}
	if !strings.Contains(frame.Header[0], "┌") {
		t.Fatalf("Header[0] is not the top border: %q", frame.Header[0])
	}
	if !strings.Contains(frame.Footer[0], "└") {
		t.Fatalf("Footer is not the bottom border: %q", frame.Footer[0])
	}
}

func TestInnerWidthChargesTheBorder(t *testing.T) {
	if got := InnerWidth(40); got != 38 {
		t.Fatalf("InnerWidth(40) = %d, want 38", got)
	}
	if got := InnerWidth(2); got != 1 {
		t.Fatalf("InnerWidth(2) = %d, want 1", got)
	}
}

func TestNarrowWidthDoesNotPanic(t *testing.T) {
	kit := plainKit()
	out := Render(kit, Spec{
		Kicker: "K",
		Rows:   []Row{{Left: "left that is far too long for the box", Right: "-> dest"}},
		Width:  8,
		Cursor: 0,
	})
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > 8 {
			t.Fatalf("row %d is %d cells past 8: %q", i, got, line)
		}
	}
}

func TestPaintPreservesThemedRowText(t *testing.T) {
	kit := plainKit()
	kit.Styles.Warning = lipgloss.NewStyle().Bold(true)
	tagged := kit.Styles.Warning.Render("  #resume")
	arrow := kit.Styles.Warning.Render("-> review")
	out := Render(kit, Spec{
		Kicker: "// WORKFLOW",
		Rows:   []Row{{Left: "01 // BACKLOG · 10"}, {Left: tagged, Right: arrow}},
		Width:  40,
		Cursor: 0,
	})
	if !strings.Contains(out, tagged) {
		t.Fatalf("Warning-styled tag was not preserved:\n%s", out)
	}
	if !strings.Contains(out, arrow) {
		t.Fatalf("Warning-styled arrow was not preserved:\n%s", out)
	}
}

func TestKickerRuleJoinsTheSides(t *testing.T) {
	kit := plainKit()
	out := Render(kit, Spec{
		Kicker: "// COMMANDS",
		Rows:   []Row{{Left: "01 // okt"}},
		Width:  32,
		Cursor: 0,
	})
	var join string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "├") {
			join = line
			break
		}
	}
	if join == "" {
		t.Fatalf("kicker rule did not join the sides:\n%s", out)
	}
	if !strings.HasPrefix(join, "├") || !strings.HasSuffix(join, "┤") {
		t.Fatalf("join was not a full-width ├─┤ row: %q\n%s", join, out)
	}
	if strings.Contains(join, "│") {
		t.Fatalf("join was wrapped as │────│ instead of ├─┤: %q", join)
	}
}

func TestFocusedKickerKeepsSGR(t *testing.T) {
	kit := plainKit()
	kicker := "\x1b[38;2;57;255;20m▸ COMMANDS\x1b[0m"
	out := Render(kit, Spec{
		Kicker: kicker,
		Rows:   []Row{{Left: "01 // okt"}},
		Width:  32,
		Cursor: 0,
	})
	if !strings.Contains(out, "\x1b[38;2;57;255;20m") {
		t.Fatalf("focused kicker lost theme primary SGR (PadLine sanitized it):\n%q", out)
	}
}
