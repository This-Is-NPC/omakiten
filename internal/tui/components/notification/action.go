package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// MessageSender is the narrow port ShowAction.Execute uses to deliver
// a ShowMsg into the running tea program. *tea.Program already
// implements Send(tea.Msg) so the production wiring is a direct cast;
// integration tests inject a recorder to assert on dispatch without
// needing a live program.
type MessageSender interface {
	Send(tea.Msg)
}

// ActionName is the canonical name registered with the hooks engine.
// Hook entries do NOT spell it out — the composition root rewrites
// every HookSpec carrying `notification: <slug>` into a notification.show invocation
// before handing the list to the engine. The constant is exported for
// tests + the hooks validator.
const ActionName = "notification.show"

// ArgNotificationSlug, ArgMessage, ArgMessageField, ArgDetailMessage, and
// ArgDetailMessageField are the internal arg keys the action receives.
// The composition root stages them from the user-facing HookSpec fields;
// users never write these keys directly. Empty values are fine — the
// action falls back to the notification YAML when a hook layer key is blank.
const (
	ArgNotificationSlug   = "_notification_slug"
	ArgMessage            = "_notification_message"
	ArgMessageField       = "_notification_message_field"
	ArgDetailMessage      = "_notification_detail_message"
	ArgDetailMessageField = "_notification_detail_message_field"
)

// ShowMsg is the message the action sends into the running tea.Program
// when an event matches a notification hook. It carries the resolved Notification
// (full YAML config) plus the rendered bubble text. The TUI parent
// constructs a notification.Model from these values.
type ShowMsg struct {
	Notification config.Notification
	Text         string
	DetailText   string
}

// BundleSnapshot is the narrow port the action consults at execute
// time: the loaded notifications map keyed by slug. We pass a snapshot
// rather than the whole bundle so callers can refresh the snapshot
// on bundle reload without rebuilding the action.
type BundleSnapshot struct {
	Notifications map[string]config.Notification
}

// ShowAction is the hooks.Action implementation that fires a notification
// notification. It holds an optional MessageSender (typically
// *tea.Program); CLI/MCP composition roots register the action with
// sender=nil and the TUI composition root calls SetSender once
// tea.NewProgram returns.
type ShowAction struct {
	mu       sync.RWMutex
	sender   MessageSender
	snapshot BundleSnapshot
}

// NewShowAction constructs a disconnected action. The TUI binds the
// sender post-hoc; CLI/MCP runs leave it nil so Execute is a silent
// no-op.
func NewShowAction(snapshot BundleSnapshot) *ShowAction {
	return &ShowAction{snapshot: snapshot}
}

// Name satisfies hooks.Action.
func (a *ShowAction) Name() string { return ActionName }

// SetProgram links the action to a running tea.Program. Convenience
// wrapper around SetSender.
func (a *ShowAction) SetProgram(p *tea.Program) {
	a.SetSender(p)
}

// SetSender installs the message sender used at Execute time. nil is
// allowed and turns Execute back into a no-op.
func (a *ShowAction) SetSender(sender MessageSender) {
	a.mu.Lock()
	a.sender = sender
	a.mu.Unlock()
}

// SetBundle refreshes the snapshot the action consults at execute
// time. Used after a config reload.
func (a *ShowAction) SetBundle(snapshot BundleSnapshot) {
	a.mu.Lock()
	a.snapshot = snapshot
	a.mu.Unlock()
}

// Execute looks up the notification named in args (rewritten by the
// composition root from HookSpec.Notification) and emits a ShowMsg with the
// resolved bubble text. With no sender (CLI/MCP) it returns nil so
// the hook records success without doing anything visible.
func (a *ShowAction) Execute(_ context.Context, ev domain.Event, args map[string]any) error {
	a.mu.RLock()
	sender := a.sender
	snapshot := a.snapshot
	a.mu.RUnlock()
	if sender == nil {
		return nil
	}

	slugRaw, ok := args[ArgNotificationSlug]
	if !ok {
		return fmt.Errorf("notification.show: %s missing — composition root must rewrite HookSpec.Notification", ArgNotificationSlug)
	}
	slug, ok := slugRaw.(string)
	if !ok || slug == "" {
		return fmt.Errorf("notification.show: %s must be a non-empty string, got %T", ArgNotificationSlug, slugRaw)
	}

	notification, ok := snapshot.Notifications[slug]
	if !ok {
		return fmt.Errorf("notification.show: notification %q not loaded (check notifications/ + custom/)", slug)
	}

	hookMessage, _ := args[ArgMessage].(string)
	hookField, _ := args[ArgMessageField].(string)
	detailMessage, _ := args[ArgDetailMessage].(string)
	detailField, _ := args[ArgDetailMessageField].(string)

	text, err := resolveMessage(ev, notification.Message, notification.MessageField, hookMessage, hookField)
	if err != nil {
		return err
	}
	detailText := resolveOptionalMessage(ev, detailMessage, detailField)

	sender.Send(ShowMsg{Notification: notification, Text: text, DetailText: detailText})
	return nil
}

// resolveMessage picks the bubble text from up to four configured
// sources, in this priority order (notification YAML wins on tie-break):
//
//  1. notification.Message            — literal text declared in the notification YAML
//  2. ev.Payload[notification.MessageField] — payload key from the notification YAML
//  3. hookMessage              — literal text declared on the hook entry
//  4. ev.Payload[hookField]    — payload key from the hook entry
//  5. ev.Body                  — last-resort fallback
//
// Any non-empty hit wins. Returning an error here means none of the
// sources resolved to a non-empty string — the action refuses to
// pop a blank balloon.
func resolveMessage(ev domain.Event, notificationMsg, notificationField, hookMsg, hookField string) (string, error) {
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

func resolveOptionalMessage(ev domain.Event, msg, field string) string {
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
