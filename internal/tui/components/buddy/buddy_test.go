package buddy

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
)

func sampleBuddy() config.Buddy {
	return config.Buddy{
		Name:            "kit",
		Size:            config.BuddySize{Width: 20, Height: 6},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.BuddyStyleRounded,
		Border:          config.BuddyBorder{Visible: true, Width: 1, Color: "#ffffff"},
		Animations: map[string][]config.BuddyFrame{
			"idle": {
				{Frame: 0, Value: "A"},
				{Frame: 1, Value: "B"},
			},
		},
		Bubble: config.BuddyBubble{TailSide: config.BuddyTailBottom},
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
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "hello",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		FrameIntervalMs: 50,
	})
	if m.State() != StateSettled {
		t.Fatalf("expected immediate Settled, got %v", m.State())
	}
	if m.cursor != len([]rune("hello")) {
		t.Fatalf("expected cursor at end on instant typing, got %d", m.cursor)
	}
}

func TestUpdate_typingTickAdvancesAndSettles(t *testing.T) {
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "hi",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 10,
		FrameIntervalMs: 100,
	})
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
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "x",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		FrameIntervalMs: 50,
	})
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
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "hello world",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 100,
		FrameIntervalMs: 100,
	})
	m, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(DismissedMsg); ok {
				t.Fatalf("dismiss key during Appearing should not emit DismissedMsg")
			}
		}
	}
	if m.dismissed {
		t.Fatalf("buddy went dismissed during Appearing")
	}
}

func TestUpdate_settledDismissKeyEmitsMsg(t *testing.T) {
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "hi",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		FrameIntervalMs: 100,
	})
	m, cmd := m.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	if cmd == nil {
		t.Fatalf("expected DismissedMsg cmd")
	}
	msg := cmd()
	dm, ok := msg.(DismissedMsg)
	if !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
	if dm.ID != m.id {
		t.Fatalf("dismiss msg id %d != model id %d", dm.ID, m.id)
	}
}

func TestUpdate_timeoutDismiss(t *testing.T) {
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "x",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeTimeout, AfterMs: 5},
		TypingMsPerChar: 0,
		FrameIntervalMs: 100,
	})
	if m.State() != StateSettled {
		t.Fatalf("expected Settled before timeout, got %v", m.State())
	}
	m, _ = m.Update(timeoutTickMsg{id: m.id})
	if !m.dismissed {
		t.Fatalf("timeout tick should mark dismissed")
	}
}

func TestUpdate_scrollKeysHitViewportInBothStates(t *testing.T) {
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "hi there friend, this should wrap",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 100,
		FrameIntervalMs: 100,
	})
	jKey := tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = m.Update(jKey)
	if m.bubble.Scroll == 0 {
		t.Fatalf("scroll key in Appearing should advance viewport")
	}
	m.state = StateSettled
	prev := m.bubble.Scroll
	m, _ = m.Update(jKey)
	if m.bubble.Scroll <= prev {
		t.Fatalf("scroll key in Settled should advance viewport (was %d, now %d)", prev, m.bubble.Scroll)
	}
}

func TestThemeSwap_recomputesColorOnNextResolve(t *testing.T) {
	buddy := sampleBuddy()
	buddy.Border.Color = "$theme.primary"
	t1 := config.Theme{Version: 1, Key: "a", Name: "A", Colors: map[string]string{"primary": "#ff0000"}}
	t2 := config.Theme{Version: 1, Key: "b", Name: "B", Colors: map[string]string{"primary": "#00ff00"}}
	m, _ := New(Options{
		Buddy:           buddy,
		Theme:           t1,
		Animation:       "idle",
		Text:            "x",
		Position:        PositionTopLeft,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		FrameIntervalMs: 100,
	})
	rc1, err := config.ResolveColor(buddy.Border.Color, m.theme)
	if err != nil {
		t.Fatalf("resolve t1: %v", err)
	}
	m = m.Theme(t2)
	rc2, err := config.ResolveColor(buddy.Border.Color, m.theme)
	if err != nil {
		t.Fatalf("resolve t2: %v", err)
	}
	if rc1.Color == rc2.Color {
		t.Fatalf("theme swap did not change color resolution: both = %q", rc1.Color)
	}
}

func TestPosition_returnsConfigured(t *testing.T) {
	m, _ := New(Options{
		Buddy:           sampleBuddy(),
		Theme:           sampleTheme(),
		Animation:       "idle",
		Text:            "x",
		Position:        PositionBottomRight,
		Dismiss:         DismissConfig{Mode: DismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		FrameIntervalMs: 100,
	})
	if m.Position() != PositionBottomRight {
		t.Fatalf("expected bottom-right, got %s", m.Position())
	}
}
