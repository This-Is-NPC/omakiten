# Hooks

Hooks let you wire automation into Omakiten's domain events without
writing Go. You declare them in the active profile yaml under `config.hooks`; the
runtime subscribes to the in-process events bus and dispatches the
matching `do:` action asynchronously.

This guide covers the YAML schema, the per-action argument contracts,
and a few worked recipes. For the broader event catalog and runtime
behaviour, see [`domain-events.md`](domain-events.md).

## Schema

```yaml
config:
  hooks:
    - on: <event_type>            # required; must be in domain.KnownEventTypes
      when:                       # optional; top-level payload equality (AND across keys)
        <payload_key>: <value>
      do: <action_name>           # required; must be a registered action
      args:                       # optional; shape depends on the action
        <key>: <value>
```

Mutating the block requires an app restart — the bundle is loaded once
at startup, same as every other config block.

### Event matching

- `on:` is matched against `domain.Event.EventType` for string equality.
  The validator rejects values outside `domain.KnownEventTypes` so typos
  fail fast at `LoadBundle` instead of going silently un-fired.
- `when:` keys are top-level payload keys in
  `domain.Event.Payload` (the JSON object stored alongside the event).
  Each declared key must be present and equal to the declared value;
  multiple keys AND together. Strings, JSON booleans (`"true"` /
  `"false"`), and JSON numbers (re-encoded via `encoding/json`) all
  match against the YAML string.

### Dispatch lifecycle

1. The bus delivers the event synchronously to the engine.
2. The engine evaluates `on:` + `when:`, then for each matched hook
   spawns a goroutine. The publisher returns immediately.
3. Inside the goroutine the engine drops any inherited deadline so the
   action's own timeout applies, then calls `Action.Execute(ctx, ev, args)`.
4. After the action returns (or panics — recovered), the engine emits
   `hook.executed` via the events store. If the engine never reached
   step 3 (action missing, gate closed, no match) **no event is
   emitted** — `hook.executed` records what happened, not what was
   tried.

### Channel gates

The same `config.events` channels covered in
[`domain-events.md`](domain-events.md#configuration) apply:

- `log` — gates persistence of the underlying domain event.
- `broadcast` — gates the bus fan-out.
- `hook` — gates the engine's dispatch. When this resolves false for
  the event_type, no hook fires for that event.

A hook that never gets a chance to run never appears in the
`hook.executed` log; that is by design.

## Built-in actions

### `exec`

Runs an external command without invoking a shell. Args are taken
literally from `argv` (no expansion); the entire `domain.Event` is
piped to stdin as JSON, so scripts can extract whatever they need with
`jq` or any JSON parser without templating in YAML.

| Key          | Type            | Required | Default | Description |
| ------------ | --------------- | -------- | ------- | ----------- |
| `argv`       | array of string | yes      | —       | `argv[0]` is the binary to run; `argv[1:]` are literal arguments. |
| `timeout_ms` | integer         | no       | `30000` | Hard deadline. On expiry the engine kills the process with `SIGKILL` and `hook.executed.success` lands as false. |

The hook fails (and emits `success=false` plus the captured stderr in
`error`) when:

- `argv` is missing or contains non-string entries (the validator
  rejects this at `LoadBundle`).
- The command exits non-zero. Captured stderr is included in the error
  message and the structured log line.
- The timeout fires. The error wraps `context.DeadlineExceeded` and
  references the configured `timeout_ms`.

### `noop`

Always returns nil. Used by tests and as a smoke option in user yamls
when you want to confirm the engine sees an event without side-effects.
Takes no args.

### Notification hooks (`notification: <slug>`)

Notification hooks ship a notification card over the TUI when the event
matches. They use a different shape from the action hooks above: the
entry carries `notification: <slug>` instead of `do:` + `args:`. Mixing the
two shapes in the same entry fails validation.

```yaml
hooks:
  - on: guard.violated
    notification: task-guard             # → notifications/task-guard.yaml
  - on: comment
    when: { author_type: agent }
    notification: agent-note
    message: "Agent comment"             # optional hook-level message fallback
    detail_message_field: hint            # optional second page shown with tab
```

The hook entry may carry `message:` (literal) or `message_field:`
(payload key) as a **fallback** the action consults when the
referenced notification YAML did not declare its own. Notification YAML wins on
tie-break — see [`notifications.md`](notifications.md#message-resolution) for
the full precedence table.

The hook entry may also carry `detail_message:` or `detail_message_field:`.
When either resolves to text, the TUI card shows the normal short message first;
pressing `tab` toggles to the detail page. This is intended for funny short copy
plus the complete guard/error hint. Detail fields are optional and are ignored
outside the TUI just like the notification itself.

Every render knob (animation, position, dismiss, message text, card
size, colors) lives inside the notification YAML — the active profile yaml only
links events to slugs. See [`notifications.md`](notifications.md) for the notification
schema.

The hook is rejected at `LoadBundle` when:

- `notification: <slug>` references a notification that is not loaded (no matching
  `notifications/<slug>.yaml` or `notifications/custom/<slug>.yaml`);
- both `notification:` and `do:` are set in the same entry;
- neither `notification:` nor `do:` is set.

Outside the TUI (CLI / MCP processes) the notification dispatch is a silent
no-op so the same hook block stays valid in every entry point.

## Recipes

### Log every blocked delete

Capture every guard violation against `task.delete` so a sidecar can
review them later:

```yaml
config:
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      do: exec
      args:
        argv: ["./scripts/log-blocked-delete.sh"]
        timeout_ms: 3000
```

`./scripts/log-blocked-delete.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
read -r event
echo "$event" | jq -c '{
  ts: now,
  task_id:    .payload | fromjson | .target.task_id,
  attempted_by: .payload | fromjson | .attempted_by,
  hint:       .payload | fromjson | .hint,
}' >> ~/.local/share/omakiten/blocked-deletes.ndjson
```

### Run a script only on archive

```yaml
config:
  hooks:
    - on: task.archived
      do: exec
      args:
        argv: ["./scripts/post-archive.sh"]
```

The script reads the JSON event from stdin and can call
`jq -r .entity_id` to pull the archived task's id.

### Show a notification when a guard blocks delete

```yaml
config:
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      notification: task-delete-warning
      message: "Trying to burn the quest log? Adorable."
      detail_message_field: hint
```

Every render knob (position, animation, dismiss) lives inside the notification
YAML. The hook supplies the short message and, when useful, a detail field such
as `hint` for the second page. To customise per event, copy the notification file to
`notifications/custom/<your-slug>.yaml`, change the slug, and reference it
from a new hook entry.

### Prompt to migrate orphaned tasks after a config swap

```yaml
config:
  hooks:
    - on: bundle.swapped
      when: { has_orphans: "true" }
      notification: kitten_orphan_migration
      message_field: orphan_count
      detail_message_field: orphan_count
```

`bundle.swapped` fires from the TUI hot-reload path (Settings → Config picker)
with payload `{from_workflow, to_workflow, orphan_count, has_orphans, groups}`.
The `when:` filter is a string match — match `true` as `"true"` (quoted) because
`HookSpec.When` decodes as `map[string]string`. Quote `"false"` the same way.

The matching `notifications/kitten_orphan_migration.yaml` declares two interactive
action buttons (see `.docs/notifications.md` → "Action buttons"). Pressing
`m` dispatches `okt workflow orphans --confirm` in-process; pressing `s`
dismisses without side effects. Both keystrokes emit `confirmation.granted`
with the human author_type so the audit log records who approved the run.

### Surface every agent comment for 8 seconds

```yaml
config:
  hooks:
    - on: comment
      when: { author_type: agent }
      notification: agent-note
```

Assuming `notifications/agent-note.yaml` carries `dismiss: { mode: timeout,
after_ms: 12000, keys: [...] }` and `message_field: body`, the comment renders
inline, can be closed manually, and disappears 12s after settling.

### Filter by author_type or source

`payload` matching is keyed on the top level of `domain.Event.Payload`,
not the row columns. To filter on `source`, query the column out via
the script after reading stdin — the JSON body includes `source`,
`author_type`, `agent_model`, and `agent_session_id` fields.

```bash
#!/usr/bin/env bash
read -r event
src=$(echo "$event" | jq -r '.source')
[[ "$src" == "mcp" ]] || exit 0  # only act on agent activity
echo "$event" >> ~/agent-events.ndjson
```

## Adding a new action

1. Implement `hooks.Action` in a new file under
   `internal/hooks/actions/` — `Name()` returns the YAML name; `Execute`
   takes `ctx`, the `domain.Event`, and the per-hook `args`.
2. Register it. The runtime registers the built-ins in
   `internal/hooks/actions/registry.go::RegisterBuiltins`. Composition-
   root callers (TUI startup, integration tests) can register
   additional actions on `agentruntime.Runtime.ActionRegistry()`.
3. Document the args contract here under "Built-in actions" — each new
   action gets its own table.
4. The `LoadBundle` validator skips `do:` checking (no engine yet at
   that layer); composition root re-runs `config.ValidateHooks` against
   the live registry so unknown action names fail startup with a clear
   error.
