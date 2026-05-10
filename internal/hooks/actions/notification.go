package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// NotificationActionName is the canonical hook action name for TUI notifications.
// Composition roots rewrite user-facing `notification: <slug>` hook entries into
// this action with the slug carried in NotificationArgSlug.
const NotificationActionName = "notification.show"

// Internal arg keys passed to NotificationShowAction. Users never write these
// directly; composition roots stage them from config.HookSpec fields.
const (
	NotificationArgSlug               = "_notification_slug"
	NotificationArgMessage            = "_notification_message"
	NotificationArgMessageField       = "_notification_message_field"
	NotificationArgDetailMessage      = "_notification_detail_message"
	NotificationArgDetailMessageField = "_notification_detail_message_field"
)

// NotificationShowMsg is the neutral message emitted by NotificationShowAction.
// TUI code adapts it to Bubble Tea by sending this value into the tea.Program;
// CLI/MCP runtimes leave the sender nil so the action is a silent no-op.
type NotificationShowMsg struct {
	Notification config.Notification
	Text         string
	DetailText   string
}

// NotificationSender is the narrow output port for notification delivery.
type NotificationSender interface {
	SendNotification(NotificationShowMsg)
}

// NotificationBundleSnapshot is the loaded notification catalog keyed by slug.
type NotificationBundleSnapshot struct {
	Notifications map[string]config.Notification
}

// NotificationShowAction resolves a configured notification and emits the
// rendered message payload through a sender supplied by the TUI runtime.
type NotificationShowAction struct {
	mu       sync.RWMutex
	sender   NotificationSender
	snapshot NotificationBundleSnapshot
}

func NewNotificationShowAction(snapshot NotificationBundleSnapshot) *NotificationShowAction {
	return &NotificationShowAction{snapshot: snapshot}
}

func (a *NotificationShowAction) Name() string { return NotificationActionName }

func (a *NotificationShowAction) SetSender(sender NotificationSender) {
	a.mu.Lock()
	a.sender = sender
	a.mu.Unlock()
}

func (a *NotificationShowAction) SetBundle(snapshot NotificationBundleSnapshot) {
	a.mu.Lock()
	a.snapshot = snapshot
	a.mu.Unlock()
}

func (a *NotificationShowAction) Execute(_ context.Context, ev domain.Event, args map[string]any) error {
	a.mu.RLock()
	sender := a.sender
	snapshot := a.snapshot
	a.mu.RUnlock()
	if sender == nil {
		return nil
	}

	slugRaw, ok := args[NotificationArgSlug]
	if !ok {
		return fmt.Errorf("notification.show: %s missing — composition root must rewrite HookSpec.Notification", NotificationArgSlug)
	}
	slug, ok := slugRaw.(string)
	if !ok || slug == "" {
		return fmt.Errorf("notification.show: %s must be a non-empty string, got %T", NotificationArgSlug, slugRaw)
	}

	notification, ok := snapshot.Notifications[slug]
	if !ok {
		return fmt.Errorf("notification.show: notification %q not loaded (check notifications/ + custom/)", slug)
	}

	hookMessage, _ := args[NotificationArgMessage].(string)
	hookField, _ := args[NotificationArgMessageField].(string)
	detailMessage, _ := args[NotificationArgDetailMessage].(string)
	detailField, _ := args[NotificationArgDetailMessageField].(string)

	text, err := ResolveNotificationMessage(ev, notification.Message, notification.MessageField, hookMessage, hookField)
	if err != nil {
		return err
	}
	detailText := ResolveOptionalNotificationMessage(ev, detailMessage, detailField)

	sender.SendNotification(NotificationShowMsg{Notification: notification, Text: text, DetailText: detailText})
	return nil
}

// ResolveNotificationMessage picks the bubble text from up to four configured
// sources, with notification YAML winning over hook-level fallbacks.
func ResolveNotificationMessage(ev domain.Event, notificationMsg, notificationField, hookMsg, hookField string) (string, error) {
	if v, ok := tryLiteral(notificationMsg); ok {
		return v, nil
	}
	if v, ok := tryPayload(ev, notificationField); ok {
		return v, nil
	}
	if v, ok := tryLiteral(hookMsg); ok {
		return v, nil
	}
	if v, ok := tryPayload(ev, hookField); ok {
		return v, nil
	}
	if ev.Body != "" {
		return ev.Body, nil
	}
	return "", fmt.Errorf("notification has no resolvable message — set message/message_field on the notification YAML or the hook entry, or surface ev.Body")
}

func ResolveOptionalNotificationMessage(ev domain.Event, msg, field string) string {
	if v, ok := tryLiteral(msg); ok {
		return v
	}
	if v, ok := tryPayload(ev, field); ok {
		return v
	}
	return ""
}

func tryLiteral(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

func tryPayload(ev domain.Event, field string) (string, bool) {
	if field == "" || ev.Payload == "" {
		return "", false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		return "", false
	}
	raw, ok := payload[field]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}
