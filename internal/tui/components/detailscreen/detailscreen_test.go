package detailscreen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestEmptyRendersEmpty(t *testing.T) {
	m := New(40)
	out := m.View(0, lipgloss.NewStyle(), lipgloss.NewStyle())
	if out != "" {
		t.Errorf("empty detail screen should render empty, got %q", out)
	}
}

func TestBuilderProducesAllFragments(t *testing.T) {
	m := New(40).
		Custom("// COMMENT · #7").
		Row("Author", "agent").
		Row("When", "2026-05-06").
		Kicker("Body").
		Span("hello world")

	out := m.View(0, lipgloss.NewStyle(), lipgloss.NewStyle())
	for _, want := range []string{"COMMENT · #7", "// AUTHOR", "agent", "// WHEN", "2026-05-06", "// BODY", "hello world"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q:\n%s", want, out)
		}
	}
}

func TestSpanRowSkipsInternalDivider(t *testing.T) {
	m := New(20).Row("A", "x").Span("yyy")
	out := m.View(0, lipgloss.NewStyle(), lipgloss.NewStyle())
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

func TestUpdateScrollsViewport(t *testing.T) {
	// Build a tall body so viewport overflow kicks in.
	m := New(40).Custom("// HEADER")
	for i := 0; i < 50; i++ {
		m = m.Span("line")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}, 5)
	if m.Viewport.Scroll != 1 {
		t.Errorf("Viewport.Scroll = %d after one j, want 1", m.Viewport.Scroll)
	}
}

func TestUpdateEscFiresCancel(t *testing.T) {
	m := New(40).Custom("// X")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc}, 5)
	if m.LastEvent() == 0 {
		t.Error("esc should propagate to viewport.EventCancel")
	}
}

func TestViewWithSmallViewportRendersFooter(t *testing.T) {
	// Build enough rows that the rendered grid exceeds 5 lines.
	m := New(20)
	for i := 0; i < 10; i++ {
		m = m.Row("Field", "value")
	}
	out := m.View(5, lipgloss.NewStyle(), lipgloss.NewStyle())
	if !strings.Contains(out, "above") && !strings.Contains(out, "below") {
		t.Errorf("overflow View should render scroll footer, got:\n%s", out)
	}
}

func TestLabelWidthExportedConstant(t *testing.T) {
	if LabelWidth != 13 {
		t.Errorf("LabelWidth = %d, want 13 — call sites compute valueWidth from this constant", LabelWidth)
	}
}
