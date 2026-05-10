package actions

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

func sampleNotificationBundleSnapshot() NotificationBundleSnapshot {
	return NotificationBundleSnapshot{
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
		Border:          config.NotificationBorder{Visible: boolPtr(true), Width: 1, Color: "#ffffff"},
		Animation:       []config.NotificationFrame{{Frame: 0, Value: "X"}},
		Bubble:          config.NotificationBubble{TailSide: config.NotificationTailBottom},
		Padding:         zeroNotificationPadding(),
		AutoHeight:      boolPtr(true),
		PaddingInside:   boolPtr(false),
		FooterVisible:   boolPtr(false),
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: intPtr(0),
		MessageField:    "hint",
	}
}

func TestNotificationShowAction_Name(t *testing.T) {
	a := NewNotificationShowAction(sampleNotificationBundleSnapshot())
	if a.Name() != NotificationActionName {
		t.Fatalf("Name() = %q, want %q", a.Name(), NotificationActionName)
	}
}

func TestNotificationShowAction_NoSenderIsNoop(t *testing.T) {
	a := NewNotificationShowAction(sampleNotificationBundleSnapshot())
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, map[string]any{NotificationArgSlug: "kit"})
	if err != nil {
		t.Fatalf("Execute with nil sender returned error: %v", err)
	}
}

func TestNotificationShowAction_unknownSlugErrors(t *testing.T) {
	a := NewNotificationShowAction(sampleNotificationBundleSnapshot())
	a.SetSender(&recordingSender{msgs: make(chan NotificationShowMsg, 1)})
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, map[string]any{NotificationArgSlug: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected unknown-slug error, got %v", err)
	}
}

func TestNotificationShowAction_missingSlugErrors(t *testing.T) {
	a := NewNotificationShowAction(sampleNotificationBundleSnapshot())
	a.SetSender(&recordingSender{msgs: make(chan NotificationShowMsg, 1)})
	err := a.Execute(context.Background(), domain.Event{Body: "hi"}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), NotificationArgSlug) {
		t.Fatalf("expected missing-slug error, got %v", err)
	}
}

func TestResolveNotificationMessage_payloadFieldWins(t *testing.T) {
	ev := domain.Event{Body: "fallback", Payload: `{"hint": "from-payload"}`}
	got, err := ResolveNotificationMessage(ev, "", "hint", "", "")
	if err != nil {
		t.Fatalf("ResolveNotificationMessage: %v", err)
	}
	if got != "from-payload" {
		t.Fatalf("got %q, want from-payload", got)
	}
}

func TestResolveNotificationMessage_fallsBackToBody(t *testing.T) {
	ev := domain.Event{Body: "fallback", Payload: `{"other": "x"}`}
	got, err := ResolveNotificationMessage(ev, "", "hint", "", "")
	if err != nil {
		t.Fatalf("ResolveNotificationMessage: %v", err)
	}
	if got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}

func TestResolveNotificationMessage_emptyEverythingErrors(t *testing.T) {
	_, err := ResolveNotificationMessage(domain.Event{}, "", "hint", "", "")
	if err == nil {
		t.Fatalf("expected error for empty event")
	}
}

func TestResolveNotificationMessage_notificationLiteralWinsOverPayload(t *testing.T) {
	ev := domain.Event{Body: "body", Payload: `{"hint":"from-payload"}`}
	got, err := ResolveNotificationMessage(ev, "notification-literal", "hint", "", "")
	if err != nil {
		t.Fatalf("ResolveNotificationMessage: %v", err)
	}
	if got != "notification-literal" {
		t.Fatalf("got %q, want notification-literal", got)
	}
}

func TestResolveNotificationMessage_notificationOverridesHook(t *testing.T) {
	got, err := ResolveNotificationMessage(domain.Event{}, "from-notification", "", "from-hook", "")
	if err != nil {
		t.Fatalf("ResolveNotificationMessage: %v", err)
	}
	if got != "from-notification" {
		t.Fatalf("got %q, want from-notification", got)
	}
}

func TestResolveNotificationMessage_hookFallbackWhenNotificationEmpty(t *testing.T) {
	got, err := ResolveNotificationMessage(domain.Event{}, "", "", "from-hook", "")
	if err != nil {
		t.Fatalf("ResolveNotificationMessage: %v", err)
	}
	if got != "from-hook" {
		t.Fatalf("got %q, want from-hook", got)
	}
}

func TestResolveNotificationMessage_hookFieldFallback(t *testing.T) {
	ev := domain.Event{Payload: `{"reason":"timeout"}`}
	got, err := ResolveNotificationMessage(ev, "", "", "", "reason")
	if err != nil {
		t.Fatalf("ResolveNotificationMessage: %v", err)
	}
	if got != "timeout" {
		t.Fatalf("got %q, want timeout", got)
	}
}

func TestResolveOptionalNotificationMessage_payloadField(t *testing.T) {
	ev := domain.Event{Payload: `{"hint":"full guard hint"}`}
	got := ResolveOptionalNotificationMessage(ev, "", "hint")
	if got != "full guard hint" {
		t.Fatalf("got %q, want full guard hint", got)
	}
}

func TestNotificationShowAction_resolvesDetailField(t *testing.T) {
	a := NewNotificationShowAction(sampleNotificationBundleSnapshot())
	sender := &recordingSender{msgs: make(chan NotificationShowMsg, 1)}
	a.SetSender(sender)
	err := a.Execute(context.Background(), domain.Event{Payload: `{"hint":"short", "full":"complete policy hint"}`}, map[string]any{
		NotificationArgSlug:               "kit",
		NotificationArgDetailMessageField: "full",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	select {
	case show := <-sender.msgs:
		if show.Text != "short" {
			t.Fatalf("Text = %q, want short", show.Text)
		}
		if show.DetailText != "complete policy hint" {
			t.Fatalf("DetailText = %q, want complete policy hint", show.DetailText)
		}
	default:
		t.Fatal("Execute did not send NotificationShowMsg")
	}
}

type recordingSender struct {
	msgs chan NotificationShowMsg
}

func (r *recordingSender) SendNotification(msg NotificationShowMsg) {
	select {
	case r.msgs <- msg:
	default:
	}
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func zeroNotificationPadding() *config.NotificationPadding {
	return &config.NotificationPadding{Top: intPtr(0), Right: intPtr(0), Bottom: intPtr(0), Left: intPtr(0)}
}
