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
//   - action:   on + when + do + args (legacy exec/noop dispatch)
//   - notification:    on + when + notification:<slug>  (notification card)
//
// `do:` must name a registered action; `notification:` must resolve to a
// loaded notification. on/event_type must be a known event type. Empty
// hooks list is allowed.
func ValidateHooks(hooks []HookSpec, isAction HookActionResolver, notifications map[string]Notification) error {
	for i, h := range hooks {
		on := strings.TrimSpace(h.On)
		if on == "" {
			return fmt.Errorf("config.hooks[%d]: on is required", i)
		}
		if !domain.IsKnownEventType(on) {
			return fmt.Errorf("config.hooks[%d]: unknown event_type %q (see internal/domain/event.go::KnownEventTypes)", i, on)
		}

		do := strings.TrimSpace(h.Do)
		notificationSlug := strings.TrimSpace(h.Notification)
		switch {
		case do == "" && notificationSlug == "":
			return fmt.Errorf("config.hooks[%d]: one of do or notification is required", i)
		case do != "" && notificationSlug != "":
			return fmt.Errorf("config.hooks[%d]: do and notification are mutually exclusive — pick one", i)
		case notificationSlug != "":
			bud, ok := notifications[notificationSlug]
			if !ok {
				return fmt.Errorf("config.hooks[%d]: notification %q not loaded (declare a notifications/%s.yaml file)", i, notificationSlug, notificationSlug)
			}
			if strings.TrimSpace(h.Message) != "" && strings.TrimSpace(h.MessageField) != "" {
				return fmt.Errorf("config.hooks[%d]: message and message_field are mutually exclusive — pick one", i)
			}
			if !notificationOrHookHasMessageSource(bud, h) {
				return fmt.Errorf("config.hooks[%d]: notification %q declares no message/message_field and the hook supplies neither — set one on either layer", i, notificationSlug)
			}
		case do != "":
			if isAction != nil && !isAction(do) {
				return fmt.Errorf("config.hooks[%d]: unknown action %q (register it before LoadBundle)", i, do)
			}
			if do == "exec" {
				if err := validateExecArgs(i, h.Args); err != nil {
					return err
				}
			}
		}
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
