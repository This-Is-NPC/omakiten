package gridtable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestEmptyRendersEmpty(t *testing.T) {
	m := NewDetail(40, lipgloss.NewStyle())
	out := m.View(lipgloss.NewStyle())
	if out != "" {
		t.Errorf("empty detail screen should render empty, got %q", out)
	}
}

func TestBuilderProducesAllFragments(t *testing.T) {
	m := NewDetail(40, lipgloss.NewStyle()).
		Custom(Raw("// COMMENT · #7")).
		Row("Author", "agent").
		Row("When", "2026-05-06").
		Kicker("Body").
		Span(Raw("hello world"))

	out := m.View(lipgloss.NewStyle())
	for _, want := range []string{"COMMENT · #7", "// AUTHOR", "agent", "// WHEN", "2026-05-06", "// BODY", "hello world"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q:\n%s", want, out)
		}
	}
}

func TestSpanRowSkipsInternalDivider(t *testing.T) {
	m := NewDetail(20, lipgloss.NewStyle()).Row("A", "x").Span(Raw("yyy"))
	out := m.View(lipgloss.NewStyle())
	var labelLine, spanLine string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "// A"):
			labelLine = line
		case strings.Contains(line, "yyy"):
			spanLine = line
		}
	}
	if strings.Count(labelLine, "│") < 3 {
		t.Errorf("label row should have left/sep/right vertical borders, got %d in %q", strings.Count(labelLine, "│"), labelLine)
	}
	if strings.Count(spanLine, "│") != 2 {
		t.Errorf("span row should have only outer vertical borders (2), got %d in %q", strings.Count(spanLine, "│"), spanLine)
	}
}

func TestLabelWidthExportedConstant(t *testing.T) {
	if LabelWidth != 13 {
		t.Errorf("LabelWidth = %d, want 13 — call sites compute valueWidth from this constant", LabelWidth)
	}
}

// TestLongLabelDoesNotWrapAndShareTotalWidth pins the auto-size fix:
// when a translated label like `// COMENTÁRIOS` (14 visible chars)
// would exceed the default 13-char label column it must expand the
// label cell rather than wrap (the wrapped continuation line dropped
// its ANSI styling and rendered the second word in default colour).
// The value column absorbs the delta so the table's outer width still
// equals (defaultLabelW + defaultValueW + borders).
func TestLongLabelDoesNotWrapAndShareTotalWidth(t *testing.T) {
	valueW := 40
	short := NewDetail(valueW, lipgloss.NewStyle()).Row("a", "v").View(lipgloss.NewStyle())
	long := NewDetail(valueW, lipgloss.NewStyle()).Row("comentários", "v").View(lipgloss.NewStyle())

	shortWidth := tableVisibleWidth(short)
	longWidth := tableVisibleWidth(long)
	if shortWidth != longWidth {
		t.Fatalf("table width drifted: short=%d long=%d (long label must not change outer footprint)", shortWidth, longWidth)
	}

	// Long-label table must render the kicker on a single line — no
	// wrapped continuation row carrying just "RIOS" / "TÁRIOS".
	for _, line := range strings.Split(long, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "│├┤┌┐└┘─┬┴┼"))
		if trimmed == "RIOS" || trimmed == "TÁRIOS" || trimmed == "TÁRIOS  v" {
			t.Fatalf("long label wrapped to a continuation line:\n%s", long)
		}
	}
}

// tableVisibleWidth returns the visible width of the widest line in a
// gridtable-rendered string. Strips ANSI via lipgloss.Width.
func tableVisibleWidth(rendered string) int {
	max := 0
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
		}
	}
	return max
}
