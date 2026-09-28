package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/overlay"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
)

// notificationState enumerates the notification's lifecycle. Appearing is
// the typing-in phase; Settled is the post-typing phase that accepts
// dismiss input and starts the timeout clock when applicable.
type notificationState int

const (
	notificationStateAppearing notificationState = iota
	notificationStateSettled
)

// notificationOptions is the input to newNotification. Notification
// carries every render+behaviour knob (size, border, animation,
// position, dismiss, typing speed); Text is the resolved bubble copy.
// Theme drives the colour resolver at every View() so a runtime theme
// switch repaints the card. Catalog resolves `${{intl:KEY}}` tokens
// declared in Notification.Actions[].Label and the dismiss-footer
// labels — composition root passes the active TUI catalog so
// notification YAMLs carry catalog keys rather than hardcoded strings.
type notificationOptions struct {
	Notification config.Notification
	Theme        config.Theme
	Text         string
	DetailText   string
	Catalog      *config.Catalog
}

// notificationModel owns the running notification. The parent typically
// holds an optional pointer (nil = no notification active). On every
// tea.Msg route messages through Update; emit on screen via View() and
// place via overlay.Overlay at Position().
type notificationModel struct {
	cfg        config.Notification
	theme      config.Theme
	state      notificationState
	text       string
	detailText string
	page       int
	cursor     int // rune cursor into text
	frame      int
	bubble     list.Viewport
	id         int64 // tick generation; replaced notifications get a new id
	dismissed  bool
	catalog    *config.Catalog
}

// DismissedMsg is sent when the notification should be removed by the parent.
// It carries the source notification id so a parent can match against the
// current notification and ignore stale dismiss messages from an older
// notification that was already replaced.
type DismissedMsg struct{ ID int64 }

// ActionMsg is sent when the user presses a key bound to one of the
// notification's declared Actions. The Slug names the source notification so
// the parent can route the action without inspecting Model state; ActionID
// is the stable identifier authored in YAML (used by the audit log); Command
// is the cobra args slice the parent should invoke in-process. An empty
// Command means the action is a labeled dismiss (no side effect).
//
// Emitting ActionMsg also dismisses the notification — the contract is "the
// user chose one option", not "the user chose AND then closes manually".
type ActionMsg struct {
	ID       int64
	Slug     string
	ActionID string
	Command  []string
}

type typingTickMsg struct{ id int64 }
type frameTickMsg struct{ id int64 }
type timeoutTickMsg struct{ id int64 }

var notificationNextID int64

// nextNotificationSession returns a monotonically increasing id used to
// tag ticks so a notification that was just replaced ignores the old
// notification's pending timer messages. The counter is process-global
// on the host; callers do not share id space across processes so
// monotonicity inside a run is enough.
func nextNotificationSession() int64 {
	notificationNextID++
	return notificationNextID
}

// newNotification constructs a Model in notificationStateAppearing (or
// Settled when TypingMsPerChar == 0) and returns the initial command
// batch that drives the typing + frame ticks (and the timeout tick
// when the dismiss mode is timeout AND the notification starts settled).
func newNotification(opts notificationOptions) (notificationModel, tea.Cmd) {
	id := nextNotificationSession()
	m := notificationModel{
		cfg:        opts.Notification,
		theme:      opts.Theme,
		text:       screenkit.SanitizeMultiline(opts.Text),
		detailText: screenkit.SanitizeMultiline(opts.DetailText),
		bubble:     list.NewViewport(),
		id:         id,
		catalog:    opts.Catalog,
	}
	if *m.cfg.TypingMsPerChar <= 0 {
		m.cursor = utf8.RuneCountInString(m.currentText())
		m.state = notificationStateSettled
	}
	cmds := []tea.Cmd{m.frameCmd()}
	if m.state == notificationStateAppearing {
		cmds = append(cmds, m.typingCmd())
	} else if m.cfg.Dismiss.Mode == config.NotificationDismissModeTimeout {
		cmds = append(cmds, m.timeoutCmd())
	}
	return m, tea.Batch(cmds...)
}

// resolve expands `${{intl:KEY}}` tokens against the configured catalog.
// nil catalog returns the input unchanged so tests can pass literal
// labels without wiring the bundle.
func (m notificationModel) resolve(s string) string {
	if m.catalog == nil {
		return s
	}
	return m.catalog.Resolve(s)
}

// ID returns the session id assigned at New; useful for parents that
// want to compare against an incoming DismissedMsg.
func (m notificationModel) ID() int64 { return m.id }

// State returns the current lifecycle state. Tests use this to assert
// transitions without poking at private fields.
func (m notificationModel) State() notificationState { return m.state }

// Position returns the configured anchor; the parent uses this when
// calling overlay.Overlay.
func (m notificationModel) Position() overlay.Position {
	return overlay.Position(m.cfg.Position)
}

// Theme replaces the theme used at the next View() call. Allows the
// runtime theme switcher to repaint without rebuilding the notification.
func (m notificationModel) Theme(theme config.Theme) notificationModel {
	m.theme = theme
	return m
}

// Update consumes a tea.Msg and returns the next model plus any
// commands. The parent dispatches every msg here while a notification is
// alive; key messages are consumed (not forwarded to the app under
// the notification) so dismiss + scroll have priority.
func (m notificationModel) Update(msg tea.Msg) (notificationModel, tea.Cmd) {
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
func (m notificationModel) Dismiss() (notificationModel, tea.Cmd) {
	if m.dismissed {
		return m, nil
	}
	id := m.id
	m.dismissed = true
	return m, func() tea.Msg { return DismissedMsg{ID: id} }
}

func (m notificationModel) handleKey(key tea.KeyMsg) (notificationModel, tea.Cmd) {
	keyStr := key.String()
	if keyStr == "tab" && m.hasDetailText() {
		m.page = 1 - m.page
		m.cursor = utf8.RuneCountInString(m.currentText())
		m.state = notificationStateSettled
		m.bubble.Scroll = 0
		return m, nil
	}
	if isNotificationScrollKey(keyStr) {
		bubble, _ := m.bubble.Update(key, m.bubbleViewport())
		m.bubble = bubble
		return m, nil
	}
	if m.state == notificationStateSettled {
		return m.handleSettledKey(keyStr)
	}
	// Consume the key — don't forward to the app underneath.
	return m, nil
}

func (m notificationModel) handleSettledKey(key string) (notificationModel, tea.Cmd) {
	// Actions take priority over dismiss keys: when a user-declared button
	// shares a key with the dismiss list, validation has already rejected the
	// ambiguous configuration.
	for _, action := range m.cfg.Actions {
		if action.Key == key {
			return m.fireAction(action)
		}
	}
	if m.dismissKeysEnabled() {
		for _, dismissKey := range m.cfg.Dismiss.Keys {
			if dismissKey == key {
				return m.Dismiss()
			}
		}
	}
	return m, nil
}

func (m notificationModel) fireAction(action config.NotificationAction) (notificationModel, tea.Cmd) {
	if m.dismissed {
		return m, nil
	}
	id := m.id
	slug := m.cfg.Name
	m.dismissed = true
	return m, func() tea.Msg {
		return ActionMsg{ID: id, Slug: slug, ActionID: action.ID, Command: action.Command}
	}
}

func (m notificationModel) dismissKeysEnabled() bool {
	if len(m.cfg.Dismiss.Keys) == 0 {
		return false
	}
	return m.cfg.Dismiss.Mode == config.NotificationDismissModeKey || m.cfg.Dismiss.Mode == config.NotificationDismissModeTimeout
}

func (m notificationModel) advanceTyping() (notificationModel, tea.Cmd) {
	total := utf8.RuneCountInString(m.currentText())
	if m.cursor < total {
		m.cursor++
	}
	if m.cursor >= total {
		m.state = notificationStateSettled
		var timeoutCmd tea.Cmd
		if m.cfg.Dismiss.Mode == config.NotificationDismissModeTimeout {
			timeoutCmd = m.timeoutCmd()
		}
		// Stop scheduling further typing ticks once Settled.
		return m, timeoutCmd
	}
	return m, m.typingCmd()
}

func (m notificationModel) advanceFrame() (notificationModel, tea.Cmd) {
	frames := m.cfg.Animation
	if len(frames) > 0 {
		m.frame = (m.frame + 1) % len(frames)
	}
	return m, m.frameCmd()
}

func (m notificationModel) typingCmd() tea.Cmd {
	if *m.cfg.TypingMsPerChar <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(*m.cfg.TypingMsPerChar) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return typingTickMsg{id: id} })
}

func (m notificationModel) frameCmd() tea.Cmd {
	if m.cfg.FrameIntervalMs <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.cfg.FrameIntervalMs) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return frameTickMsg{id: id} })
}

func (m notificationModel) timeoutCmd() tea.Cmd {
	if m.cfg.Dismiss.AfterMs <= 0 {
		return nil
	}
	id := m.id
	delay := time.Duration(m.cfg.Dismiss.AfterMs) * time.Millisecond
	return tea.Tick(delay, func(time.Time) tea.Msg { return timeoutTickMsg{id: id} })
}

func isNotificationScrollKey(s string) bool {
	switch s {
	case "j", "k", "up", "down", "pgup", "pgdown", "ctrl+u", "ctrl+d", "g", "G", "home", "end":
		return true
	}
	return false
}

// View maps config+state onto overlay card props and returns the painted
// chrome. Bytes must stay identical to the former component View.
func (m notificationModel) View() string {
	if m.dismissed {
		return ""
	}
	return overlay.RenderCard(m.card())
}

func (m notificationModel) card() overlay.Card {
	n := m.cfg
	frameWidth := n.Size.Width
	if n.Border.Visible != nil && *n.Border.Visible {
		frameWidth = n.Size.Width - 2
	}
	if frameWidth < 1 {
		frameWidth = 1
	}
	frames := make([]string, len(n.Animation))
	for i, f := range n.Animation {
		frames[i] = screenkit.SanitizeMultiline(f.Value)
	}
	customBorder := lipgloss.Border{
		Top:         screenkit.Sanitize(n.CustomBorder.Top),
		Bottom:      screenkit.Sanitize(n.CustomBorder.Bottom),
		Left:        screenkit.Sanitize(n.CustomBorder.Left),
		Right:       screenkit.Sanitize(n.CustomBorder.Right),
		TopLeft:     screenkit.Sanitize(n.CustomBorder.TopLeft),
		TopRight:    screenkit.Sanitize(n.CustomBorder.TopRight),
		BottomLeft:  screenkit.Sanitize(n.CustomBorder.BottomLeft),
		BottomRight: screenkit.Sanitize(n.CustomBorder.BottomRight),
	}
	return overlay.Card{
		Width:            n.Size.Width,
		Height:           n.Size.Height,
		AutoHeight:       n.AutoHeight != nil && *n.AutoHeight,
		BorderVisible:    n.Border.Visible != nil && *n.Border.Visible,
		BorderForeground: m.resolvedColor(n.Border.Color),
		BorderBackground: m.resolvedColor(n.Border.Background),
		Background:       m.resolvedColor(n.Background),
		Padding:          notificationPadding(n.Padding),
		PaddingInside:    n.PaddingInside != nil && *n.PaddingInside,
		Style:            n.Style,
		CustomBorder:     customBorder,
		TailSide:         n.Bubble.TailSide,
		Frames:           frames,
		Frame:            m.frame,
		Text:             m.typed(),
		Scroll:           m.bubble.Scroll,
		FormatScrollHint: m.formatScrollHint,
		Footer:           m.renderFooter(frameWidth),
	}
}

func notificationPadding(p *config.NotificationPadding) overlay.Padding {
	if p == nil {
		return overlay.Padding{}
	}
	out := overlay.Padding{}
	if p.Top != nil {
		out.Top = *p.Top
	}
	if p.Right != nil {
		out.Right = *p.Right
	}
	if p.Bottom != nil {
		out.Bottom = *p.Bottom
	}
	if p.Left != nil {
		out.Left = *p.Left
	}
	return out
}

func (m notificationModel) resolvedColor(value string) lipgloss.Color {
	if value == "" {
		return ""
	}
	rc, err := config.ResolveColor(value, m.theme)
	if err != nil || rc.IsTransparent() {
		return ""
	}
	return rc.Color
}

func (m notificationModel) formatScrollHint(above, below int) string {
	if above <= 0 && below <= 0 {
		return ""
	}
	resolve := func(key string) string {
		if m.catalog == nil {
			return key
		}
		return m.catalog.Get(key)
	}
	parts := make([]string, 0, 2)
	if above > 0 {
		parts = append(parts, tr(resolve, "tui.scroll.above_fmt", "▲ %d above", above))
	}
	if below > 0 {
		parts = append(parts, tr(resolve, "tui.scroll.below_fmt", "▼ %d below", below))
	}
	return strings.Join(parts, " · ")
}

func tr(text func(string) string, key, fallback string, args ...any) string {
	tmpl := fallback
	if text != nil {
		if got := text(key); got != "" && got != key {
			tmpl = got
		}
	}
	if len(args) == 0 {
		return tmpl
	}
	return fmt.Sprintf(tmpl, args...)
}

// renderFooter paints the same compact keybinding treatment used by the
// main TUI footer. It advertises tab paging when detail text is present and
// key-based dismiss controls when the notification uses dismiss.mode=key.
func (m notificationModel) renderFooter(innerWidth int) string {
	if m.cfg.FooterVisible == nil || !*m.cfg.FooterVisible {
		return ""
	}
	tokens := make([]tokenstrip.Key, 0, 2+len(m.cfg.Actions))
	if m.hasDetailText() {
		tokens = append(tokens, tokenstrip.Key{Key: "tab", Label: m.resolve("${{intl:notifications.footer.details}}"), Primary: true})
	}
	// Action buttons go first when present so the user's primary choices
	// dominate the footer. The dismiss hint follows so esc still discoverable.
	for i, action := range m.cfg.Actions {
		tokens = append(tokens, tokenstrip.Key{Key: action.Key, Label: m.resolve(action.Label), Primary: i == 0 && !m.hasDetailText()})
	}
	if m.dismissKeysEnabled() {
		if key := footerDismissKey(m.cfg.Dismiss.Keys); key != "" {
			labelKey := "${{intl:notifications.footer.close}}"
			if len(m.cfg.Actions) > 0 {
				labelKey = "${{intl:notifications.footer.cancel}}"
			}
			tokens = append(tokens, tokenstrip.Key{Key: key, Label: m.resolve(labelKey), Primary: !m.hasDetailText() && len(m.cfg.Actions) == 0})
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	styles := tokenstrip.KeyStyles{
		Primary:   lipgloss.NewStyle().Foreground(themeColor(m.theme, "primary", "#39FF14")).Bold(true),
		Secondary: lipgloss.NewStyle().Foreground(themeColor(m.theme, "border", "#494543")),
	}
	styles.Align = notificationFooterPosition(m.cfg.FooterPosition)
	return tokenstrip.KeysWrapped(tokens, styles, innerWidth)
}

func themeColor(theme config.Theme, key, fallback string) lipgloss.Color {
	if value := theme.Colors[key]; value != "" {
		return lipgloss.Color(value)
	}
	return lipgloss.Color(fallback)
}

func notificationFooterPosition(position string) string {
	switch position {
	case config.NotificationFooterLeft:
		return tokenstrip.AlignLeft
	case config.NotificationFooterCenter:
		return tokenstrip.AlignCenter
	case config.NotificationFooterRight:
		return tokenstrip.AlignRight
	}
	panic("invalid notification footer_position: " + position)
}

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

func (m notificationModel) typed() string {
	text := m.currentText()
	if m.cursor >= utf8.RuneCountInString(text) {
		return text
	}
	runes := []rune(text)
	return string(runes[:m.cursor])
}

func (m notificationModel) currentText() string {
	if m.page == 1 && m.hasDetailText() {
		return m.detailText
	}
	return m.text
}

func (m notificationModel) hasDetailText() bool {
	return strings.TrimSpace(m.detailText) != ""
}

func (m notificationModel) bubbleViewport() int {
	height := m.cfg.Size.Height
	bubbleHeight := height / 2
	if bubbleHeight < 1 {
		bubbleHeight = 1
	}
	return bubbleHeight
}
