package config

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
)

// HookActionResolver reports whether a `do:` name is a registered
// action. Composition root passes a closure backed by the engine's
// ActionRegistry; tests pass a fixed map. Validation hard-rejects
// unknown actions so typos surface at LoadBundle, not at first event.
type HookActionResolver func(name string) bool

// ValidateHooks runs the hooks block through the catalog + ref
// checks. Each entry is one of two mutually-exclusive shapes:
//
//   - action:        on + when + do + args  (exec/noop dispatch)
//   - notification:  on + when + notification:<slug>  (notification card)
//
// `do:` must name a registered action; `notification:` must resolve to a
// loaded notification. on/event_type must be one of `knownEvents` —
// the bundle-local event_type registry (typically EventsSettings.Definitions)
// supplies the closed set so validation does not depend on a global
// domain.KnownEventTypes that is empty at LoadBundle time. A nil/empty
// knownEvents disables the event_type check, which lets test fixtures
// supply a hook entry without declaring a full events block.
func ValidateHooks(hooks []HookSpec, knownEvents map[string]struct{}, isAction HookActionResolver, notifications map[string]Notification) error {
	for i, h := range hooks {
		if err := validateHook(i, h, knownEvents, isAction, notifications); err != nil {
			return err
		}
	}
	return nil
}

func validateHook(i int, h HookSpec, knownEvents map[string]struct{}, isAction HookActionResolver, notifications map[string]Notification) error {
	on := strings.TrimSpace(h.On)
	if on == "" {
		return fmt.Errorf("config.hooks[%d]: on is required", i)
	}
	if len(knownEvents) > 0 {
		if _, ok := knownEvents[on]; !ok {
			return fmt.Errorf("config.hooks[%d]: unknown event_type %q (declare it under config.events.definitions in the active kit)", i, on)
		}
	}

	do := strings.TrimSpace(h.Do)
	notificationSlug := strings.TrimSpace(h.Notification)
	if do == "" && notificationSlug == "" {
		return fmt.Errorf("config.hooks[%d]: one of do or notification is required", i)
	}
	if do != "" && notificationSlug != "" {
		return fmt.Errorf("config.hooks[%d]: do and notification are mutually exclusive — pick one", i)
	}
	if notificationSlug != "" {
		// A notification with no screen records notification.shown, which
		// would fire the same hook again.
		if on == domain.EventTypeNotificationShown {
			return fmt.Errorf("config.hooks[%d]: a notification cannot fire on %s, which a notification records", i, domain.EventTypeNotificationShown)
		}
		return validateHookNotification(i, h, notificationSlug, notifications)
	}
	if isAction != nil && !isAction(do) {
		return fmt.Errorf("config.hooks[%d]: unknown action %q (register it before LoadBundle)", i, do)
	}
	if do == "exec" {
		return validateExecArgs(i, h.Args)
	}
	return nil
}

func validateHookNotification(i int, h HookSpec, slug string, notifications map[string]Notification) error {
	bundled, ok := notifications[slug]
	if !ok {
		return fmt.Errorf("config.hooks[%d]: notification %q not loaded (declare a notifications/%s.yaml file)", i, slug, slug)
	}
	if strings.TrimSpace(h.Message) != "" && strings.TrimSpace(h.MessageField) != "" {
		return fmt.Errorf("config.hooks[%d]: message and message_field are mutually exclusive — pick one", i)
	}
	if strings.TrimSpace(h.DetailMessage) != "" && strings.TrimSpace(h.DetailMessageField) != "" {
		return fmt.Errorf("config.hooks[%d]: detail_message and detail_message_field are mutually exclusive — pick one", i)
	}
	if !notificationOrHookHasMessageSource(bundled, h) {
		return fmt.Errorf("config.hooks[%d]: notification %q declares no message/message_field and the hook supplies neither — set one on either layer", i, slug)
	}
	return nil
}

// notificationOrHookHasMessageSource reports whether the (notification, hook) pair
// resolves to at least one bubble-text source. Either layer may
// supply a literal `message` or a payload-driven `message_field`;
// the action picks the notification layer first when both are set.
func notificationOrHookHasMessageSource(bud Notification, h HookSpec) bool {
	if strings.TrimSpace(bud.Message) != "" || strings.TrimSpace(bud.MessageField) != "" {
		return true
	}
	if strings.TrimSpace(h.Message) != "" || strings.TrimSpace(h.MessageField) != "" {
		return true
	}
	return false
}

func validateExecArgs(i int, args map[string]interface{}) error {
	raw, ok := args["argv"]
	if !ok {
		return fmt.Errorf("config.hooks[%d]: action exec requires args.argv (array of strings)", i)
	}
	switch v := raw.(type) {
	case []string:
		if len(v) == 0 {
			return fmt.Errorf("config.hooks[%d]: action exec args.argv must be non-empty", i)
		}
	case []interface{}:
		if len(v) == 0 {
			return fmt.Errorf("config.hooks[%d]: action exec args.argv must be non-empty", i)
		}
		for j, item := range v {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("config.hooks[%d]: action exec args.argv[%d] must be a string, got %T", i, j, item)
			}
		}
	default:
		return fmt.Errorf("config.hooks[%d]: action exec args.argv must be an array of strings, got %T", i, raw)
	}
	return nil
}
