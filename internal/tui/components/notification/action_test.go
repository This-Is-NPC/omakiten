package notification

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func sampleBundleSnapshotForNotification() BundleSnapshot {
	return BundleSnapshot{
		Notifications: map[string]config.Notification{
			"kit": sampleNotificationConfig(),
		},
	}
}

func sampleNotificationConfig() config.Notification {
	return config.Notification{
		Name:            "kit",
		Size:            config.NotificationSize{Width: 20, Height: 6},
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
}

func TestShowAction_Name(t *testing.T) {
	a := NewShowAction(sampleBundleSnapshotForNotification())
	if a.Name() != ActionName {
		t.Fatalf("Name() = %q, want %q", a.Name(), ActionName)
	}
}

func TestShowAction_NoSenderIsNoop(t *testing.T) {
	a := NewShowAction(sampleBundleSnapshotForNotification())
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, map[string]any{ArgNotificationSlug: "kit"})
	if err != nil {
		t.Fatalf("Execute with nil sender returned error: %v", err)
	}
}

func TestShowAction_unknownSlugErrors(t *testing.T) {
	a := NewShowAction(sampleBundleSnapshotForNotification())
	a.SetSender(&recordingSender{msgs: make(chan tea.Msg, 1)})
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, map[string]any{ArgNotificationSlug: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected unknown-slug error, got %v", err)
	}
}

func TestShowAction_missingSlugErrors(t *testing.T) {
	a := NewShowAction(sampleBundleSnapshotForNotification())
	a.SetSender(&recordingSender{msgs: make(chan tea.Msg, 1)})
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), ArgNotificationSlug) {
		t.Fatalf("expected missing-slug error, got %v", err)
	}
}

func TestResolveMessage_payloadFieldWins(t *testing.T) {
	ev := domain.Event{Body: "fallback", Payload: `{"hint": "from-payload"}`}
	got, err := resolveMessage(ev, "", "hint", "", "")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "from-payload" {
		t.Fatalf("got %q, want from-payload", got)
	}
}

func TestResolveMessage_fallsBackToBody(t *testing.T) {
	ev := domain.Event{Body: "fallback", Payload: `{"other": "x"}`}
	got, err := resolveMessage(ev, "", "hint", "", "")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestResolveMessage_emptyEverythingErrors(t *testing.T) {
	_, err := resolveMessage(domain.Event{}, "", "hint", "", "")
	if err == nil {
		t.Fatalf("expected error for empty event")
	}
}

func TestResolveMessage_notificationLiteralWinsOverPayload(t *testing.T) {
	ev := domain.Event{Body: "body", Payload: `{"hint":"from-payload"}`}
	got, err := resolveMessage(ev, "notification-literal", "hint", "", "")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "notification-literal" {
		t.Fatalf("got %q, want notification-literal", got)
	}
}

// TestResolveMessage_notificationOverridesHook pins the tie-break rule:
// when both layers declare a literal message, the notification YAML wins.
func TestResolveMessage_notificationOverridesHook(t *testing.T) {
	ev := domain.Event{}
	got, err := resolveMessage(ev, "from-notification", "", "from-hook", "")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "from-notification" {
		t.Fatalf("got %q, want from-notification (notification must win on tie-break)", got)
	}
}

// TestResolveMessage_hookFallbackWhenNotificationEmpty verifies that the
// hook layer is consulted only when the notification layer has nothing.
func TestResolveMessage_hookFallbackWhenNotificationEmpty(t *testing.T) {
	ev := domain.Event{}
	got, err := resolveMessage(ev, "", "", "from-hook", "")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "from-hook" {
		t.Fatalf("got %q, want from-hook", got)
	}
}

// TestResolveMessage_hookFieldFallback verifies the hook can supply
// a payload-driven field too.
func TestResolveMessage_hookFieldFallback(t *testing.T) {
	ev := domain.Event{Payload: `{"reason":"timeout"}`}
	got, err := resolveMessage(ev, "", "", "", "reason")
	if err != nil {
		t.Fatalf("resolveMessage: %v", err)
	}
	if got != "timeout" {
		t.Fatalf("got %q, want timeout", got)
	}
}

// recordingSender is a tiny in-memory stand-in for *tea.Program; the
// integration tests use a richer one but the action_test only needs
// to know that Execute attempted a Send.
type recordingSender struct {
	msgs chan tea.Msg
}

func (r *recordingSender) Send(msg tea.Msg) {
	select {
	case r.msgs <- msg:
	default:
	}
}
