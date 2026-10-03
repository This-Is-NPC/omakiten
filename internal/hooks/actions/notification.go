package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/hooks"
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
	// NotificationArgResolvedKit names the kit identity the hook entry
	// originated from (root snapshot or sub-kit snapshot). The action
	// uses it to pick the matching per-kit notification catalog so a
	// sub-kit-only slug resolves through the sub-kit catalog instead of
	// falling back to the root one (#301 review §11557 finding A5).
	NotificationArgResolvedKit = "_notification_resolved_kit"
)

// NotificationShowMsg is the neutral message emitted by NotificationShowAction.
// TUI code adapts it to Bubble Tea by sending this value into the tea.Program;
// CLI runtimes leave the sender nil so the action is a silent no-op.
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
// Catalog is optional — when non-nil, Execute expands `${{intl:KEY}}`
// tokens inside the resolved message + detail text against it so users
// (and bundled presets) can keep notification copy in the language
// catalog instead of hardcoding it in each preset's hook entries.
//
// NotificationsByKit holds per-kit catalogs (kit identity → slug →
// notification). When the hook entry's resolved kit is present and the
// slug resolves against that per-kit catalog, the per-kit notification
// wins over the root entry — so a sub-kit hook can ship its own slug
// without colliding with (or being shadowed by) the root catalog (#301
// review §11557 finding A5). Falls back to `Notifications` when the
// per-kit map is unset for the resolved kit or carries no entry for
// the slug.
type NotificationBundleSnapshot struct {
	Notifications      map[string]config.Notification
	NotificationsByKit map[string]map[string]config.Notification
	Catalog            *config.Catalog
}

// NotificationShowAction resolves a configured notification and emits the
// rendered message payload through a sender supplied by the TUI runtime.
// With no sender, as in the CLI and the daemon, it records the rendered
// notification as notification.shown so the clients following the event
// stream show it.
type NotificationShowAction struct {
	mu       sync.RWMutex
	sender   NotificationSender
	recorder hooks.EventRecorder
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

// SetRecorder installs where a notification with no sender is recorded.
func (a *NotificationShowAction) SetRecorder(recorder hooks.EventRecorder) {
	a.mu.Lock()
	a.recorder = recorder
	a.mu.Unlock()
}

func (a *NotificationShowAction) SetBundle(snapshot NotificationBundleSnapshot) {
	a.mu.Lock()
	a.snapshot = snapshot
	a.mu.Unlock()
}

func (a *NotificationShowAction) Execute(ctx context.Context, ev domain.Event, args map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.RLock()
	sender := a.sender
	recorder := a.recorder
	snapshot := a.snapshot
	a.mu.RUnlock()
	if sender == nil && recorder == nil {
		return nil
	}

	slug, err := notificationSlug(args)
	if err != nil {
		return err
	}

	resolvedKit, _ := args[NotificationArgResolvedKit].(string)
	notification, ok := resolveNotification(snapshot, slug, resolvedKit)
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
	if snapshot.Catalog != nil {
		text = snapshot.Catalog.Resolve(text)
		if detailText != "" {
			detailText = snapshot.Catalog.Resolve(detailText)
		}
	}

	// Render every action's Command through text/template so users can wire
	// args like "--project={{.Project.Slug}}" or "--id={{.Payload.id}}".
	// Templating errors propagate so the user sees a precise failure instead
	// of a notification that quietly dispatches the wrong command.
	rendered, err := renderActionArguments(notification.Actions, ev)
	if err != nil {
		return fmt.Errorf("notification.show: notification %q: %w", slug, err)
	}
	notification.Actions = rendered

	if err := ctx.Err(); err != nil {
		return err
	}
	msg := NotificationShowMsg{Notification: notification, Text: text, DetailText: detailText}
	if sender == nil {
		return recordShown(ctx, recorder, ev, resolvedKit, msg)
	}
	sender.SendNotification(msg)
	return nil
}

// notificationSlug reads the slug the composition root staged in args.
func notificationSlug(args map[string]any) (string, error) {
	slugRaw, ok := args[NotificationArgSlug]
	if !ok {
		return "", fmt.Errorf("notification.show: %s missing — composition root must rewrite HookSpec.Notification", NotificationArgSlug)
	}
	slug, ok := slugRaw.(string)
	if !ok || slug == "" {
		return "", fmt.Errorf("notification.show: %s must be a non-empty string, got %T", NotificationArgSlug, slugRaw)
	}
	return slug, nil
}

// recordShown writes notification.shown for the event that fired the hook.
func recordShown(ctx context.Context, recorder hooks.EventRecorder, ev domain.Event, resolvedKit string, msg NotificationShowMsg) error {
	payload, err := json.Marshal(map[string]string{
		"notification": msg.Notification.Name,
		"text":         msg.Text,
		"detail":       msg.DetailText,
		"event_type":   ev.EventType,
		"resolved_kit": resolvedKit,
	})
	if err != nil {
		return err
	}
	return recorder.RecordEntityEvent(ctx, domain.EventEntitySystem, 0, ev.ProjectID, domain.EventTypeNotificationShown, string(payload))
}

// resolveNotification picks the notification entry for slug, preferring
// the per-kit catalog (when present and non-empty) before falling back
// to the root catalog. Per-kit lookup is the locked behaviour from
// #301 review §11557 finding A5 — a sub-kit-only slug must execute
// successfully at runtime, not just validate at load time.
func resolveNotification(snapshot NotificationBundleSnapshot, slug, resolvedKit string) (config.Notification, bool) {
	if resolvedKit != "" {
		if perKit, ok := snapshot.NotificationsByKit[resolvedKit]; ok {
			if n, ok := perKit[slug]; ok {
				return n, true
			}
		}
	}
	n, ok := snapshot.Notifications[slug]
	return n, ok
}

func renderActionArguments(actions []config.NotificationAction, ev domain.Event) ([]config.NotificationAction, error) {
	if len(actions) == 0 {
		return actions, nil
	}
	data, err := templateData(ev)
	if err != nil {
		return nil, err
	}
	out := make([]config.NotificationAction, len(actions))
	for i, action := range actions {
		out[i] = action
		if len(action.Arguments) == 0 {
			continue
		}
		rendered := make(map[string]any, len(action.Arguments))
		for key, value := range action.Arguments {
			if segment, ok := value.(string); ok {
				text, err := renderTemplateSegment(segment, data)
				if err != nil {
					return nil, fmt.Errorf("actions[%d].arguments.%s: %w", i, key, err)
				}
				value = text
			}
			rendered[key] = value
		}
		out[i].Arguments = rendered
	}
	return out, nil
}

func renderTemplateSegment(segment string, data map[string]any) (string, error) {
	if !strings.Contains(segment, "{{") {
		return segment, nil
	}
	tpl, err := template.New("notification-action").Option("missingkey=error").Parse(segment)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func templateData(ev domain.Event) (map[string]any, error) {
	payload := map[string]any{}
	if ev.Payload != "" && ev.Payload != "{}" {
		if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
			return nil, fmt.Errorf("payload is not valid JSON: %w", err)
		}
	}
	return map[string]any{"Payload": payload, "Event": map[string]any{
		"ID":         ev.ID,
		"EventType":  ev.EventType,
		"EntityType": ev.EntityType,
		"EntityID":   ev.EntityID,
		"ProjectID":  ev.ProjectID,
	}}, nil
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
	switch v := raw.(type) {
	case string:
		if v == "" {
			return "", false
		}
		return v, true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case float64:
		// JSON numbers always round-trip through float64; format whole
		// values as ints so "2 tasks" stays cleaner than "2.000000 tasks".
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), true
		}
		return fmt.Sprintf("%g", v), true
	}
	return "", false
}
