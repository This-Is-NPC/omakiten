package actions

import "omakiten/internal/hooks"

// RegisterBuiltins installs the runtime's first-party actions on the
// supplied registry: exec for shelling out and noop for tests / smoke
// configs. Composition root calls this exactly once at startup; callers
// that need bundle-backed actions such as notification.show register them
// directly via hooks.ActionRegistry.Register.
func RegisterBuiltins(reg *hooks.ActionRegistry) {
	reg.Register(Exec{})
	reg.Register(Noop{})
}
