package overlay

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/scrollwindow"
)

// Closed presentation knobs for Card. Style and TailSide. Host maps
// config strings onto these; they are the same spellings YAML already
// uses so the mapping is identity.
const (
	StyleRounded = "rounded"
	StyleSquare  = "square"
	StyleDouble  = "double"
	StyleThick   = "thick"
	StyleHidden  = "hidden"
	StyleCustom  = "custom"

	TailBottom = "bottom"
	TailTop    = "top"
	TailLeft   = "left"
	TailRight  = "right"
)

// Padding is the per-side cell inset inside the card border. Zero means
// flush against the border.
type Padding struct {
	Top, Right, Bottom, Left int
}

// Card is the presentation spec for the notification chrome. The host
// owns lifecycle, config, time and id; this is paint only.
type Card struct {
	Width, Height int
	AutoHeight    bool

	BorderVisible    bool
	BorderForeground lipgloss.Color
	BorderBackground lipgloss.Color
	Background       lipgloss.Color

	Padding       Padding
	PaddingInside bool

	Style        string
	CustomBorder lipgloss.Border

	TailSide string
	// Frames are the animation arts; empty means bubble-only (no tail,
	// no frame column). Frame indexes the current art.
	Frames []string
	Frame  int

	// Text is already typed; the host owns the cursor. Scroll is the
	// bubble window offset the host Viewport already clamped.
	Text   string
	Scroll int

	// FormatScrollHint formats the overflow row. The host supplies
	// catalog-resolved copy; overlay centres it.
	FormatScrollHint func(above, below int) string

	// Footer is already painted (host builds it with tokenstrip + catalog).
	Footer string
}

// RenderCard paints the notification chrome. Width is always honoured.
// The card outer height tracks the body's natural height when AutoHeight
// is set — see cardStyle.
//
// Layout follows TailSide:
//
//   - bottom — vertical, bubble above frame, tail "\/" between.
//   - top    — vertical, frame above bubble, tail "/\" between.
//   - right  — horizontal, bubble left, tail ">" column, frame right.
//   - left   — horizontal, frame left, tail "<" column, bubble right.
func RenderCard(c Card) string {
	c = sanitizeCard(c)
	width := c.Width
	// lipgloss Style.Width is the FRAME width (outer block including
	// padding) and the border lives outside. To make the visible card
	// honour Width, hand lipgloss `width-2` when a border is on.
	frameWidth := width
	if c.BorderVisible {
		frameWidth = width - 2
	}
	if frameWidth < 1 {
		frameWidth = 1
	}
	// Body's content rectangle subtracts the padding columns lipgloss
	// will eat from the frame.
	innerWidth := frameWidth - c.Padding.Left - c.Padding.Right
	if innerWidth < 1 {
		innerWidth = 1
	}

	footer := c.Footer
	footerH := 0
	if footer != "" {
		footerH = strings.Count(footer, "\n") + 1
	}

	var rendered string
	if !c.AutoHeight && c.Height > 0 {
		rendered = renderFixedHeight(c, innerWidth, frameWidth, footer, footerH)
	} else {
		rendered = renderFlowHeight(c, innerWidth, frameWidth, footer)
	}
	return cardStyle(c, frameWidth).Render(rendered)
}

// sanitizeCard protects the overlay's public paint boundary. Notification
// models sanitize their inputs before reaching here, but Card is also a shared
// component API and must not trust a direct caller with terminal-bearing text.
func sanitizeCard(c Card) Card {
	c.Text = screenkit.SanitizeMultiline(c.Text)
	if len(c.Frames) > 0 {
		frames := make([]string, len(c.Frames))
		for i, frame := range c.Frames {
			frames[i] = screenkit.SanitizeMultiline(frame)
		}
		c.Frames = frames
	}
	border, valid := sanitizeCustomBorder(c.CustomBorder)
	c.CustomBorder = border
	if c.Style == StyleCustom && !valid {
		// A config glyph can become empty after controls are removed. Use the
		// stable square border rather than letting lipgloss interpret a partial
		// custom border differently across terminal profiles.
		c.Style = StyleSquare
	}
	return c
}

func sanitizeCustomBorder(border lipgloss.Border) (lipgloss.Border, bool) {
	border.Top = screenkit.Sanitize(border.Top)
	border.Bottom = screenkit.Sanitize(border.Bottom)
	border.Left = screenkit.Sanitize(border.Left)
	border.Right = screenkit.Sanitize(border.Right)
	border.TopLeft = screenkit.Sanitize(border.TopLeft)
	border.TopRight = screenkit.Sanitize(border.TopRight)
	border.BottomLeft = screenkit.Sanitize(border.BottomLeft)
	border.BottomRight = screenkit.Sanitize(border.BottomRight)
	valid := border.Top != "" && border.Bottom != "" && border.Left != "" && border.Right != "" &&
		border.TopLeft != "" && border.TopRight != "" && border.BottomLeft != "" && border.BottomRight != ""
	return border, valid
}

// renderFlowHeight builds the inner card content when the card flows
// to its body height (AutoHeight=true). Padding wraps the body
// (manually — see cardStyle's note on doubling). The footer sits on
// its own reserved last row at the frame width: padding.bottom adds
// blank rows BETWEEN the body and the footer, not after it.
func renderFlowHeight(c Card, innerWidth, frameWidth int, footer string) string {
	var bodyLines []string
	body := renderBody(c, innerWidth)
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}
	rows := composeFramedRows(bodyLines, c.Padding, frameWidth, "", footer)
	return strings.Join(rows, "\n")
}

// renderFixedHeight composes the inner card content for the
// AutoHeight=false path. The bubble scrolls inside the body region;
// tail + frame stay fixed. Padding wraps the body — padding.bottom
// stops where the footer's reserved row begins, so the dismiss-key
// hint always reads as a separate band at the bottom of the card.
func renderFixedHeight(c Card, innerWidth, frameWidth int, footer string, footerH int) string {
	frameHeight := c.Height
	if c.BorderVisible {
		frameHeight -= 2
	}
	if frameHeight < 1 {
		frameHeight = 1
	}

	paddingTop := c.Padding.Top
	paddingBottom := c.Padding.Bottom

	// Speculatively reserve one row for the scroll hint; rebuild
	// without the reservation if the bubble actually fits so we
	// don't burn a row on an empty hint.
	const hintReserve = 1
	bodyH := frameHeight - paddingTop - paddingBottom - footerH - hintReserve
	if bodyH < 1 {
		bodyH = 1
	}
	bodyLines, above, below := renderBodyWithBubbleScroll(c, innerWidth, bodyH)
	hint := renderScrollHint(c, above, below, frameWidth)
	if hint == "" {
		// No scroll → reclaim the reserved row.
		bodyH = frameHeight - paddingTop - paddingBottom - footerH
		if bodyH < 1 {
			bodyH = 1
		}
		bodyLines, _, _ = renderBodyWithBubbleScroll(c, innerWidth, bodyH)
	}

	if c.PaddingInside {
		bodyLines = padToHeightCentered(bodyLines, bodyH)
	} else {
		bodyLines = padToHeightTop(bodyLines, bodyH)
	}

	rows := composeFramedRows(bodyLines, c.Padding, frameWidth, hint, footer)
	return strings.Join(rows, "\n")
}

// composeFramedRows lays out the inner card content row-by-row:
//   - paddingTop blank rows of frameWidth spaces
//   - each bodyLine prefixed with padding.Left spaces
//   - paddingBottom blank rows
//   - scrollHint (already frameWidth wide) — sits between the body
//     and the footer when bubble content overflows
//   - footer (already frameWidth wide) on its own reserved bottom row
//
// Horizontal padding is applied here rather than via lipgloss.Padding
// so the scroll hint and footer can break free of the indent and sit
// edge-to-edge against the border.
func composeFramedRows(bodyLines []string, p Padding, frameWidth int, scrollHint, footer string) []string {
	rows := make([]string, 0, p.Top+len(bodyLines)+p.Bottom+2)
	blank := strings.Repeat(" ", frameWidth)
	leftPad := strings.Repeat(" ", p.Left)
	for i := 0; i < p.Top; i++ {
		rows = append(rows, blank)
	}
	for _, line := range bodyLines {
		rows = append(rows, leftPad+line)
	}
	for i := 0; i < p.Bottom; i++ {
		rows = append(rows, blank)
	}
	if scrollHint != "" {
		rows = append(rows, scrollHint)
	}
	if footer != "" {
		rows = append(rows, footer)
	}
	return rows
}

// renderBodyWithBubbleScroll lays out bubble + tail + frame so the
// bubble respects Card.Scroll while the tail and frame stay fixed.
// Vertical layouts (top/bottom) drive the scroll math here;
// horizontal layouts fall back to renderBody since bubble + frame
// share rows there and a column-style scroll would defeat the
// side-by-side intent.
func renderBodyWithBubbleScroll(c Card, innerWidth, bodyH int) ([]string, int, int) {
	side := c.TailSide
	if len(c.Frames) > 0 && (side == TailLeft || side == TailRight) {
		body := renderBody(c, innerWidth)
		if body == "" {
			return nil, 0, 0
		}
		return strings.Split(body, "\n"), 0, 0
	}

	bubble := renderBubble(c, innerWidth)
	tail := renderTail(c, innerWidth)
	frame := renderFrame(c, innerWidth)

	var bubbleLines []string
	if bubble != "" {
		bubbleLines = strings.Split(bubble, "\n")
	}
	var frameLines []string
	if frame != "" {
		frameLines = strings.Split(frame, "\n")
	}
	tailH := 0
	if tail != "" {
		tailH = 1
	}

	fixedH := tailH + len(frameLines)
	bubbleH := bodyH - fixedH
	if bubbleH < 1 {
		bubbleH = 1
	}
	visible, above, below := scrollwindow.SliceLines(bubbleLines, c.Scroll, bubbleH)

	rows := make([]string, 0, bodyH)
	if side == TailTop {
		rows = append(rows, frameLines...)
		if tail != "" {
			rows = append(rows, tail)
		}
		rows = append(rows, visible...)
		return rows, above, below
	}
	if side != "" && side != TailBottom {
		panic("invalid notification bubble.tail_side: " + side)
	}
	rows = append(rows, visible...)
	if tail != "" {
		rows = append(rows, tail)
	}
	rows = append(rows, frameLines...)
	return rows, above, below
}

func renderScrollHint(c Card, above, below, frameWidth int) string {
	if above <= 0 && below <= 0 {
		return ""
	}
	if c.FormatScrollHint == nil {
		return ""
	}
	text := c.FormatScrollHint(above, below)
	text = screenkit.Sanitize(text)
	if text == "" {
		return ""
	}
	return centerLine(text, frameWidth)
}

func padToHeightCentered(lines []string, h int) []string {
	if len(lines) >= h {
		return lines[:h]
	}
	extra := h - len(lines)
	top := extra / 2
	bottom := extra - top
	out := make([]string, 0, h)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	out = append(out, lines...)
	for i := 0; i < bottom; i++ {
		out = append(out, "")
	}
	return out
}

func padToHeightTop(lines []string, h int) []string {
	if len(lines) >= h {
		return lines[:h]
	}
	out := make([]string, len(lines), h)
	copy(out, lines)
	for i := len(lines); i < h; i++ {
		out = append(out, "")
	}
	return out
}

func renderBody(c Card, innerWidth int) string {
	if len(c.Frames) == 0 {
		return renderBubble(c, innerWidth)
	}
	switch c.TailSide {
	case TailTop:
		frame := renderFrame(c, innerWidth)
		tail := renderTail(c, innerWidth)
		bubble := renderBubble(c, innerWidth)
		return joinNonEmpty(frame, tail, bubble)
	case TailBottom:
		bubble := renderBubble(c, innerWidth)
		tail := renderTail(c, innerWidth)
		frame := renderFrame(c, innerWidth)
		return joinNonEmpty(bubble, tail, frame)
	case TailLeft:
		return renderHorizontal(c, innerWidth, false)
	case TailRight:
		return renderHorizontal(c, innerWidth, true)
	}
	panic("invalid notification bubble.tail_side: " + c.TailSide)
}

func renderHorizontal(c Card, innerWidth int, bubbleLeft bool) string {
	frameW := frameNaturalWidth(c)
	tailGlyph := ">"
	if !bubbleLeft {
		tailGlyph = "<"
	}
	const minBubbleW = 6
	bubbleW := innerWidth - frameW - 1 // 1 cell for the tail glyph column
	if bubbleW < minBubbleW {
		bubbleW = minBubbleW
		if frameW > innerWidth-bubbleW-1 {
			frameW = innerWidth - bubbleW - 1
			if frameW < 1 {
				frameW = 1
			}
		}
	}

	bubble := renderBubble(c, bubbleW)
	frame := renderFrameRaw(c, frameW)
	tailCol := tailColumn(tailGlyph, lipgloss.Height(frame))

	if bubbleLeft {
		return lipgloss.JoinHorizontal(lipgloss.Center, bubble, tailCol, frame)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, frame, tailCol, bubble)
}

func frameNaturalWidth(c Card) int {
	frames := c.Frames
	if len(frames) == 0 {
		return 0
	}
	max := 0
	for _, f := range frames {
		for _, line := range strings.Split(strings.Trim(f, "\n"), "\n") {
			if w := utf8.RuneCountInString(line); w > max {
				max = w
			}
		}
	}
	return max
}

func renderFrameRaw(c Card, width int) string {
	frames := c.Frames
	if len(frames) == 0 {
		return ""
	}
	idx := c.Frame % len(frames)
	frameValue := strings.Trim(frames[idx], "\n")
	lines := strings.Split(frameValue, "\n")
	for i, line := range lines {
		if utf8.RuneCountInString(line) > width {
			runes := []rune(line)
			lines[i] = string(runes[:width])
		}
	}
	return strings.Join(lines, "\n")
}

func tailColumn(glyph string, height int) string {
	if height < 1 {
		height = 1
	}
	rows := make([]string, height)
	mid := height / 2
	for i := range rows {
		if i == mid {
			rows[i] = glyph
		} else {
			rows[i] = " "
		}
	}
	return strings.Join(rows, "\n")
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n")
}

func renderBubble(c Card, width int) string {
	text := c.Text
	wrapped := softWrap(text, width-2)
	lines := strings.Split(wrapped, "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return ""
	}
	if len(lines) == 1 {
		// Both opening and closing quotes land on the same row, so
		// reserve 4 cells of chrome ("“ " + " ”") instead of the
		// 2-per-edge budget the multi-line branch uses. Without this
		// the single-line bubble overflows the configured width by 2
		// cells and lipgloss wraps it into a phantom extra row.
		body := padRight(lines[0], width-4)
		return "“ " + body + " ”"
	}
	for i, line := range lines {
		lines[i] = padRight(line, width-2)
	}
	lines[0] = "“ " + lines[0]
	lines[len(lines)-1] = lines[len(lines)-1] + " ”"
	return strings.Join(lines, "\n")
}

func renderTail(c Card, width int) string {
	switch c.TailSide {
	case "":
		return ""
	case TailBottom:
		return centerLine("\\/", width)
	case TailTop:
		return centerLine("/\\", width)
	case TailLeft:
		return "<"
	case TailRight:
		return padRight("", width-1) + ">"
	}
	panic("invalid notification bubble.tail_side: " + c.TailSide)
}

func renderFrame(c Card, width int) string {
	frames := c.Frames
	if len(frames) == 0 {
		return ""
	}
	idx := c.Frame % len(frames)
	frameValue := strings.Trim(frames[idx], "\n")
	lines := strings.Split(frameValue, "\n")
	lines = dedentBlock(lines)
	// Center each row so the rectangle's bounding box sits at the
	// card's horizontal mid-line, aligned with the centered tail
	// glyph that points to it.
	for i, line := range lines {
		lines[i] = centerLine(strings.TrimRight(line, " "), width)
	}
	return strings.Join(lines, "\n")
}

func dedentBlock(lines []string) []string {
	min := -1
	for _, line := range lines {
		stripped := strings.TrimLeft(line, " ")
		if stripped == "" {
			continue
		}
		indent := len(line) - len(stripped)
		if min < 0 || indent < min {
			min = indent
		}
	}
	if min <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) >= min {
			out[i] = line[min:]
		} else {
			out[i] = ""
		}
	}
	return out
}

func cardStyle(c Card, width int) lipgloss.Style {
	style := lipgloss.NewStyle().Width(width)
	// Padding (vertical AND horizontal) is laid out manually by
	// renderFlowHeight / renderFixedHeight. Letting lipgloss apply
	// .Padding here would double the rows (manual rows + lipgloss
	// rows) and would also indent the footer — the user wants the
	// footer flush at the frame width on its own reserved band.
	if !c.BorderVisible || c.Style == StyleHidden {
		return style
	}

	style = style.Border(cardBorder(c))
	if c.BorderForeground != "" {
		style = style.BorderForeground(c.BorderForeground)
	}
	if c.BorderBackground != "" {
		style = style.BorderBackground(c.BorderBackground)
	}
	if c.Background != "" {
		style = style.Background(c.Background)
	}
	return style
}

func cardBorder(c Card) lipgloss.Border {
	switch c.Style {
	case StyleSquare:
		return lipgloss.NormalBorder()
	case StyleDouble:
		return lipgloss.DoubleBorder()
	case StyleThick:
		return lipgloss.ThickBorder()
	case StyleHidden:
		return lipgloss.HiddenBorder()
	case StyleCustom:
		return c.CustomBorder
	}
	return lipgloss.RoundedBorder()
}

func softWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var b strings.Builder
	for i, paragraph := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		runes := []rune(paragraph)
		for len(runes) > width {
			b.WriteString(string(runes[:width]))
			b.WriteString("\n")
			runes = runes[width:]
		}
		b.WriteString(string(runes))
	}
	return b.String()
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	current := utf8.RuneCountInString(s)
	if current >= width {
		runes := []rune(s)
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-current)
}

func centerLine(s string, width int) string {
	current := utf8.RuneCountInString(s)
	if current >= width {
		return s
	}
	left := (width - current) / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", width-current-left)
}
