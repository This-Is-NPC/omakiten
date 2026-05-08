package tui

import (
	"strings"
	"testing"
)

func TestDetailGridEmptyBuilder(t *testing.T) {
	m := Model{styles: newStyles(tuiTestTheme())}
	out := m.newDetailGrid(40).Render(m.styles.border)
	if out != "" {
		t.Errorf("empty grid should render empty, got %q", out)
	}
}

func TestDetailGridRowsAndKickers(t *testing.T) {
	m := Model{styles: newStyles(tuiTestTheme())}
	out := m.newDetailGrid(40).
		Custom(m.detailKickerWithID("Comment", 7)).
		Row("Author", "agent").
		Row("When", "2026-05-06").
		Kicker("Body").
		Span("hello world").
		Render(m.styles.border)

	for _, want := range []string{
		"COMMENT · #7", // Custom kicker rendered through styles
		"// AUTHOR",    // Row label
		"agent",        // Row value
		"// WHEN",
		"2026-05-06",
		"// BODY",     // Kicker
		"hello world", // Span
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered grid missing %q:\n%s", want, out)
		}
	}
}

func TestDetailGridSpanCoversFullWidth(t *testing.T) {
	// A span row must not draw an internal vertical divider — that's the
	// whole point of the "single-cell row" branch in renderGridTable.
	m := Model{styles: newStyles(tuiTestTheme())}
	out := m.newDetailGrid(20).
		Row("A", "x").
		Span("yyy").
		Render(m.styles.border)

	// The label-row line for "A" must contain a `│` separator inside the
	// borders; the span line must NOT (only the outer borders).
	lines := strings.Split(out, "\n")
	var labelLine, spanLine string
	for _, line := range lines {
		switch {
		case strings.Contains(line, "// A"):
			labelLine = line
		case strings.Contains(line, "yyy"):
			spanLine = line
		}
	}
	if strings.Count(labelLine, "│") < 3 {
		t.Errorf("label row should have 3 vertical borders (left|sep|right), got %d in %q", strings.Count(labelLine, "│"), labelLine)
	}
	if strings.Count(spanLine, "│") != 2 {
		t.Errorf("span row should have only outer vertical borders (2), got %d in %q", strings.Count(spanLine, "│"), spanLine)
	}
}

func TestDetailGridLabelWidthConstant(t *testing.T) {
	if detailGridLabelWidth != 13 {
		t.Errorf("detailGridLabelWidth changed from 13 to %d — call sites assume 13 in their valueWidth math", detailGridLabelWidth)
	}
}
