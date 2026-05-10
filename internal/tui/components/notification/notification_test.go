package notification

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
)

func sampleNotification() config.Notification {
	return config.Notification{
		Name:            "kit",
		Size:            config.NotificationSize{Width: 20, Height: 6},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.NotificationStyleRounded,
		Border:          config.NotificationBorder{Visible: true, Width: 1, Color: "#ffffff"},
		Animation: []config.NotificationFrame{
			{Frame: 0, Value: "A"},
			{Frame: 1, Value: "B"},
		},
		Bubble:          config.NotificationBubble{TailSide: config.NotificationTailBottom},
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		MessageField:    "hint",
	}
}

func sampleTheme() config.Theme {
	return config.Theme{
		Version: 1,
		Key:     "demo",
		Name:    "Demo",
		Colors:  map[string]string{"primary": "#39ff14", "border": "#494543"},
	}
}

func TestNew_typingZeroSettlesImmediately(t *testing.T) {
	bud := sampleNotification()
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "hello"})
	if m.State() != StateSettled {
		t.Fatalf("expected immediate Settled, got %v", m.State())
	}
}

func TestUpdate_typingTickAdvancesAndSettles(t *testing.T) {
	bud := sampleNotification()
	bud.TypingMsPerChar = 10
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "hi"})
	if m.State() != StateAppearing {
		t.Fatalf("expected Appearing, got %v", m.State())
	}
	tick := typingTickMsg{id: m.id}
	m, _ = m.Update(tick)
	if m.cursor != 1 {
		t.Fatalf("after one tick want cursor=1, got %d", m.cursor)
	}
	m, _ = m.Update(tick)
	if m.State() != StateSettled {
		t.Fatalf("after typing finishes expected Settled, got %v", m.State())
	}
}

func TestUpdate_frameTickLoops(t *testing.T) {
	bud := sampleNotification()
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "x"})
	startFrame := m.frame
	m, _ = m.Update(frameTickMsg{id: m.id})
	if m.frame == startFrame {
		t.Fatalf("frame tick did not advance")
	}
	m, _ = m.Update(frameTickMsg{id: m.id})
	if m.frame != startFrame {
		t.Fatalf("frame should loop back: got %d", m.frame)
	}
}

func TestUpdate_appearingIgnoresDismissKey(t *testing.T) {
	bud := sampleNotification()
	bud.TypingMsPerChar = 100
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "hello world"})
	m, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(DismissedMsg); ok {
				t.Fatalf("dismiss key during Appearing should not emit DismissedMsg")
			}
		}
	}
	if m.dismissed {
		t.Fatalf("notification went dismissed during Appearing")
	}
}

func TestUpdate_settledDismissKeyEmitsMsg(t *testing.T) {
	bud := sampleNotification()
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "hi"})
	_, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	if cmd == nil {
		t.Fatalf("expected DismissedMsg cmd")
	}
	msg := cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
}

func TestUpdate_timeoutDismiss(t *testing.T) {
	bud := sampleNotification()
	bud.Dismiss = config.NotificationDismiss{Mode: config.NotificationDismissModeTimeout, AfterMs: 5}
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "x"})
	if m.State() != StateSettled {
		t.Fatalf("expected Settled, got %v", m.State())
	}
	m, _ = m.Update(timeoutTickMsg{id: m.id})
	if !m.dismissed {
		t.Fatalf("timeout tick should mark dismissed")
	}
}

func TestUpdate_scrollKeysHitViewportInBothStates(t *testing.T) {
	bud := sampleNotification()
	bud.TypingMsPerChar = 100
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "hi there friend, this should wrap"})
	jKey := tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = m.Update(jKey)
	if m.bubble.Scroll == 0 {
		t.Fatalf("scroll key in Appearing should advance viewport")
	}
	m.state = StateSettled
	prev := m.bubble.Scroll
	m, _ = m.Update(jKey)
	if m.bubble.Scroll <= prev {
		t.Fatalf("scroll key in Settled should advance viewport")
	}
}

func TestThemeSwap_recomputesColorOnNextResolve(t *testing.T) {
	bud := sampleNotification()
	bud.Border.Color = "$theme.primary"
	t1 := config.Theme{Version: 1, Key: "a", Name: "A", Colors: map[string]string{"primary": "#ff0000"}}
	t2 := config.Theme{Version: 1, Key: "b", Name: "B", Colors: map[string]string{"primary": "#00ff00"}}
	m, _ := New(Options{Notification: bud, Theme: t1, Text: "x"})
	rc1, err := config.ResolveColor(bud.Border.Color, m.theme)
	if err != nil {
		t.Fatalf("resolve t1: %v", err)
	}
	m = m.Theme(t2)
	rc2, err := config.ResolveColor(bud.Border.Color, m.theme)
	if err != nil {
		t.Fatalf("resolve t2: %v", err)
	}
	if rc1.Color == rc2.Color {
		t.Fatalf("theme swap did not change color resolution: both = %q", rc1.Color)
	}
}

func TestPosition_returnsConfigured(t *testing.T) {
	bud := sampleNotification()
	bud.Position = config.NotificationPositionBottomRight
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "x"})
	if string(m.Position()) != config.NotificationPositionBottomRight {
		t.Fatalf("expected bottom-right, got %s", m.Position())
	}
}

func TestView_noVerticalPadding(t *testing.T) {
	bud := sampleNotification()
	bud.Size = config.NotificationSize{Width: 16, Height: 99}
	bud.Animation = []config.NotificationFrame{{Frame: 0, Value: "X"}}
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "hi"})
	view := m.View()
	rows := strings.Split(view, "\n")
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows (border + 3 body + border), got %d:\n%s", len(rows), view)
	}
}

// Layout tests for the four tail_side branches — the visual contract
// users tune via the notification YAML.
func TestView_layoutFollowsTailSide_bottom(t *testing.T) {
	bud := layoutFixtureNotification(config.NotificationTailBottom)
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "bubble"})
	view := m.View()
	bubbleIdx := strings.Index(view, "bubble")
	frameIdx := strings.Index(view, "FRAMEMARK")
	if bubbleIdx < 0 || frameIdx < 0 {
		t.Fatalf("missing markers:\n%s", view)
	}
	if bubbleIdx >= frameIdx {
		t.Fatalf("expected bubble above frame for tail=bottom; bubble@%d frame@%d", bubbleIdx, frameIdx)
	}
}

func TestView_layoutFollowsTailSide_top(t *testing.T) {
	bud := layoutFixtureNotification(config.NotificationTailTop)
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "bubble"})
	view := m.View()
	bubbleIdx := strings.Index(view, "bubble")
	frameIdx := strings.Index(view, "FRAMEMARK")
	if frameIdx >= bubbleIdx {
		t.Fatalf("expected frame above bubble; frame@%d bubble@%d", frameIdx, bubbleIdx)
	}
}

func TestView_layoutFollowsTailSide_right(t *testing.T) {
	bud := layoutFixtureNotification(config.NotificationTailRight)
	bud.Size.Width = 40
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "bubble"})
	view := m.View()
	for _, row := range strings.Split(view, "\n") {
		if b := strings.Index(row, "bubble"); b >= 0 {
			if f := strings.Index(row, "FRAMEMARK"); f >= 0 && b < f {
				return
			}
		}
	}
	t.Fatalf("expected bubble left of FRAMEMARK in same row:\n%s", view)
}

func TestView_layoutFollowsTailSide_left(t *testing.T) {
	bud := layoutFixtureNotification(config.NotificationTailLeft)
	bud.Size.Width = 40
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "bubble"})
	view := m.View()
	for _, row := range strings.Split(view, "\n") {
		if f := strings.Index(row, "FRAMEMARK"); f >= 0 {
			if b := strings.Index(row, "bubble"); b >= 0 && f < b {
				return
			}
		}
	}
	t.Fatalf("expected FRAMEMARK left of bubble in same row:\n%s", view)
}

func TestRenderFrame_horizontallyCentered(t *testing.T) {
	bud := sampleNotification()
	bud.Size = config.NotificationSize{Width: 22, Height: 4}
	bud.Style = config.NotificationStyleHidden
	bud.Border = config.NotificationBorder{Visible: false}
	bud.Animation = []config.NotificationFrame{{Frame: 0, Value: "o"}}
	m, _ := New(Options{Notification: bud, Theme: sampleTheme(), Text: "x"})
	rendered := m.renderFrame(22)
	leading := len(rendered) - len(strings.TrimLeft(rendered, " "))
	if leading < 10 || leading > 11 {
		t.Fatalf("expected glyph centered ~10–11 cells, got %d (rendered=%q)", leading, rendered)
	}
}

func layoutFixtureNotification(tail string) config.Notification {
	return config.Notification{
		Name:            "kit",
		Size:            config.NotificationSize{Width: 24, Height: 8},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.NotificationStyleHidden,
		Border:          config.NotificationBorder{Visible: false},
		Animation:       []config.NotificationFrame{{Frame: 0, Value: "FRAMEMARK"}},
		Bubble:          config.NotificationBubble{TailSide: tail},
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		MessageField:    "hint",
	}
}
