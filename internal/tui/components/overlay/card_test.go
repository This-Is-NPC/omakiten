package overlay

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderFrame_horizontallyCentered(t *testing.T) {
	c := Card{
		Width:         22,
		Height:        4,
		Style:         StyleHidden,
		BorderVisible: false,
		Frames:        []string{"o"},
		Text:          "x",
		AutoHeight:    true,
	}
	rendered := renderFrame(c, 22)
	leading := len(rendered) - len(strings.TrimLeft(rendered, " "))
	if leading < 10 || leading > 11 {
		t.Fatalf("expected glyph centered ~10–11 cells, got %d (rendered=%q)", leading, rendered)
	}
}

func TestRenderCardSanitizesFramesAndCustomBorderAtCompactAndWideWidths(t *testing.T) {
	const hostile = "glyph \x1b[31mred\x1b]0;owned\a\x00\x9b31m\x9d0;owned\x07漢字"
	for _, width := range []int{16, 48} {
		c := Card{
			Width:         width,
			Height:        8,
			Style:         StyleCustom,
			BorderVisible: true,
			CustomBorder: lipgloss.Border{
				Top: hostile, Bottom: hostile, Left: hostile, Right: hostile,
				TopLeft: hostile, TopRight: hostile, BottomLeft: hostile, BottomRight: hostile,
			},
			Frames:     []string{hostile + "\n漢字"},
			Text:       hostile,
			TailSide:   TailBottom,
			AutoHeight: true,
		}
		rendered := RenderCard(c)
		if strings.IndexFunc(strings.ReplaceAll(rendered, "\n", ""), unicode.IsControl) >= 0 {
			t.Fatalf("width %d retained a terminal control: %q", width, rendered)
		}
		if width == 48 && !strings.Contains(rendered, "漢字") {
			t.Fatalf("width %d lost harmless Unicode: %q", width, rendered)
		}
	}
}

func TestRenderCardFallsBackWhenCustomBorderSanitizesEmpty(t *testing.T) {
	c := Card{
		Width:         24,
		Style:         StyleCustom,
		BorderVisible: true,
		CustomBorder: lipgloss.Border{
			Top: "-", Bottom: "-", Left: "|", Right: "|",
			TopLeft: "\x1b[31m\x00\x9b", TopRight: "+", BottomLeft: "+", BottomRight: "+",
		},
		Text:       "safe",
		AutoHeight: true,
	}
	if got := RenderCard(c); !strings.Contains(got, "┌") {
		t.Fatalf("sanitized-empty custom corner did not use the standard border: %q", got)
	}
}
