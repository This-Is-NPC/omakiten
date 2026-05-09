// Package actions provides the built-in hook actions registered by the
// runtime composition root: noop (test fixture) and exec (shell-less
// command runner with JSON stdin and timeout).
package actions

import (
	"context"

	"omakiten/internal/domain"
)

// Noop is a zero-side-effect action used by tests and as a smoke option
// in user yamls. It always returns nil; the engine still emits
// hook.executed for it so observability stays uniform.
type Noop struct{}

func (Noop) Name() string { return "noop" }

func (Noop) Execute(_ context.Context, _ domain.Event, _ map[string]any) error {
	return nil
}
