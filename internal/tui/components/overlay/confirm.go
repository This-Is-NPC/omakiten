package overlay

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// Confirm is the presentation spec for a confirm/apply card. The host owns
// lifecycle and copy; this is paint only — kicker, hint, labelled candidate
// fields, a list, message, and confirm/cancel footer.
type Confirm struct {
	Width int

	BorderForeground lipgloss.Color

	Styles Styles

	// Kicker is already painted (host uses screenkit.Kicker / KickerCount).
	Kicker string
	Hint   string

	Fields []ConfirmField

	ListTitle string
	ListItems []string
	ListEmpty string

	Message string
	Footer  string
}

// ConfirmField is one labelled candidate row inside Confirm.
type ConfirmField struct {
	Label, Value string
}

const confirmLabelColumn = 18

// RenderConfirm paints a bordered apply/confirm card. Width is always honoured.
// Height flows to the body. Confirm/cancel live in Footer; candidate fields
// live in Fields — Card cannot host that shape without the notification bubble.
func RenderConfirm(c Confirm) string {
	c.Footer = screenkit.Sanitize(c.Footer)
	width := c.Width
	if width < 24 {
		width = 24
	}
	frameWidth := width - 2
	if frameWidth < 8 {
		frameWidth = 8
	}
	innerWidth := frameWidth - 4
	if innerWidth < 8 {
		innerWidth = 8
	}

	body := confirmBody(c, innerWidth)
	var bodyLines []string
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}
	rows := composeFramedRows(bodyLines, Padding{Left: 2, Right: 2}, frameWidth, "", c.Footer)
	card := Card{
		Width:            width,
		BorderVisible:    true,
		Style:            StyleRounded,
		BorderForeground: c.BorderForeground,
		AutoHeight:       true,
	}
	return cardStyle(card, frameWidth).Render(strings.Join(rows, "\n"))
}

func confirmBody(c Confirm, innerWidth int) string {
	styles := c.Styles
	var lines []string
	if c.Kicker != "" {
		lines = append(lines, c.Kicker)
	}
	if c.Hint != "" {
		lines = append(lines, styles.Hint.Render(screenkit.Sanitize(c.Hint)))
	}
	if c.Kicker != "" || c.Hint != "" {
		lines = append(lines, styles.Separator.Render(strings.Repeat("─", innerWidth)))
	}
	lines = append(lines, confirmFieldLines(c.Fields, styles, innerWidth)...)
	if c.ListTitle != "" || len(c.ListItems) > 0 {
		lines = append(lines, confirmListLines(c, innerWidth)...)
	}
	lines = append(lines, confirmMessageLines(c.Message, styles)...)
	return strings.Join(lines, "\n")
}

func confirmFieldLines(fields []ConfirmField, styles Styles, innerWidth int) []string {
	var lines []string
	for _, field := range fields {
		label := screenkit.Kicker(styles.Info, screenkit.Sanitize(field.Label))
		value := screenkit.Sanitize(field.Value)
		if value == "" {
			value = " "
		}
		pad := confirmLabelColumn - lipgloss.Width(label)
		if pad < 1 {
			pad = 1
		}
		lines = append(lines, padRight(label+strings.Repeat(" ", pad)+value, innerWidth))
	}
	return lines
}

func confirmListLines(c Confirm, innerWidth int) []string {
	lines := []string{""}
	if c.ListTitle != "" {
		lines = append(lines, c.Styles.Info.Render(screenkit.Sanitize(c.ListTitle)))
	}
	if len(c.ListItems) == 0 {
		if c.ListEmpty != "" {
			lines = append(lines, c.Styles.Hint.Render(screenkit.Sanitize(c.ListEmpty)))
		}
		return lines
	}
	for _, item := range c.ListItems {
		lines = append(lines, padRight("- "+screenkit.Sanitize(item), innerWidth))
	}
	return lines
}

func confirmMessageLines(message string, styles Styles) []string {
	if message == "" {
		return nil
	}
	return []string{"", styles.Hint.Render(screenkit.Sanitize(message))}
}
