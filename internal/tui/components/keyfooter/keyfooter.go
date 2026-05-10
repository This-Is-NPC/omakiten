// Package keyfooter renders the compact keybinding footer shared by the
// main TUI chrome and overlay components.
package keyfooter

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/config"
)

// Token is one keybinding entry in a footer row.
type Token struct {
	Key     string
	Label   string
	Primary bool
}

// Styles carries the two text treatments used by the footer. Primary is
// reserved for the highest-priority keys; Secondary renders labels,
// separators, and non-primary keys.
type Styles struct {
	Primary      lipgloss.Style
	Secondary    lipgloss.Style
	Separator    string
	MaxPrimaries int
	Align        string
}

const (
	AlignLeft   = "left"
	AlignCenter = "center"
	AlignRight  = "right"
)

// ThemeStyles returns the canonical Omakiten footer treatment for packages
// that do not have access to the root tui.styles type.
func ThemeStyles(theme config.Theme) Styles {
	color := func(key, fallback string) lipgloss.Color {
		if value := theme.Colors[key]; value != "" {
			return lipgloss.Color(value)
		}
		return lipgloss.Color(fallback)
	}
	return Styles{
		Primary:   lipgloss.NewStyle().Foreground(color("primary", "#39FF14")).Bold(true),
		Secondary: lipgloss.NewStyle().Foreground(color("border", "#494543")),
	}
}

// Render formats tokens as a single footer row.
func Render(tokens []Token, styles Styles) string {
	return render(tokens, styles, 0)
}

// RenderWrapped formats tokens and wraps them across rows when width is
// positive. It never truncates a single token; a token wider than width is
// emitted on its own row.
func RenderWrapped(tokens []Token, styles Styles, width int) string {
	return render(tokens, styles, width)
}

func render(tokens []Token, styles Styles, width int) string {
	styles = normalizeStyles(styles)
	pieces, sep := renderPieces(tokens, styles)
	if width <= 0 {
		return strings.Join(pieces, sep)
	}

	var lines []string
	var line []string
	lineWidth := 0
	sepWidth := ansi.StringWidth(sep)
	for _, piece := range pieces {
		pieceWidth := ansi.StringWidth(piece)
		addWidth := pieceWidth
		if len(line) > 0 {
			addWidth += sepWidth
		}
		if len(line) > 0 && lineWidth+addWidth > width {
			lines = append(lines, strings.Join(line, sep))
			line = nil
			lineWidth = 0
		}
		if len(line) > 0 {
			lineWidth += sepWidth
		}
		line = append(line, piece)
		lineWidth += pieceWidth
	}
	if len(line) > 0 {
		lines = append(lines, strings.Join(line, sep))
	}
	for i, line := range lines {
		lines[i] = alignLine(line, width, styles.Align)
	}
	return strings.Join(lines, "\n")
}

func normalizeStyles(styles Styles) Styles {
	if styles.Separator == "" {
		styles.Separator = "  "
	}
	if styles.MaxPrimaries <= 0 {
		styles.MaxPrimaries = 3
	}
	if styles.Align == "" {
		styles.Align = AlignLeft
	}
	return styles
}

func alignLine(line string, width int, align string) string {
	if width <= 0 {
		return line
	}
	current := ansi.StringWidth(line)
	if current >= width {
		return line
	}
	pad := width - current
	switch align {
	case AlignCenter:
		left := pad / 2
		return strings.Repeat(" ", left) + line + strings.Repeat(" ", pad-left)
	case AlignRight:
		return strings.Repeat(" ", pad) + line
	default:
		return line
	}
}

func renderPieces(tokens []Token, styles Styles) ([]string, string) {
	primaryBudget := styles.MaxPrimaries
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if strings.TrimSpace(t.Key) == "" {
			continue
		}
		keyStyle := styles.Secondary.Render(t.Key)
		if t.Primary && primaryBudget > 0 {
			keyStyle = styles.Primary.Render(t.Key)
			primaryBudget--
		}
		piece := keyStyle
		if t.Label != "" {
			piece += styles.Secondary.Render(" " + t.Label)
		}
		parts = append(parts, piece)
	}
	return parts, styles.Secondary.Render(styles.Separator)
}
