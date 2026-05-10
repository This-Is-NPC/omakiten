package hooks

import (
	"context"

	"omakiten/internal/domain"
)

// Action is the contract every hook target implements. Name is matched
// against Hook.Do; Execute receives the event and the hook's Args.
// Returning an error surfaces in the hook.executed payload as
// success=false but never blocks other hooks. Implementations must
// honor ctx — the engine wraps Execute in a context with timeout
// derived from action-specific args.
type Action interface {
	Name() string
	Execute(ctx context.Context, ev domain.Event, args map[string]any) error
}

// ActionRegistry is the engine-local set of available actions. Looking
// up an action returns nil when the name was never registered, so the
// validator can reject unknown `do:` declarations at config load.
type ActionRegistry struct {
	byName map[string]Action
}

// NewActionRegistry returns an empty registry. Built-in actions register
// themselves through Register; the runtime's composition root may also
// register additional adapters (e.g. the notification show in the upcoming
// task 3).
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{byName: map[string]Action{}}
}

func (r *ActionRegistry) Register(a Action) {
	r.byName[a.Name()] = a
}

func (r *ActionRegistry) Get(name string) (Action, bool) {
	a, ok := r.byName[name]
	return a, ok
}

func (r *ActionRegistry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	return out
}
