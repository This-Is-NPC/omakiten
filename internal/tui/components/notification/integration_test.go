package notification_test

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
	"omakiten/internal/tui/components/notification"
)

type recordingSender struct {
	msgs chan tea.Msg
}

func (r *recordingSender) Send(msg tea.Msg) {
	select {
	case r.msgs <- msg:
	default:
	}
}

func ptrBool(b bool) *bool { return &b }

func openEventSettings() config.EventsSettings {
	return config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: ptrBool(true), Broadcast: ptrBool(true), Hook: ptrBool(true)},
	}
}

func newTestNotification() config.Notification {
	return config.Notification{
		Name:            "guard-violation",
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

// TestHooksToNotificationShow exercises the full event→notification pipeline:
//
//	bus.Publish(guard.violated)
//	  → hooks.Engine matches on the notification hook (rewritten to notification.show)
//	  → ShowAction.Execute looks up the notification by slug, resolves message
//	  → MessageSender (recorder) captures the dispatched ShowMsg
func TestHooksToNotificationShow(t *testing.T) {
	bud := newTestNotification()
	settings := openEventSettings()
	sender := &recordingSender{msgs: make(chan tea.Msg, 4)}
	notificationAction := notification.NewShowAction(notification.BundleSnapshot{
		Notifications: map[string]config.Notification{bud.Name: bud},
	})
	notificationAction.SetSender(sender)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	registry.Register(notificationAction)

	hookSpec := hooks.Hook{
		On:   domain.EventTypeGuardViolated,
		Do:   notification.ActionName,
		Args: map[string]any{notification.ArgNotificationSlug: bud.Name},
	}
	engine := hooks.NewEngine([]hooks.Hook{hookSpec}, registry, settings, nopRecorder{})
	bus := events.NewInProcessBus(settings)
	engine.Start(bus)
	defer engine.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, domain.Event{
		EventType: domain.EventTypeGuardViolated,
		Payload:   `{"hint":"policy: blocked"}`,
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	select {
	case msg := <-sender.msgs:
		show, ok := msg.(notification.ShowMsg)
		if !ok {
			t.Fatalf("expected ShowMsg, got %T", msg)
		}
		if show.Notification.Name != bud.Name {
			t.Fatalf("notification.Name = %q, want %q", show.Notification.Name, bud.Name)
		}
		if show.Text != "policy: blocked" {
			t.Fatalf("text = %q, want hint payload", show.Text)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("no ShowMsg received within timeout")
	}
}

// TestHooksToNotificationShow_skipsWhenWhenFilterMisses confirms the engine
// drops events whose payload does not match the hook's `when:` clause.
func TestHooksToNotificationShow_skipsWhenWhenFilterMisses(t *testing.T) {
	bud := newTestNotification()
	settings := openEventSettings()
	sender := &recordingSender{msgs: make(chan tea.Msg, 4)}
	notificationAction := notification.NewShowAction(notification.BundleSnapshot{
		Notifications: map[string]config.Notification{bud.Name: bud},
	})
	notificationAction.SetSender(sender)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	registry.Register(notificationAction)

	hookSpec := hooks.Hook{
		On:   domain.EventTypeGuardViolated,
		When: map[string]string{"operation": "task.delete"},
		Do:   notification.ActionName,
		Args: map[string]any{notification.ArgNotificationSlug: bud.Name},
	}
	engine := hooks.NewEngine([]hooks.Hook{hookSpec}, registry, settings, nopRecorder{})
	bus := events.NewInProcessBus(settings)
	engine.Start(bus)
	defer engine.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, domain.Event{
		EventType: domain.EventTypeGuardViolated,
		Payload:   `{"operation":"task.transition","hint":"need self-branch"}`,
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	select {
	case msg := <-sender.msgs:
		t.Fatalf("notification fired despite when filter miss: %T %+v", msg, msg)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestHooksToNotificationShow_fallsBackToBody confirms the message resolver
// uses ev.Body when the notification's message_field is absent from payload.
func TestHooksToNotificationShow_fallsBackToBody(t *testing.T) {
	bud := newTestNotification()
	bud.Name = "agent-comment"
	bud.MessageField = "missing_key"
	settings := openEventSettings()
	sender := &recordingSender{msgs: make(chan tea.Msg, 4)}
	notificationAction := notification.NewShowAction(notification.BundleSnapshot{
		Notifications: map[string]config.Notification{bud.Name: bud},
	})
	notificationAction.SetSender(sender)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	registry.Register(notificationAction)

	hookSpec := hooks.Hook{
		On:   domain.EventTypeComment,
		Do:   notification.ActionName,
		Args: map[string]any{notification.ArgNotificationSlug: bud.Name},
	}
	engine := hooks.NewEngine([]hooks.Hook{hookSpec}, registry, settings, nopRecorder{})
	bus := events.NewInProcessBus(settings)
	engine.Start(bus)
	defer engine.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := bus.Publish(ctx, domain.Event{
		EventType: domain.EventTypeComment,
		Body:      "fallback comment body",
		Payload:   `{}`,
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	select {
	case msg := <-sender.msgs:
		show, ok := msg.(notification.ShowMsg)
		if !ok {
			t.Fatalf("expected ShowMsg, got %T", msg)
		}
		if show.Text != "fallback comment body" {
			t.Errorf("text = %q, want fallback to body", show.Text)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("no ShowMsg received — body fallback not exercised")
	}
}

type nopRecorder struct{}

func (nopRecorder) RecordEntityEvent(_ context.Context, _ string, _, _ int64, _, _ string) error {
	return nil
}
