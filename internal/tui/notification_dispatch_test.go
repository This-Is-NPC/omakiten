package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/token"
	"omakiten/internal/tui/components/notification"
)

func newNotificationTestModel(t *testing.T) Model {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, t.TempDir()+"/omakiten.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	bud := config.Notification{
		Name:            "guard-violation",
		Size:            config.NotificationSize{Width: 16, Height: 6},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.NotificationStyleRounded,
		Border:          config.NotificationBorder{Visible: true, Width: 1, Color: "#ffffff"},
		Animation:       []config.NotificationFrame{{Frame: 0, Value: "X"}},
		Bubble:          config.NotificationBubble{TailSide: config.NotificationTailBottom},
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: 0,
		MessageField:    "hint",
	}
	binding := NotificationBinding{Notifications: map[string]config.Notification{bud.Name: bud}}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Workflow:     app.NewWorkflowServiceFromStore(store),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Config:       store,
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, binding)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	model.width = 80
	model.height = 24
	return model
}

// TestModelDispatchesShowMsg confirms the parent Model wiring picks up
// a notification.ShowMsg, sets the slot, and renders the notification.
func TestModelDispatchesShowMsg(t *testing.T) {
	model := newNotificationTestModel(t)
	bud := model.notifications["guard-violation"]
	msg := notification.ShowMsg{Notification: bud, Text: "policy violation"}

	next, _ := model.Update(msg)
	mn := next.(Model)
	if mn.notification == nil {
		t.Fatalf("ShowMsg did not populate m.notification")
	}
	view := stripANSI(mn.View())
	if !strings.Contains(view, "policy") {
		t.Errorf("rendered view does not contain bubble text: %q", trim(view, 400))
	}
	if !strings.Contains(view, "╭") {
		t.Errorf("rendered view does not show the rounded border: %q", trim(view, 400))
	}
	if mn.notification.Position() != notification.Position(config.NotificationPositionCenter) {
		t.Errorf("position = %s, want center", mn.notification.Position())
	}
}

func TestModelDispatchesShowMsgWithDetailText(t *testing.T) {
	model := newNotificationTestModel(t)
	bud := model.notifications["guard-violation"]
	msg := notification.ShowMsg{Notification: bud, Text: "short warning", DetailText: "full policy details"}

	next, _ := model.Update(msg)
	mn := next.(Model)
	if mn.notification == nil {
		t.Fatalf("ShowMsg did not populate m.notification")
	}
	next, _ = mn.Update(tea.KeyMsg(tea.Key{Type: tea.KeyTab}))
	mn = next.(Model)
	view := stripANSI(mn.notification.View())
	if !strings.Contains(view, "full") {
		t.Fatalf("tab did not render detail text: %q", trim(view, 400))
	}
}

func TestModel_notificationEscapeDismissesViaCmd(t *testing.T) {
	model := newNotificationTestModel(t)
	bud := model.notifications["guard-violation"]
	show := notification.ShowMsg{Notification: bud, Text: "hello"}

	next, _ := model.Update(show)
	mn := next.(Model)
	if mn.notification == nil {
		t.Fatalf("setup: ShowMsg didn't set notification")
	}

	next, cmd := mn.Update(tea.KeyMsg(tea.Key{Type: tea.KeyEsc}))
	mn = next.(Model)
	if cmd == nil {
		t.Fatalf("esc on settled notification must return a cmd")
	}
	msg := cmd()
	dm, ok := msg.(notification.DismissedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want DismissedMsg", msg)
	}

	next, _ = mn.Update(dm)
	mn = next.(Model)
	if mn.notification != nil {
		t.Fatalf("DismissedMsg did not clear notification slot")
	}
}

func TestModelDismissedMsgClearsNotification(t *testing.T) {
	model := newNotificationTestModel(t)
	bud := model.notifications["guard-violation"]
	show := notification.ShowMsg{Notification: bud, Text: "x"}
	next, _ := model.Update(show)
	mn := next.(Model)
	if mn.notification == nil {
		t.Fatalf("setup: notification not set")
	}
	id := mn.notification.ID()

	next, _ = mn.Update(notification.DismissedMsg{ID: id})
	mn = next.(Model)
	if mn.notification != nil {
		t.Fatalf("DismissedMsg did not clear notification")
	}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
