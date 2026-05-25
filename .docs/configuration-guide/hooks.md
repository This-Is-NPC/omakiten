# Hooks

Hooks let you wire automation into Omakiten's domain events without
writing Go. You declare them in the active profile yaml under `config.hooks`; the
runtime subscribes to the in-process events bus and dispatches the
matching `do:` action asynchronously.

This guide covers the YAML schema, the per-action argument contracts,
and a few worked recipes. For the canonical list of event types,
inspect `internal/domain/events.go::KnownEventTypes` — the source of
truth for everything an `on:` clause can subscribe to.

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
at startup, same as every other config block. (The TUI hot-reload path
rebuilds the engine when the active YAML's mtime changes, so an in-app
edit + save will be picked up by the next event; CLI / MCP processes
that run as one-shots reload by re-exec.)

### Per-project dispatch

Each `ProjectRuntime` in the `BundleCache` owns its own `hooks.Engine`,
`ActionRegistry`, and `NotificationShowAction`. Engines filter their
dispatch by `engine.projectID == event.ProjectID`:

- engine `projectID == 0` (bootstrap window before a project resolves, or tests) catches all events.
- event `ProjectID == 0` (system events like `bundle.swapped`,
  `hook.executed` written against the system entity) reaches every engine.
- otherwise the engine reacts only to events scoped to its project.

The consequence: a `mcp.tool_call` hook declared in project A's bundle
will not fire on tool calls dispatched against project B's service,
even when both run through the same `okt mcp serve` process. See
[`mcp.md`](../mcp.md#per-project-routing) for how the dispatch
side decides which project a tool call belongs to.

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
[`system.md § config.events`](system.md#configevents) apply:

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
| `argv`       | array of string | yes      | —       | `argv[0]` is the binary to run; `argv[1:]` are literal arguments. See **argv[0] resolution** below. |
| `timeout_ms` | integer         | no       | `30000` | Hard deadline. On expiry the engine kills the process with `SIGKILL` and `hook.executed.success` lands as false. |

#### argv[0] resolution

`argv[0]` is pinned to an absolute path before the engine spawns the
child so a hook in a project YAML cannot trigger a PATH-shadowed
binary on the operator's machine. Resolution rules:

- **Absolute path** (`/usr/bin/jq`, `/home/me/bin/run.sh`) — passed
  through after `filepath.Clean`.
- **Bare command name** (`bash`, `jq`, `okt`) — resolved via
  `exec.LookPath` against the current `PATH` at hook-execution time
  and pinned to the resulting absolute path. If `PATH` does not
  contain the binary, the hook fails before launch.
- **Explicit relative path** (`./script.sh`, `../bin/foo`,
  `sub/dir/foo`) — **rejected**. The hook YAML cannot reason about
  the runtime's CWD, so resolving relative paths against it is a
  footgun. Use an absolute path, or wrap the script in a bare command
  (`argv: ["bash", "/abs/path/to/script.sh"]`).

The hook fails (and emits `success=false` plus the captured stderr in
`error`) when:

- `argv` is missing or contains non-string entries (the validator
  rejects this at `LoadBundle`).
- `argv[0]` is an explicit relative path or is not found on `PATH`.
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
        argv: ["bash", "/home/me/scripts/log-blocked-delete.sh"]
        timeout_ms: 3000
```

`/home/me/scripts/log-blocked-delete.sh`:

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
        argv: ["bash", "/home/me/scripts/post-archive.sh"]
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

## Guard hints and i18n

Guard hint strings reaching a hook payload (`event.payload.hint` on
`guard.violated`) may embed `${{intl:KEY}}` tokens so preset YAMLs keep
hint copy in the locale catalog instead of inlining a single language.
`resolveHint` in `internal/app/guards/evaluator.go:212` expands the
tokens against the active i18n catalog via `Snapshot.ResolveGuardHint`
before the violation event is emitted, so the hook (and any
notification chained off it via `detail_message_field: hint`) always
sees the resolved string.

Fallback chain when a key is absent: active locale → baseline locale →
**literal key text**. `Catalog.Get` returns the key verbatim and logs
at debug level (`catalog: key missing from active and baseline;
returning key literal`) — hooks never see an empty hint, but they may
see the bare key when a translation is missing. Malformed tokens
(`${{intl:`, `${{intl:}}`) are left verbatim with a debug-level log
line; only well-formed `${{intl:KEY}}` is substituted.

## Step-by-step walkthrough — wiring a hook into your workflow

This walkthrough takes you from "I have Omakiten installed" to "a script of mine runs every time event X happens" without writing Go. The canonical action ships in the kit (`exec`) so anything you can run in a shell can react to a task move, a comment, a guard violation, or any other event in `internal/domain/events.go::KnownEventTypes`.

### Prerequisites

- Omakiten installed and at least one project registered. If `okt --version` works and `okt project list` shows your project, you are ready.
- A shell (bash / zsh / fish — anything that can read a line from stdin).
- `jq` — used by every example below to read the JSON event payload from stdin. Optional; you can substitute any JSON parser.

### Step 1 — Locate the active profile yaml

See [`path-resolution.md`](path-resolution.md) for the full resolver contract — `$OMAKITEN_HOME` / XDG / OS defaults, `.active` resolution, `custom/` shadowing, and discovery fallthrough.

```bash
cat "$HOME/.config/omakiten/config/.active"   # prints the active basename (e.g. omakase.yaml)
okt config validate                            # runs the strict loader against the resolved path
```

Open the active file in your editor; you will be appending one block. If you want your edits to survive `mise run install` / kit refreshes, copy the file into `<config-dir>/custom/<name>.yaml` first.

### Step 2 — Add a hook block

Hooks live under `config.hooks` as a list. Each entry has three required fields (`on`, `do`, `args`) and one optional filter (`when`). For your first hook, append the block below to log every new comment to a sidecar file:

```yaml
config:
  # … your existing config …
  hooks:
    - on: comment
      do: exec
      args:
        argv: ["/home/me/scripts/log-comment.sh"]
        timeout_ms: 3000
```

Notes:

- `on:` must be one of the event types in `internal/domain/events.go::KnownEventTypes`. Typos fail at startup with a `validation_error` pointing at the bad value, so you cannot configure a hook that silently never fires.
- `argv` is taken **literally**. No shell expansion — write the absolute path of your script there, not `~/scripts/...`.
- `timeout_ms` is the deadline for your script. Default is `30000` (30 s); shorter is better for hot-path events.

### Step 3 — Write the script

Make the file executable and have it read one line from stdin (the event JSON). Example:

```bash
#!/usr/bin/env bash
set -euo pipefail

# The engine pipes the entire domain.Event to stdin as a single JSON line.
read -r event

# Pull the fields you care about. For comments, body lives at .body,
# the task id at .entity_id, and the project id at .project_id.
task_id=$(echo "$event" | jq -r '.entity_id')
body=$(echo "$event" | jq -r '.body')
ts=$(date -Iseconds)

mkdir -p ~/.local/share/omakiten/sidecar
printf '%s\ttask=%s\t%s\n' "$ts" "$task_id" "$body" \
  >> ~/.local/share/omakiten/sidecar/comments.log
```

Save as `/home/me/scripts/log-comment.sh`, then:

```bash
chmod +x /home/me/scripts/log-comment.sh
```

### Step 4 — Restart and verify

The hook bundle loads at startup. Restart whatever is running Omakiten (the TUI, the MCP server, an `okt` command in flight), then trigger the event:

```bash
okt comment add 1 -b "first hook test"
```

Check the sidecar:

```bash
tail -1 ~/.local/share/omakiten/sidecar/comments.log
# 2026-05-09T17:23:11+00:00  task=1  first hook test
```

If nothing landed, jump to [Troubleshooting](#troubleshooting).

### Step 5 — Confirm the engine saw the hook

Every dispatch emits a `hook.executed` row through the events store regardless of success. There is no MCP tool that lists raw events by `event_type`; query the SQLite log directly:

```bash
sqlite3 "$HOME/.local/share/omakiten/omakiten.db" \
  "SELECT created_at, payload FROM events WHERE event_type = 'hook.executed' ORDER BY id DESC LIMIT 5;"
```

(swap the DB path for `<project-root>/.omakiten/omakiten.db` when the project is repo-local). The payload tells you which hook fired (`hook_index`), which event triggered it (`target_event_id`), the configured action (`action`), the duration (`duration_ms`), and on failure the captured error (`error`). Use this to tell "the script ran but exited non-zero" apart from "the engine never reached the script".

### Step 6 — Filter events with `when:`

Most useful hooks only care about a slice of an event type. Add a `when:` block to restrict by top-level payload keys (AND across keys, string-equality match):

```yaml
config:
  hooks:
    - on: task.moved
      when:
        to: review            # only fire when a task lands in review
      do: exec
      args:
        argv: ["/home/me/scripts/notify-review.sh"]
```

JSON booleans (`"true"` / `"false"`) and numbers re-serialise through `encoding/json`, so `count: 3` matches a payload `{"count": 3}` without quoting.

If the filter rules out every hook for an event, no `hook.executed` row is emitted — that is by design (`hook.executed` records what happened, not what was tried).

### Step 7 — Production-ready patterns

The examples below cover the cases users hit first. Each builds on Steps 1–6.

#### Wire multiple hooks against the same event

The list under `config.hooks` is order-preserving but the engine spawns each match on its own goroutine — slow scripts do not block fast ones:

```yaml
config:
  hooks:
    - on: task.completed
      do: exec
      args:
        argv: ["/home/me/scripts/sync-to-linear.sh"]
        timeout_ms: 10000
    - on: task.completed
      do: exec
      args:
        argv: ["/home/me/scripts/post-to-slack.sh"]
        timeout_ms: 5000
```

#### Filter by source / agent_model

`when:` only sees the JSON payload, not the row columns. To branch on `source` (cli / mcp / tui) or `agent_model` (the model id MCP tools carry), read those off stdin in your script:

```bash
#!/usr/bin/env bash
read -r event
src=$(echo "$event" | jq -r '.source')
model=$(echo "$event" | jq -r '.agent_model')
[[ "$src" == "mcp" ]] || exit 0          # only act on agent activity
[[ "$model" == "claude-opus-4-7" ]] || exit 0
echo "$event" >> ~/agent-events.ndjson
```

#### Capture every blocked transition

Guard violations emit `guard.violated` with `operation` carrying the move name. Build an audit log of every blocked delete:

```yaml
config:
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      do: exec
      args:
        argv: ["/home/me/scripts/log-blocked-delete.sh"]
        timeout_ms: 3000
```

#### Disable a hook without deleting it

You can comment the YAML block out, or you can shut the channel gate with `config.events.overrides.<event_type>.hook: false`. The latter keeps the hook configured but stops dispatch — useful when you want to A/B a noisy hook without re-editing the array.

### Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `okt config show` rejects the bundle with `validation_error` and a path like `config.hooks[0].on` | Event type typo — must match a string in `internal/domain/events.go::KnownEventTypes` | Copy the event name verbatim from the catalog |
| Hook never fires; nothing in `hook.executed` | (a) Channel gate closed (`config.events.overrides.<event_type>.hook: false`, or inherited from `defaults.hook: false`); (b) `when:` rules out every event; (c) you forgot to restart after editing the YAML | Check the gate first via `okt config show`; `when:` accepts only top-level payload keys; restart the running runtime |
| `hook.executed.success = false`, `error: "exec /path/foo failed: …"` | Script exited non-zero or could not run | Run the script manually with the same JSON on stdin: `echo '{}' \| /path/foo` |
| `hook.executed.error: "exec … timed out after 3s"` | Script crossed `timeout_ms` | Lengthen the timeout, or move the slow part into a background job your script kicks off and returns from |
| Script gets `argv` placeholders verbatim instead of expansions | `argv` does not run a shell — `~/scripts/foo` is taken literally | Use absolute paths, or `argv: ["/bin/bash", "-c", "your shell expression"]` |
| Hook fires twice on a single action | Two YAML entries match the same event | Tighten `when:` on one of them, or remove the duplicate |

## Update when

- A new built-in action lands under `internal/hooks/actions/` — add its
  args contract to **Built-in actions**.
- `internal/domain/events.go::KnownEventTypes` gains or drops an event
  type — sync the `on:` subscriber list and event-payload shape.
- The hook action registry adds a registration hook or a new validation
  pass at composition-root time.

## See also

- [`mcp.md`](../mcp.md) — agent-facing tool surface. The `search` and
  `metrics.summary` tools are useful for hook payloads that enrich
  themselves with project state before dispatching.
- [`system.md § config.hooks`](system.md#confighooks) — wiring shape
  in the active profile yaml.
- [`notifications.md`](notifications.md) — notification cards a hook
  can dispatch via `notification: <slug>`.
- `internal/domain/events.go::KnownEventTypes` — canonical list of
  domain events a hook may subscribe to.

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
