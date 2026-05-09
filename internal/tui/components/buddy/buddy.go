package buddy

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/viewport"
)

// State enumerates the buddy's lifecycle. Appearing is the typing-in
// phase; Settled is the post-typing phase that accepts dismiss input
// and starts the timeout clock when applicable.
type State int

const (
	StateAppearing State = iota
	StateSettled
)

// DismissMode names how the buddy goes away. Key listens for one of
// the configured Keys and emits DismissedMsg; Timeout fires AfterMs ms
// after entering Settled; NextStatus expects the parent to call
// Dismiss() at the appropriate domain transition.
type DismissMode string

const (
	DismissModeKey        DismissMode = "key"
	DismissModeTimeout    DismissMode = "timeout"
	DismissModeNextStatus DismissMode = "next_status"
)

// DismissModes is the closed set used by the hooks validator and the
// docs.
var DismissModes = []DismissMode{DismissModeKey, DismissModeTimeout, DismissModeNextStatus}

// DismissConfig is the parsed form of the buddy.show args.dismiss
// object. Mode selects which fields apply: Keys for key, AfterMs for
// timeout, neither for next_status. The hooks validator rejects
// inconsistent shapes at LoadBundle.
type DismissConfig struct {
	Mode    DismissMode
	Keys    []string
	AfterMs int
}

// Options is the input to New. Every field is required; the hooks
// validator filters out incomplete shapes upstream so the constructor
// does not have to defend against zero values.
type Options struct {
	Buddy           config.Buddy
	Theme           config.Theme
	Animation       string
	Text            string
	Position        Position
	Dismiss         DismissConfig
	TypingMsPerChar int
	FrameIntervalMs int
}

// Model owns the running mascot. The parent typically holds an
// optional pointer (nil = no buddy active). On every tea.Msg, route
// non-key messages and key messages through Update; emit on screen via
// View() and place via Overlay.
type Model struct {
	buddy           config.Buddy
	theme           config.Theme
	state           State
	text            string
	cursor          int // rune cursor into text
	frame           int
	anim            string
	position        Position
	dismiss         DismissConfig
	bubble          viewport.Model
	typingMsPerChar int
	frameIntervalMs int
	id              int64 // tick generation; replaced buddies get a new id
	dismissed       bool
}

// DismissedMsg is sent when the buddy should be removed by the parent.
// It carries the source buddy id so a parent can match against the
// current buddy and ignore stale dismiss messages from an older
// buddy that was already replaced.
type DismissedMsg struct{ ID int64 }

type typingTickMsg struct{ id int64 }
type frameTickMsg struct{ id int64 }
type timeoutTickMsg struct{ id int64 }

var nextID int64

// nextSession returns a monotonically increasing id used to tag ticks
// so a buddy that was just replaced ignores the old buddy's pending
// timer messages. The counter is process-global; callers do not share
// id space across processes so monotonicity inside a run is enough.
func nextSession() int64 {
	nextID++
	return nextID
}

// New constructs a Model in StateAppearing (or StateSettled when
// TypingMsPerChar == 0) and returns the initial command batch that
// drives the typing + frame ticks (and the timeout tick when the
// dismiss mode is timeout AND the buddy starts settled).
func New(opts Options) (Model, tea.Cmd) {
	id := nextSession()
	m := Model{
		buddy:           opts.Buddy,
		theme:           opts.Theme,
		text:            opts.Text,
		anim:            opts.Animation,
		position:        opts.Position,
		dismiss:         opts.Dismiss,
		typingMsPerChar: opts.TypingMsPerChar,
		frameIntervalMs: opts.FrameIntervalMs,
		bubble:          viewport.New(),
		id:              id,
	}
	if m.typingMsPerChar <= 0 {
		m.cursor = utf8.RuneCountInString(m.text)
		m.state = StateSettled
	}
	cmds := []tea.Cmd{m.frameCmd()}
	if m.state == StateAppearing {
		cmds = append(cmds, m.typingCmd())
	} else if m.dismiss.Mode == DismissModeTimeout {
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
func (m Model) Position() Position { return m.position }

// Theme replaces the theme used at the next View() call. Allows the
// runtime theme switcher to repaint without rebuilding the buddy.
func (m Model) Theme(theme config.Theme) Model {
	m.theme = theme
	return m
}

// Update consumes a tea.Msg and returns the next model plus any
// commands. The parent dispatches every msg here while a buddy is
// alive; key messages are consumed (not forwarded to the app under
// the buddy) so dismiss + scroll have priority.
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
		return m.dismiss_(), nil
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

func (m Model) dismiss_() Model {
	m.dismissed = true
	return m
}

func (m Model) handleKey(key tea.KeyMsg) (Model, tea.Cmd) {
	keyStr := key.String()
	if isScrollKey(keyStr) {
		bubble, _ := m.bubble.Update(key, m.bubbleViewport())
		m.bubble = bubble
		return m, nil
	}
	if m.state == StateSettled && m.dismiss.Mode == DismissModeKey {
		for _, k := range m.dismiss.Keys {
			if k == keyStr {
				return m.Dismiss()
			}
		}
	}
	// Consume the key — don't forward to the app underneath.
	return m, nil
}

func (m Model) advanceTyping() (Model, tea.Cmd) {
	total := utf8.RuneCountInString(m.text)
	if m.cursor < total {
		m.cursor++
	}
	if m.cursor >= total {
		m.state = StateSettled
		var timeoutCmd tea.Cmd
		if m.dismiss.Mode == DismissModeTimeout {
			timeoutCmd = m.timeoutCmd()
		}
		// Stop scheduling further typing ticks once Settled.
		return m, timeoutCmd
	}
	return m, m.typingCmd()
}

func (m Model) advanceFrame() (Model, tea.Cmd) {
	frames := m.buddy.Animations[m.anim]
	if len(frames) > 0 {
		m.frame = (m.frame + 1) % len(frames)
	}
	return m, m.frameCmd()
}

func (m Model) typingCmd() tea.Cmd {
	if m.typingMsPerChar <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.typingMsPerChar) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return typingTickMsg{id: id} })
}

func (m Model) frameCmd() tea.Cmd {
	if m.frameIntervalMs <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.frameIntervalMs) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return frameTickMsg{id: id} })
}

func (m Model) timeoutCmd() tea.Cmd {
	if m.dismiss.AfterMs <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.dismiss.AfterMs) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return timeoutTickMsg{id: id} })
}

func isScrollKey(s string) bool {
	switch s {
	case "j", "k", "up", "down", "pgup", "pgdown", "ctrl+u", "ctrl+d", "g", "G", "home", "end":
		return true
	}
	return false
}

// View renders the bordered card the parent can pass to Overlay. Cards
// always honour Buddy.Size.Width/Height; lipgloss adds 1 cell on each
// side for a visible border. Colors resolve against the live theme on
// every call so a runtime theme switch is visible at the next frame.
func (m Model) View() string {
	if m.dismissed {
		return ""
	}
	width := m.buddy.Size.Width
	height := m.buddy.Size.Height

	bubbleHeight := height / 2
	if bubbleHeight < 1 {
		bubbleHeight = 1
	}
	frameHeight := height - bubbleHeight
	if frameHeight < 1 {
		frameHeight = 1
		bubbleHeight = height - 1
		if bubbleHeight < 1 {
			bubbleHeight = 1
		}
	}

	bubble := m.renderBubble(width, bubbleHeight)
	tail := m.renderTail(width)
	frame := m.renderFrame(width, frameHeight-strings.Count(tail, "\n"))
	body := joinNonEmpty(bubble, tail, frame)

	style := m.cardStyle(width, height)
	return style.Render(body)
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
	if m.cursor >= utf8.RuneCountInString(m.text) {
		return m.text
	}
	runes := []rune(m.text)
	return string(runes[:m.cursor])
}

func (m Model) renderBubble(width, height int) string {
	text := m.typed()
	wrapped := softWrap(text, width-2)
	lines := strings.Split(wrapped, "\n")
	visible, _, _ := viewport.Slice(lines, m.bubble.Scroll, height)
	for len(visible) < height {
		visible = append(visible, "")
	}
	for i, line := range visible {
		visible[i] = padRight(line, width-2)
	}
	if len(visible) > 0 {
		visible[0] = "“ " + visible[0]
		visible[len(visible)-1] = visible[len(visible)-1] + " ”"
	}
	return strings.Join(visible, "\n")
}

func (m Model) bubbleViewport() int {
	height := m.buddy.Size.Height
	bubbleHeight := height / 2
	if bubbleHeight < 1 {
		bubbleHeight = 1
	}
	return bubbleHeight
}

func (m Model) renderTail(width int) string {
	switch m.buddy.Bubble.TailSide {
	case config.BuddyTailBottom:
		return centerLine("\\/", width)
	case config.BuddyTailTop:
		return centerLine("/\\", width)
	case config.BuddyTailLeft:
		return "<"
	case config.BuddyTailRight:
		return padRight("", width-1) + ">"
	}
	return ""
}

func (m Model) renderFrame(width, height int) string {
	frames := m.buddy.Animations[m.anim]
	var frameValue string
	if len(frames) > 0 {
		idx := m.frame % len(frames)
		frameValue = frames[idx].Value
	}
	lines := strings.Split(strings.TrimRight(frameValue, "\n"), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = padRight(line, width)
	}
	return strings.Join(lines, "\n")
}

func (m Model) cardStyle(width, height int) lipgloss.Style {
	style := lipgloss.NewStyle().Width(width).Height(height)
	if !m.buddy.Border.Visible || m.buddy.Style == config.BuddyStyleHidden {
		return style
	}

	border := buddyBorder(m.buddy)
	style = style.Border(border)
	if c := m.buddy.Border.Color; c != "" {
		if rc, err := config.ResolveColor(c, m.theme); err == nil && !rc.IsTransparent() {
			style = style.BorderForeground(rc.TerminalColor())
		}
	}
	if c := m.buddy.Border.Background; c != "" {
		if rc, err := config.ResolveColor(c, m.theme); err == nil && !rc.IsTransparent() {
			style = style.BorderBackground(rc.TerminalColor())
		}
	}
	if bg := m.buddy.Background; bg != "" {
		if rc, err := config.ResolveColor(bg, m.theme); err == nil && !rc.IsTransparent() {
			style = style.Background(rc.TerminalColor())
		}
	}
	return style
}

func buddyBorder(b config.Buddy) lipgloss.Border {
	switch b.Style {
	case config.BuddyStyleSquare:
		return lipgloss.NormalBorder()
	case config.BuddyStyleDouble:
		return lipgloss.DoubleBorder()
	case config.BuddyStyleThick:
		return lipgloss.ThickBorder()
	case config.BuddyStyleHidden:
		return lipgloss.HiddenBorder()
	case config.BuddyStyleCustom:
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
