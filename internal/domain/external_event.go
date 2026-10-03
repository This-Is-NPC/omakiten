package domain

import (
	"fmt"
	"regexp"
)

// Caps on what an outside caller may emit. An external event is written by
// anything holding the token or the CLI, so its size is bounded like any
// other write: rejected, never truncated.
const (
	// MaxExternalFields caps the fields of one external event.
	MaxExternalFields = 32
	// MaxExternalValueBytes caps one field's value.
	MaxExternalValueBytes = 4 * 1024
	// MaxExternalEventsPerMinute caps the external events one project
	// takes in a minute, so a caller in a loop cannot flood the log.
	MaxExternalEventsPerMinute = 60
)

// externalName is the shape of an external event's name and of its fields'
// names: lower snake case, starting with a letter.
var externalName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidateExternalName rejects a name an external event cannot carry.
func ValidateExternalName(name string) error {
	if !externalName.MatchString(name) {
		return NewError(ErrValidation,
			fmt.Sprintf("external event name %q must be lower snake case, start with a letter, and have at most 64 characters", name),
			map[string]any{"field": "name", "value": name})
	}
	return nil
}

// ValidateExternalPayload rejects fields an external event cannot carry:
// too many, a badly formed name, or a value over its cap.
func ValidateExternalPayload(payload map[string]string) error {
	if len(payload) > MaxExternalFields {
		return NewError(ErrValidation,
			fmt.Sprintf("external event has %d fields; the maximum is %d", len(payload), MaxExternalFields),
			map[string]any{"field": "payload", "length": len(payload), "max": MaxExternalFields})
	}
	for key, value := range payload {
		if !externalName.MatchString(key) {
			return NewError(ErrValidation,
				fmt.Sprintf("external event field %q must be lower snake case, start with a letter, and have at most 64 characters", key),
				map[string]any{"field": "payload", "key": key})
		}
		if err := validateByteCap("payload."+key, value, MaxExternalValueBytes); err != nil {
			return err
		}
	}
	return nil
}
