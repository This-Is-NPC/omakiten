// Package actions is the registry of side-effects available to YAML
// hooks. Each Action exposes Name() and Execute(ctx, event, args). The
// canonical action is `exec` — runs an external command without invoking
// a shell; argv comes literally from YAML, the event payload is piped to
// stdin as JSON. New actions register through ActionRegistry in the
// parent package.
package actions
