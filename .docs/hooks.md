# Hooks

Hooks let you wire automation into Omakiten's domain events without
writing Go. You declare them in `omakiten.yaml::config.hooks`; the
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

### `buddy.show`

Spawns the configured ASCII mascot over the TUI when the event matches.
Outside the TUI (CLI / MCP processes) the action is a silent no-op so
the same hook block stays valid in every entry point. The buddy
catalogue lives in `buddies/<name>.yaml` and the active mascot is
selected via `config.tui.buddy.active`. See
[`buddies.md`](buddies.md) for the buddy YAML schema, color refs, and
authoring guidance.

| Key                  | Type    | Required | Default                       | Description |
| -------------------- | ------- | -------- | ----------------------------- | ----------- |
| `animation`          | string  | yes      | —                             | Name of an animation declared on the active buddy (`idle`, `deny`, …). The validator rejects names not present at `LoadBundle`. |
| `position`           | string  | yes      | —                             | One of `top-left`, `top-center`, `top-right`, `middle-left`, `center`, `middle-right`, `bottom-left`, `bottom-center`, `bottom-right`. |
| `typing_ms_per_char` | integer | yes      | —                             | Per-character delay during the appear-typing phase. `0` means show the full bubble immediately. |
| `dismiss`            | object  | yes      | —                             | Discriminated by `mode`. See below. |
| `message_field`      | string  | yes      | —                             | Top-level key to pull from `domain.Event.Payload`. Falls back to `domain.Event.Body`; an empty result aborts the show. |
| `frame_interval_ms`  | integer | no       | buddy YAML's `frame_interval_ms` | Override per-hook frame cadence; otherwise the buddy's declared rhythm wins. Must be `> 0` when set. |

`dismiss.mode` selects which extra fields apply:

| `mode`        | Required extra | Behaviour |
| ------------- | -------------- | --------- |
| `key`         | `keys` — non-empty array of key names | Buddy stays until the user presses one of the listed keys. Ignored while typing. |
| `timeout`     | `after_ms` — integer `> 0`            | Timer starts when the buddy reaches `Settled` (typing finished); expires → dismiss. |
| `next_status` | none                                  | Buddy stays until the parent triggers a domain transition; useful for sticky “did you mean…” prompts. |

The hook is rejected at `LoadBundle` when:

- `config.tui.buddy.active` is empty AND any hook does `do: buddy.show`;
- `animation` does not exist on the active buddy;
- `position` is not one of the nine anchors;
- `dismiss.mode` is `key` without a non-empty `keys`, or `timeout` without a positive `after_ms`;
- `typing_ms_per_char` is negative; `frame_interval_ms` is `<= 0` when supplied.

`message_field` is a single top-level key — path syntax is intentionally
out of scope. Wire payloads to top-level fields if you need to surface a
specific value.

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

### Show a mascot when a guard blocks delete

```yaml
config:
  tui:
    buddy:
      active: kitten
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      do: buddy.show
      args:
        animation: deny
        position: top-right
        typing_ms_per_char: 25
        dismiss: { mode: key, keys: [esc] }
        message_field: hint
```

The guard violation event ships a `hint` field in its payload (see
[`guards-guide.md`](guards-guide.md)); the buddy types it letter by
letter, settles, and disappears on `esc`.

### Surface every agent comment for 8 seconds

```yaml
config:
  tui:
    buddy:
      active: kitten
  hooks:
    - on: comment.created
      when: { author_type: agent }
      do: buddy.show
      args:
        animation: idle
        position: bottom-center
        typing_ms_per_char: 0
        dismiss: { mode: timeout, after_ms: 8000 }
        message_field: body
```

`typing_ms_per_char: 0` shows the full comment instantly so the buddy
is mostly visible during the timeout window, not while typing.

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
