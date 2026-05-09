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

// HookActionArgValidator gives the composition root a place to plug
// per-action argument shape checks at LoadBundle. The closure is
// invoked once per hook with the resolved action name and its raw
// args map. Returning an error aborts startup with the wrapped
// message; returning nil leaves the hook accepted.
type HookActionArgValidator func(action string, args map[string]any) error

// ValidateHooks runs the hooks block through the catalog + action
// checks. on:/event_type must be a known event type; do: must be
// registered. exec actions need a non-empty argv array. Empty hooks
// list is allowed. validateArgs (optional) lets the composition root
// register per-action arg validators (e.g. buddy.show) without
// pulling additional packages into config.
func ValidateHooks(hooks []HookSpec, isAction HookActionResolver, validateArgs HookActionArgValidator) error {
	for i, h := range hooks {
		on := strings.TrimSpace(h.On)
		if on == "" {
			return fmt.Errorf("config.hooks[%d]: on is required", i)
		}
		if !domain.IsKnownEventType(on) {
			return fmt.Errorf("config.hooks[%d]: unknown event_type %q (see internal/domain/event.go::KnownEventTypes)", i, on)
		}
		do := strings.TrimSpace(h.Do)
		if do == "" {
			return fmt.Errorf("config.hooks[%d]: do is required", i)
		}
		if isAction != nil && !isAction(do) {
			return fmt.Errorf("config.hooks[%d]: unknown action %q (register it before LoadBundle)", i, do)
		}
		if do == "exec" {
			if err := validateExecArgs(i, h.Args); err != nil {
				return err
			}
		}
		if validateArgs != nil {
			if err := validateArgs(do, h.Args); err != nil {
				return fmt.Errorf("config.hooks[%d]: %w", i, err)
			}
		}
	}
	return nil
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
