package notification

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/keyfooter"
	"omakiten/internal/tui/components/viewport"
)

// State enumerates the notification's lifecycle. Appearing is the typing-in
// phase; Settled is the post-typing phase that accepts dismiss input
// and starts the timeout clock when applicable.
type State int

const (
	StateAppearing State = iota
	StateSettled
)

// Options is the input to New. Notification carries every render+behaviour
// knob (size, border, animation, position, dismiss, typing speed);
// Text is the resolved bubble copy. Theme drives the colour resolver
// at every View() so a runtime theme switch repaints the card.
type Options struct {
	Notification config.Notification
	Theme        config.Theme
	Text         string
	DetailText   string
}

// Model owns the running notification notification. The parent typically
// holds an optional pointer (nil = no notification active). On every tea.Msg
// route messages through Update; emit on screen via View() and place
// via Overlay at Position().
type Model struct {
	notification config.Notification
	theme        config.Theme
	state        State
	text         string
	detailText   string
	page         int
	cursor       int // rune cursor into text
	frame        int
	bubble       viewport.Model
	id           int64 // tick generation; replaced notifications get a new id
	dismissed    bool
}

// DismissedMsg is sent when the notification should be removed by the parent.
// It carries the source notification id so a parent can match against the
// current notification and ignore stale dismiss messages from an older
// notification that was already replaced.
type DismissedMsg struct{ ID int64 }

type typingTickMsg struct{ id int64 }
type frameTickMsg struct{ id int64 }
type timeoutTickMsg struct{ id int64 }

var nextID int64

// nextSession returns a monotonically increasing id used to tag ticks
// so a notification that was just replaced ignores the old notification's pending
// timer messages. The counter is process-global; callers do not share
// id space across processes so monotonicity inside a run is enough.
func nextSession() int64 {
	nextID++
	return nextID
}

// New constructs a Model in StateAppearing (or StateSettled when
// TypingMsPerChar == 0) and returns the initial command batch that
// drives the typing + frame ticks (and the timeout tick when the
// dismiss mode is timeout AND the notification starts settled).
func New(opts Options) (Model, tea.Cmd) {
	id := nextSession()
	m := Model{
		notification: opts.Notification,
		theme:        opts.Theme,
		text:         opts.Text,
		detailText:   opts.DetailText,
		bubble:       viewport.New(),
		id:           id,
	}
	if *m.notification.TypingMsPerChar <= 0 {
		m.cursor = utf8.RuneCountInString(m.currentText())
		m.state = StateSettled
	}
	cmds := []tea.Cmd{m.frameCmd()}
	if m.state == StateAppearing {
		cmds = append(cmds, m.typingCmd())
	} else if m.notification.Dismiss.Mode == config.NotificationDismissModeTimeout {
		cmds = append(cmds, m.timeoutCmd())
	}
	return m, tea.Batch(cmds...)
}

// ID returns the session id assigned at New; useful for parents that
// want to compare against an incoming DismissedMsg.
func (m Model) ID() int64 { return m.id }

// State returns the current lifecycle state. Tests use this to assert
// transitions without poking at private fields.
func (m Model) State() State { return m.state }

// Position returns the configured anchor; the parent uses this when
// calling Overlay.
func (m Model) Position() Position { return Position(m.notification.Position) }

// Theme replaces the theme used at the next View() call. Allows the
// runtime theme switcher to repaint without rebuilding the notification.
func (m Model) Theme(theme config.Theme) Model {
	m.theme = theme
	return m
}

// Update consumes a tea.Msg and returns the next model plus any
// commands. The parent dispatches every msg here while a notification is
// alive; key messages are consumed (not forwarded to the app under
// the notification) so dismiss + scroll have priority.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.dismissed {
		return m, nil
	}
	switch v := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(v)
	case typingTickMsg:
		if v.id != m.id {
			return m, nil
		}
		return m.advanceTyping()
	case frameTickMsg:
		if v.id != m.id {
			return m, nil
		}
		return m.advanceFrame()
	case timeoutTickMsg:
		if v.id != m.id {
			return m, nil
		}
		return m.Dismiss()
	}
	return m, nil
}

// Dismiss flips the model to a dismissed state and returns a command
// that emits DismissedMsg. Parents call this for next_status mode or
// any external trigger (window close, etc.).
func (m Model) Dismiss() (Model, tea.Cmd) {
	if m.dismissed {
		return m, nil
	}
	id := m.id
	m.dismissed = true
	return m, func() tea.Msg { return DismissedMsg{ID: id} }
}

func (m Model) handleKey(key tea.KeyMsg) (Model, tea.Cmd) {
	keyStr := key.String()
	if keyStr == "tab" && m.hasDetailText() {
		m.page = 1 - m.page
		m.cursor = utf8.RuneCountInString(m.currentText())
		m.state = StateSettled
		m.bubble.Scroll = 0
		return m, nil
	}
	if isScrollKey(keyStr) {
		bubble, _ := m.bubble.Update(key, m.bubbleViewport())
		m.bubble = bubble
		return m, nil
	}
	if m.state == StateSettled && m.dismissKeysEnabled() {
		for _, k := range m.notification.Dismiss.Keys {
			if k == keyStr {
				return m.Dismiss()
			}
		}
	}
	// Consume the key — don't forward to the app underneath.
	return m, nil
}

func (m Model) dismissKeysEnabled() bool {
	if len(m.notification.Dismiss.Keys) == 0 {
		return false
	}
	return m.notification.Dismiss.Mode == config.NotificationDismissModeKey || m.notification.Dismiss.Mode == config.NotificationDismissModeTimeout
}

func (m Model) advanceTyping() (Model, tea.Cmd) {
	total := utf8.RuneCountInString(m.currentText())
	if m.cursor < total {
		m.cursor++
	}
	if m.cursor >= total {
		m.state = StateSettled
		var timeoutCmd tea.Cmd
		if m.notification.Dismiss.Mode == config.NotificationDismissModeTimeout {
			timeoutCmd = m.timeoutCmd()
		}
		// Stop scheduling further typing ticks once Settled.
		return m, timeoutCmd
	}
	return m, m.typingCmd()
}

func (m Model) advanceFrame() (Model, tea.Cmd) {
	frames := m.notification.Animation
	if len(frames) > 0 {
		m.frame = (m.frame + 1) % len(frames)
	}
	return m, m.frameCmd()
}

func (m Model) typingCmd() tea.Cmd {
	if *m.notification.TypingMsPerChar <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(*m.notification.TypingMsPerChar) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return typingTickMsg{id: id} })
}

func (m Model) frameCmd() tea.Cmd {
	if m.notification.FrameIntervalMs <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.notification.FrameIntervalMs) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return frameTickMsg{id: id} })
}

func (m Model) timeoutCmd() tea.Cmd {
	if m.notification.Dismiss.AfterMs <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.notification.Dismiss.AfterMs) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return timeoutTickMsg{id: id} })
}

func isScrollKey(s string) bool {
	switch s {
	case "j", "k", "up", "down", "pgup", "pgdown", "ctrl+u", "ctrl+d", "g", "G", "home", "end":
		return true
	}
	return false
}

// View renders the card the parent overlays on the base view. Width
// is always honoured. The card outer height tracks the body's
// natural height (no padding) — see cardStyle.
//
// Layout follows Bubble.TailSide:
//
//   - bottom           — vertical, bubble above frame, tail "\V/" between.
//   - top              — vertical, frame above bubble, tail "/\" between.
//   - right            — horizontal, bubble left, tail ">" column, frame right.
//   - left             — horizontal, frame left, tail "<" column, bubble right.
//
// Colors resolve against the live theme on every call so a runtime
// theme switch is visible at the next frame.
func (m Model) View() string {
	if m.dismissed {
		return ""
	}
	width := m.notification.Size.Width
	// lipgloss Style.Width is the FRAME width (outer block including
	// padding) and the border lives outside. To make the visible card
	// honour Size.Width, hand lipgloss `width-2` when a border is on.
	frameWidth := width
	if *m.notification.Border.Visible {
		frameWidth = width - 2
	}
	if frameWidth < 1 {
		frameWidth = 1
	}
	// Body's content rectangle subtracts the padding columns lipgloss
	// will eat from the frame.
	innerWidth := frameWidth - *m.notification.Padding.Left - *m.notification.Padding.Right
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Footer renders at the FRAME width — outside horizontal padding —
	// so the dismiss hint reads as a deliberate bottom band edge to
	// edge, not indented like the body content above it.
	footer := m.renderFooter(frameWidth)
	footerH := 0
	if footer != "" {
		footerH = strings.Count(footer, "\n") + 1
	}

	var rendered string
	if !m.notificationAutoHeight() && m.notification.Size.Height > 0 {
		rendered = m.renderFixedHeight(innerWidth, frameWidth, footer, footerH)
	} else {
		rendered = m.renderFlowHeight(innerWidth, frameWidth, footer)
	}
	return m.cardStyle(frameWidth).Render(rendered)
}

// renderFlowHeight builds the inner card content when the card flows
// to its body height (auto_height=true). Padding wraps the body
// (manually — see cardStyle's note on doubling). The footer sits on
// its own reserved last row at the frame width: padding.bottom adds
// blank rows BETWEEN the body and the footer, not after it.
//
// auto_height=true means there's no scrollable region (the card grows
// to fit), so the scroll-hint slot is unused here.
func (m Model) renderFlowHeight(innerWidth, frameWidth int, footer string) string {
	body := m.renderBody(innerWidth)
	var bodyLines []string
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}
	rows := composeFramedRows(bodyLines, *m.notification.Padding, frameWidth, "", footer)
	return strings.Join(rows, "\n")
}

// renderFixedHeight composes the inner card content for the
// auto_height=false path. The bubble scrolls inside the body region;
// tail + frame stay fixed. Padding wraps the body — padding.bottom
// stops where the footer's reserved row begins, so the dismiss-key
// hint always reads as a separate band at the bottom of the card.
// When the bubble overflows the visible window, a one-row scroll
// hint ("▲ N above · ▼ N below") sits between padding.bottom and
// the footer so the user always knows there's more text to scroll.
func (m Model) renderFixedHeight(innerWidth, frameWidth int, footer string, footerH int) string {
	frameHeight := m.notification.Size.Height
	if *m.notification.Border.Visible {
		frameHeight -= 2
	}
	if frameHeight < 1 {
		frameHeight = 1
	}

	paddingTop := *m.notification.Padding.Top
	paddingBottom := *m.notification.Padding.Bottom

	// Speculatively reserve one row for the scroll hint; rebuild
	// without the reservation if the bubble actually fits so we
	// don't burn a row on an empty hint.
	const hintReserve = 1
	bodyH := frameHeight - paddingTop - paddingBottom - footerH - hintReserve
	if bodyH < 1 {
		bodyH = 1
	}
	bodyLines, above, below := m.renderBodyWithBubbleScroll(innerWidth, bodyH)
	hint := renderScrollHint(above, below, frameWidth)
	if hint == "" {
		// No scroll → reclaim the reserved row.
		bodyH = frameHeight - paddingTop - paddingBottom - footerH
		if bodyH < 1 {
			bodyH = 1
		}
		bodyLines, _, _ = m.renderBodyWithBubbleScroll(innerWidth, bodyH)
	}

	if *m.notification.PaddingInside {
		bodyLines = padToHeightCentered(bodyLines, bodyH)
	} else {
		bodyLines = padToHeightTop(bodyLines, bodyH)
	}

	rows := composeFramedRows(bodyLines, *m.notification.Padding, frameWidth, hint, footer)
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
func composeFramedRows(bodyLines []string, p config.NotificationPadding, frameWidth int, scrollHint, footer string) []string {
	rows := make([]string, 0, *p.Top+len(bodyLines)+*p.Bottom+2)
	blank := strings.Repeat(" ", frameWidth)
	leftPad := strings.Repeat(" ", *p.Left)
	for i := 0; i < *p.Top; i++ {
		rows = append(rows, blank)
	}
	for _, line := range bodyLines {
		rows = append(rows, leftPad+line)
	}
	for i := 0; i < *p.Bottom; i++ {
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
// bubble respects m.bubble.Scroll while the tail and frame stay
// fixed. Vertical layouts (top/bottom) drive the scroll math here;
// horizontal layouts fall back to renderBody since bubble + frame
// share rows there and a column-style scroll would defeat the
// side-by-side intent.
//
// Returns the rendered rows plus the row counts hidden above/below
// the visible bubble window so the caller can render a scroll hint.
func (m Model) renderBodyWithBubbleScroll(innerWidth, bodyH int) ([]string, int, int) {
	side := m.notification.Bubble.TailSide
	if len(m.notification.Animation) > 0 && (side == config.NotificationTailLeft || side == config.NotificationTailRight) {
		body := m.renderBody(innerWidth)
		if body == "" {
			return nil, 0, 0
		}
		return strings.Split(body, "\n"), 0, 0
	}

	bubble := m.renderBubble(innerWidth)
	tail := m.renderTail(innerWidth)
	frame := m.renderFrame(innerWidth)

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
	visible, above, below := viewport.Slice(bubbleLines, m.bubble.Scroll, bubbleH)

	rows := make([]string, 0, bodyH)
	if side == config.NotificationTailTop {
		rows = append(rows, frameLines...)
		if tail != "" {
			rows = append(rows, tail)
		}
		rows = append(rows, visible...)
		return rows, above, below
	}
	if side != "" && side != config.NotificationTailBottom {
		panic("invalid notification bubble.tail_side: " + side)
	}
	rows = append(rows, visible...)
	if tail != "" {
		rows = append(rows, tail)
	}
	rows = append(rows, frameLines...)
	return rows, above, below
}

// renderScrollHint formats the "▲ N above · ▼ N below" indicator the
// user expects when bubble content is clipped by the body region.
// Returns "" when there's nothing hidden — caller should NOT reserve
// a row for an empty hint.
func renderScrollHint(above, below, frameWidth int) string {
	if above <= 0 && below <= 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if above > 0 {
		parts = append(parts, fmt.Sprintf("▲ %d above", above))
	}
	if below > 0 {
		parts = append(parts, fmt.Sprintf("▼ %d below", below))
	}
	return centerLine(strings.Join(parts, " · "), frameWidth)
}

// padToHeightCentered shrinks or grows `lines` to exactly `h` rows
// with any extra rows split top + bottom (top gets the smaller half
// when h-len is odd).
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

// padToHeightTop shrinks or grows `lines` to exactly `h` rows with
// any extra rows tacked on at the bottom.
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

// notificationAutoHeight reports whether the card height should track
// the body's natural row count or be pinned to Size.Height. The value is
// required by notification YAML and validated before the renderer sees it.
func (m Model) notificationAutoHeight() bool {
	return *m.notification.AutoHeight
}

// renderFooter paints the same compact keybinding treatment used by the
// main TUI footer. It advertises tab paging when detail text is present and
// key-based dismiss controls when the notification uses dismiss.mode=key.
func (m Model) renderFooter(innerWidth int) string {
	if !*m.notification.FooterVisible {
		return ""
	}
	tokens := make([]keyfooter.Token, 0, 2)
	if m.hasDetailText() {
		tokens = append(tokens, keyfooter.Token{Key: "tab", Label: "details", Primary: true})
	}
	if m.dismissKeysEnabled() {
		if key := footerDismissKey(m.notification.Dismiss.Keys); key != "" {
			tokens = append(tokens, keyfooter.Token{Key: key, Label: "close", Primary: !m.hasDetailText()})
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	styles := keyfooter.ThemeStyles(m.theme)
	styles.Align = notificationFooterPosition(m.notification.FooterPosition)
	return keyfooter.RenderWrapped(tokens, styles, innerWidth)
}

func notificationFooterPosition(position string) string {
	switch position {
	case config.NotificationFooterLeft:
		return keyfooter.AlignLeft
	case config.NotificationFooterCenter:
		return keyfooter.AlignCenter
	case config.NotificationFooterRight:
		return keyfooter.AlignRight
	}
	panic("invalid notification footer_position: " + position)
}

// footerDismissKey formats the dismiss-key list as "esc/q/enter/space" using
// readable labels for the few keys whose Key.String() form would surprise the
// user (a literal space becomes "space").
func footerDismissKey(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	labels := make([]string, 0, len(keys))
	for _, k := range keys {
		switch k {
		case " ":
			labels = append(labels, "space")
		case "":
			continue
		default:
			labels = append(labels, k)
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, "/")
}

// renderBody composes bubble + tail + frame in the order/orientation
// dictated by Bubble.TailSide. Vertical layouts (top/bottom) join
// with newlines; horizontal layouts (left/right) split innerWidth
// between the bubble and the frame and join via lipgloss.JoinHorizontal.
//
// When the notification declares no animation frames the body
// collapses to bubble-only — no tail, no frame column — so a
// plain-text notification looks deliberate instead of pointing at
// empty space.
func (m Model) renderBody(innerWidth int) string {
	if len(m.notification.Animation) == 0 {
		return m.renderBubble(innerWidth)
	}
	switch m.notification.Bubble.TailSide {
	case config.NotificationTailTop:
		frame := m.renderFrame(innerWidth)
		tail := m.renderTail(innerWidth)
		bubble := m.renderBubble(innerWidth)
		return joinNonEmpty(frame, tail, bubble)
	case config.NotificationTailBottom:
		bubble := m.renderBubble(innerWidth)
		tail := m.renderTail(innerWidth)
		frame := m.renderFrame(innerWidth)
		return joinNonEmpty(bubble, tail, frame)
	case config.NotificationTailLeft:
		return m.renderHorizontal(innerWidth, false)
	case config.NotificationTailRight:
		return m.renderHorizontal(innerWidth, true)
	}
	panic("invalid notification bubble.tail_side: " + m.notification.Bubble.TailSide)
}

// renderHorizontal lays out bubble + tail glyph column + frame
// side-by-side. bubbleLeft=true puts the bubble on the left + frame
// on the right (tail glyph ">", pointing at the frame); false flips
// it. The frame's natural width comes from the active animation's
// widest line; bubble takes the remainder. Floors bubbleW at 6 cells
// so the speech keeps the quote-mark chrome plus at least one
// character of body.
func (m Model) renderHorizontal(innerWidth int, bubbleLeft bool) string {
	frameW := m.frameNaturalWidth()
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

	bubble := m.renderBubble(bubbleW)
	frame := m.renderFrameRaw(frameW)
	tailCol := tailColumn(tailGlyph, lipgloss.Height(frame))

	if bubbleLeft {
		return lipgloss.JoinHorizontal(lipgloss.Center, bubble, tailCol, frame)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, frame, tailCol, bubble)
}

// frameNaturalWidth returns the widest line across the active
// animation's frames. Computed across all frames so animation ticks
// don't reflow the card mid-show.
func (m Model) frameNaturalWidth() int {
	frames := m.notification.Animation
	if len(frames) == 0 {
		return 0
	}
	max := 0
	for _, f := range frames {
		for _, line := range strings.Split(strings.Trim(f.Value, "\n"), "\n") {
			if w := utf8.RuneCountInString(line); w > max {
				max = w
			}
		}
	}
	return max
}

// renderFrameRaw returns the frame value trimmed to the natural
// rectangle (no surrounding pad). Used by the horizontal layout where
// the frame is its own column.
func (m Model) renderFrameRaw(width int) string {
	frames := m.notification.Animation
	if len(frames) == 0 {
		return ""
	}
	idx := m.frame % len(frames)
	frameValue := strings.Trim(frames[idx].Value, "\n")
	lines := strings.Split(frameValue, "\n")
	for i, line := range lines {
		if utf8.RuneCountInString(line) > width {
			runes := []rune(line)
			lines[i] = string(runes[:width])
		}
	}
	return strings.Join(lines, "\n")
}

// tailColumn returns a vertical stack of `glyph` characters tall
// enough to span `height` rows — used by the horizontal layout to
// paint the speech-tail between the bubble and the frame.
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

func (m Model) typed() string {
	text := m.currentText()
	if m.cursor >= utf8.RuneCountInString(text) {
		return text
	}
	runes := []rune(text)
	return string(runes[:m.cursor])
}

func (m Model) currentText() string {
	if m.page == 1 && m.hasDetailText() {
		return m.detailText
	}
	return m.text
}

func (m Model) hasDetailText() bool {
	return strings.TrimSpace(m.detailText) != ""
}

func (m Model) renderBubble(width int) string {
	text := m.typed()
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

func (m Model) bubbleViewport() int {
	height := m.notification.Size.Height
	bubbleHeight := height / 2
	if bubbleHeight < 1 {
		bubbleHeight = 1
	}
	return bubbleHeight
}

func (m Model) renderTail(width int) string {
	switch m.notification.Bubble.TailSide {
	case "":
		return ""
	case config.NotificationTailBottom:
		return centerLine("\\/", width)
	case config.NotificationTailTop:
		return centerLine("/\\", width)
	case config.NotificationTailLeft:
		return "<"
	case config.NotificationTailRight:
		return padRight("", width-1) + ">"
	}
	panic("invalid notification bubble.tail_side: " + m.notification.Bubble.TailSide)
}

func (m Model) renderFrame(width int) string {
	frames := m.notification.Animation
	if len(frames) == 0 {
		return ""
	}
	idx := m.frame % len(frames)
	frameValue := strings.Trim(frames[idx].Value, "\n")
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

// dedentBlock strips the largest common run of leading spaces from
// every non-empty line so the YAML's hand-indented ASCII art is
// re-anchored to column 0 before centering. Blank lines are left
// alone so a frame with empty rows in the middle does not collapse.
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

// cardStyle builds the lipgloss style for the rendered card. Width
// is honoured strictly. The card outer height is intentionally NOT
// imposed here — it tracks the rendered body's natural height so the
// user never sees blank padding rows above or below the bubble +
// frame stack. config.NotificationSize.Height keeps its existing meaning
// for the bubble's scroll viewport (see bubbleViewport); to control
// the focused card's visual aspect, dial config.tui.notification.focused
// width against the body's natural row count.
func (m Model) cardStyle(width int) lipgloss.Style {
	style := lipgloss.NewStyle().Width(width)
	// Padding (vertical AND horizontal) is laid out manually by
	// renderFlowHeight / renderFixedHeight. Letting lipgloss apply
	// .Padding here would double the rows (manual rows + lipgloss
	// rows) and would also indent the footer — the user wants the
	// footer flush at the frame width on its own reserved band.
	if !*m.notification.Border.Visible || m.notification.Style == config.NotificationStyleHidden {
		return style
	}

	border := notificationBorder(m.notification)
	style = style.Border(border)
	if c := m.notification.Border.Color; c != "" {
		if rc, err := config.ResolveColor(c, m.theme); err == nil && !rc.IsTransparent() {
			style = style.BorderForeground(rc.TerminalColor())
		}
	}
	if c := m.notification.Border.Background; c != "" {
		if rc, err := config.ResolveColor(c, m.theme); err == nil && !rc.IsTransparent() {
			style = style.BorderBackground(rc.TerminalColor())
		}
	}
	if bg := m.notification.Background; bg != "" {
		if rc, err := config.ResolveColor(bg, m.theme); err == nil && !rc.IsTransparent() {
			style = style.Background(rc.TerminalColor())
		}
	}
	return style
}

func notificationBorder(b config.Notification) lipgloss.Border {
	switch b.Style {
	case config.NotificationStyleSquare:
		return lipgloss.NormalBorder()
	case config.NotificationStyleDouble:
		return lipgloss.DoubleBorder()
	case config.NotificationStyleThick:
		return lipgloss.ThickBorder()
	case config.NotificationStyleHidden:
		return lipgloss.HiddenBorder()
	case config.NotificationStyleCustom:
		return lipgloss.Border{
			Top:         b.CustomBorder.Top,
			Bottom:      b.CustomBorder.Bottom,
			Left:        b.CustomBorder.Left,
			Right:       b.CustomBorder.Right,
			TopLeft:     b.CustomBorder.TopLeft,
			TopRight:    b.CustomBorder.TopRight,
			BottomLeft:  b.CustomBorder.BottomLeft,
			BottomRight: b.CustomBorder.BottomRight,
		}
	}
	return lipgloss.RoundedBorder()
}

// softWrap splits text into lines no wider than width visible cells.
// Naïve byte-based wrap because the renderer never receives styled
// markup — bubble content is always plain text resolved from the
// event payload.
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
