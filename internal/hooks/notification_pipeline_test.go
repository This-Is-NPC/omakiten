package hooks_test

import (
	"context"
	"testing"
	"time"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/events"
	"omakiten/internal/hooks"
	"omakiten/internal/hooks/actions"
)

type pipelineSender struct {
	msgs chan actions.NotificationShowMsg
}

func (r *pipelineSender) SendNotification(msg actions.NotificationShowMsg) {
	select {
	case r.msgs <- msg:
	default:
	}
}

type pipelineRecorder struct{}

func (pipelineRecorder) RecordEntityEvent(_ context.Context, _ string, _, _ int64, _, _ string) error {
	return nil
}

func pipelineNotification() config.Notification {
	t := true
	f := false
	zero := 0
	return config.Notification{
		Name:            "guard-violation",
		Size:            config.NotificationSize{Width: 20, Height: 6},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.NotificationStyleRounded,
		Border:          config.NotificationBorder{Visible: &t, Width: 1, Color: "#ffffff"},
		Animation:       []config.NotificationFrame{{Frame: 0, Value: "X"}},
		Bubble:          config.NotificationBubble{TailSide: config.NotificationTailBottom},
		Padding:         &config.NotificationPadding{Top: &zero, Right: &zero, Bottom: &zero, Left: &zero},
		AutoHeight:      &t,
		PaddingInside:   &f,
		FooterVisible:   &f,
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: &zero,
		MessageField:    "hint",
	}
}

func openEventSettings() config.EventsSettings {
	t := true
	return config.EventsSettings{
		Defaults: config.EventChannelSettings{Log: &t, Broadcast: &t, Hook: &t},
	}
}

// TestHooksToNotificationShow exercises the full event→notification pipeline:
//
//	bus.Publish(guard.violated)
//	  → Engine matches on the notification hook (rewritten to notification.show)
//	  → NotificationShowAction.Execute looks up the notification by slug, resolves message
//	  → MessageSender (recorder) captures the dispatched ShowMsg
func TestHooksToNotificationShow(t *testing.T) {
	bud := pipelineNotification()
	settings := openEventSettings()
	sender := &pipelineSender{msgs: make(chan actions.NotificationShowMsg, 4)}
	notificationAction := actions.NewNotificationShowAction(actions.NotificationBundleSnapshot{
		Notifications: map[string]config.Notification{bud.Name: bud},
	})
	notificationAction.SetSender(sender)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	registry.Register(notificationAction)

	hookSpec := hooks.Hook{
		On:   domain.EventTypeGuardViolated,
		Do:   actions.NotificationActionName,
		Args: map[string]any{actions.NotificationArgSlug: bud.Name},
	}
	engine := hooks.NewEngine([]hooks.Hook{hookSpec}, registry, settings, pipelineRecorder{})
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
		if msg.Notification.Name != bud.Name {
			t.Fatalf("notification.Name = %q, want %q", msg.Notification.Name, bud.Name)
		}
		if msg.Text != "policy: blocked" {
			t.Fatalf("text = %q, want hint payload", msg.Text)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("no ShowMsg received within timeout")
	}
}

func TestHooksToNotificationShow_skipsWhenWhenFilterMisses(t *testing.T) {
	bud := pipelineNotification()
	settings := openEventSettings()
	sender := &pipelineSender{msgs: make(chan actions.NotificationShowMsg, 4)}
	notificationAction := actions.NewNotificationShowAction(actions.NotificationBundleSnapshot{
		Notifications: map[string]config.Notification{bud.Name: bud},
	})
	notificationAction.SetSender(sender)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	registry.Register(notificationAction)

	hookSpec := hooks.Hook{
		On:   domain.EventTypeGuardViolated,
		When: map[string]string{"operation": "task.delete"},
		Do:   actions.NotificationActionName,
		Args: map[string]any{actions.NotificationArgSlug: bud.Name},
	}
	engine := hooks.NewEngine([]hooks.Hook{hookSpec}, registry, settings, pipelineRecorder{})
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

func TestHooksToNotificationShow_fallsBackToBody(t *testing.T) {
	bud := pipelineNotification()
	bud.Name = "agent-comment"
	bud.MessageField = "missing_key"
	settings := openEventSettings()
	sender := &pipelineSender{msgs: make(chan actions.NotificationShowMsg, 4)}
	notificationAction := actions.NewNotificationShowAction(actions.NotificationBundleSnapshot{
		Notifications: map[string]config.Notification{bud.Name: bud},
	})
	notificationAction.SetSender(sender)

	registry := hooks.NewActionRegistry()
	actions.RegisterBuiltins(registry)
	registry.Register(notificationAction)

	hookSpec := hooks.Hook{
		On:   domain.EventTypeComment,
		Do:   actions.NotificationActionName,
		Args: map[string]any{actions.NotificationArgSlug: bud.Name},
	}
	engine := hooks.NewEngine([]hooks.Hook{hookSpec}, registry, settings, pipelineRecorder{})
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
		if msg.Text != "fallback comment body" {
			t.Errorf("text = %q, want fallback to body", msg.Text)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("no ShowMsg received — body fallback not exercised")
	}
}
