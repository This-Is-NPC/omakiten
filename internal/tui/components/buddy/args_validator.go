package buddy

import (
	"fmt"
)

// ValidateShowArgs runs the buddy.show argument shape checks plus
// the cross-field check against the active buddy: the requested
// animation must exist. Composition roots wire this up as the
// per-action validator passed into config.ValidateHooks.
//
// knownAnimations is the set of animation names declared on the
// active buddy. Pass nil/empty to signal "no buddy active"; the
// validator then rejects any buddy.show hook with a clear message
// pointing at config.tui.buddy.active.
func ValidateShowArgs(args map[string]any, knownAnimations map[string]struct{}) error {
	if len(knownAnimations) == 0 {
		return fmt.Errorf("buddy.show requires config.tui.buddy.active to name a loaded buddy")
	}
	parsed, err := ParseArgs(args)
	if err != nil {
		return err
	}
	if _, ok := knownAnimations[parsed.Animation]; !ok {
		return fmt.Errorf("animation %q not declared on the active buddy", parsed.Animation)
	}
	return nil
}

