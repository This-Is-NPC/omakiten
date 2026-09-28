package actions

import (
	"context"

	"omakiten/internal/domain"
)

// Noop is a zero-side-effect action used by tests and as a smoke option
// in user yamls. It returns any cancellation already requested by the
// engine; otherwise the engine emits a successful hook.executed event.
type Noop struct{}

func (Noop) Name() string { return "noop" }

func (Noop) Execute(ctx context.Context, _ domain.Event, _ map[string]any) error {
	return ctx.Err()
}
