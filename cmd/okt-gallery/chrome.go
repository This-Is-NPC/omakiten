package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
)

// The gallery paints itself with lipgloss and bubbles ONLY — never with the
// components it exhibits.
//
// Dogfooding is the obvious temptation and the wrong call here: a tool whose job
// is to reveal a broken cardlist cannot have its own index built out of cardlist,
// because the first symptom of the bug would be that the tool no longer runs. The
// chrome and the subject have to be able to fail independently, so the helpers
// this file re-implements (pad, wrap, kicker) are deliberate duplicates of
// omakiten's, not an oversight. TestTheChromeDoesNotImportWhatItInspects pins it.

// chromeStyles is the gallery's own palette, resolved from the same theme the
// components are painted with so the tool does not look foreign next to them.
type chromeStyles struct {
	Panel     lipgloss.Style
	Card      lipgloss.Style
	CardOn    lipgloss.Style
	Border    lipgloss.Style
	Accent    lipgloss.Style
	Muted     lipgloss.Style
	Text      lipgloss.Style
	Warning   lipgloss.Style
	Separator lipgloss.Style
}

func newChromeStyles(theme config.Theme) chromeStyles {
	color := func(key, fallback string) lipgloss.Color {
		if value := theme.Colors[key]; value != "" {
			return lipgloss.Color(value)
		}
		return lipgloss.Color(fallback)
	}
	border := color("border", "#494543")
	foreground := color("foreground", "#E5E2E1")
	primary := color("primary", "#39FF14")
	warning := color("warning", "#FFB347")

	box := func(tone lipgloss.Color, pad int) lipgloss.Style {
		return lipgloss.NewStyle().
			Foreground(foreground).
			Border(lipgloss.NormalBorder()).
			BorderForeground(tone).
			Padding(0, pad)
	}
	return chromeStyles{
		Panel:     box(border, 1),
		Card:      box(border, 1),
		CardOn:    box(primary, 1).Bold(true),
		Border:    lipgloss.NewStyle().Foreground(border),
		Accent:    lipgloss.NewStyle().Foreground(primary),
		Muted:     lipgloss.NewStyle().Foreground(border),
		Text:      lipgloss.NewStyle().Foreground(foreground),
		Warning:   lipgloss.NewStyle().Foreground(warning),
		Separator: lipgloss.NewStyle().Foreground(border),
	}
}

// kicker is the dev-editorial section label: `// LABEL`.
func (s chromeStyles) kicker(label string) string {
	return s.Muted.Render("// " + strings.ToUpper(label))
}

func (s chromeStyles) kickerOn(label string) string {
	return s.Accent.Render("// " + strings.ToUpper(label))
}

func (s chromeStyles) rule(width int) string {
	if width <= 0 {
		return ""
	}
	return s.Separator.Render(strings.Repeat("─", width))
}

// padLine right-pads a styled line to a visible width.
func padLine(line string, width int) string {
	if visible := lipgloss.Width(line); visible < width {
		return line + strings.Repeat(" ", width-visible)
	}
	return line
}

// fitLine forces a line to exactly `width` columns, truncating when it is over.
func fitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(line) > width {
		return ansi.Truncate(line, width, "")
	}
	return padLine(line, width)
}

// clipLine caps a line at `width` without padding it out. It is fitLine's half
// for a row that must not OVERFLOW but has no business claiming the whole width
// either — the footer, which is the last row on screen and would otherwise trail
// a band of themed background across it.
func clipLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(line) > width {
		return ansi.Truncate(line, width, "")
	}
	return line
}

// wrapAt soft-wraps at a width, ANSI-aware.
func wrapAt(block string, width int) string {
	if width <= 0 {
		return block
	}
	out := make([]string, 0, 4)
	for _, line := range strings.Split(block, "\n") {
		if lipgloss.Width(line) <= width {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(line, width, " "), "\n")...)
	}
	return strings.Join(out, "\n")
}

// frameCost is what a style spends AROUND its content, measured by rendering it
// at a known width and taking the difference.
//
// GetHorizontalFrameSize does not answer this — it sums the declared border and
// padding, while Style.Width already accounts for the padding — and the boxes
// built on the declared number overflowed the panels framing them by two columns.
func frameCost(style lipgloss.Style) int {
	const probe = 10
	return maxInt(lipgloss.Width(style.Width(probe).Render("x"))-probe, 0)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	return maxInt(lo, minInt(v, hi))
}
