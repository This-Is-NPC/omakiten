# Domain Events

Omakiten persists every state-changing operation as a row in the unified
`events` table. The same store backs three audiences:

- **Activity feed** — `entity_type='task'` rows render in the per-task
  activity column (TUI / MCP `tasks.continue`).
- **Logs view** — `event_type='cli.tool_call'` / `'mcp.tool_call'` /
  `'tui.tool_call'` rows are the per-call activity log written by
  `activity.Track`. The legacy `'operation'` value was renamed in
  migration 019 so hooks can subscribe per-source (`on: mcp.tool_call`)
  and filter on payload fields (`when: { tool_name: tasks.create }`)
  without reading SQL columns.
- **Domain audit / metrics** — every other event_type (task.*, comment,
  comment.*, tag.*, dependency.*, guard.*, error.*, solution.*) is what
  this guide catalogs.

The catalog is closed: `internal/domain/event.go::KnownEventTypes` is the
single source of truth and the config validator rejects overrides
referencing values outside it.

## Catalog

| Event type            | Entity row              | Trigger                                                       | Payload (minimum)                                              | Source typical    | Default log |
| --------------------- | ----------------------- | ------------------------------------------------------------- | -------------------------------------------------------------- | ----------------- | ----------- |
| `task.created`        | task                    | `Store.CreateTask`                                            | `{bucket}` (priority/title implicit on the task row)           | cli / mcp / tui   | true        |
| `task.moved`          | task                    | `Store.MoveTask` when bucket changes                          | `{from, to}`                                                   | cli / mcp / tui   | true        |
| `task.completed`      | task                    | `WorkflowService.MoveTask` into the final bucket              | `{bucket}`                                                     | cli / mcp / tui   | true        |
| `task.edited`         | task                    | `Store.EmitTaskEditedEvent` after `UpdateTask`                | `{title?,description?,priority?}` each as `{from,to}`; `priority.from`/`priority.to` are integer priority ids (resolve via `EnumRegistry` at the consumer) | cli / mcp / tui   | true        |
| `task.removed`        | system (project-scoped) | `Store.HardDeleteTask`                                        | `{project_id, task_id, title, bucket_key, state}`              | cli / mcp / tui   | true        |
| `task.archived`       | task                    | `Store.SetTaskState(archived)`                                | `{from_bucket, to_bucket, from_state, to_state}`               | cli / mcp / tui   | true        |
| `task.unarchived`     | task                    | `Store.SetTaskState(active)`                                  | `{from_bucket, to_bucket, from_state, to_state}`               | cli / mcp / tui   | true        |
| `comment`             | task                    | `Store.AddComment` (raw insert; this row IS the comment data) | comment body in `body` column; `Tags` via `event_tags`         | cli / mcp / tui   | true (data; gating not honored — see note) |
| `comment.edited`      | task                    | `Store.UpdateComment`                                         | `{comment_id, body:{from,to}?}`                                | cli / mcp / tui   | true        |
| `comment.removed`     | task                    | `Store.DeleteComment`                                         | `{comment_id, author_type, body}`                              | cli / mcp / tui   | true        |
| `tag.added`           | task / project / error  | `TagService.Add`                                              | `{entity_type, entity_id, tag_id, tag_name}`                   | cli / mcp / tui   | **false**   |
| `tag.removed`         | task / project / error  | `TagService.Remove`                                           | `{entity_type, entity_id, tag_id, tag_name}`                   | cli / mcp / tui   | **false**   |
| `dependency.added`    | task (the dependent)    | `DependencyService.Add`                                       | `{depends_on_task_id}`                                         | cli / mcp / tui   | true        |
| `dependency.removed`  | task (the dependent)    | `DependencyService.Remove`                                    | `{depends_on_task_id}`                                         | cli / mcp / tui   | true        |
| `guard.violated`      | task / comment          | Every `domain.ErrGuardViolation` return path (transitions, operation guards, permission denials) | `{operation, rule, hint, target, attempted_by}`               | cli / mcp / tui   | true        |
| `error.recorded`      | error                   | `ErrorService.Record`                                         | `{tags, has_context}`                                          | mcp (typical)     | true        |
| `error.searched`      | error (entity_id=0)     | `ErrorService.Search`                                         | `{query, tags, result_count}`                                  | mcp (typical)     | true        |
| `solution.added`      | solution                | `ErrorService.AddSolution`                                    | `{error_id}`                                                   | mcp (typical)     | true        |
| `solution.confirmed`  | solution                | `ErrorService.ConfirmSolution` (regardless of outcome)        | `{error_id, success, likes}`                                   | mcp (typical)     | true        |
| `solution.liked`      | solution                | `ConfirmSolution(success=true)`                               | `{error_id, likes}`                                            | mcp (typical)     | true        |
| `solution.failed`     | solution                | `ConfirmSolution(success=false)`                              | `{error_id, likes}`                                            | mcp (typical)     | true        |
| `solution.viewed_top` | solution (entity_id=0)  | `ErrorService.ListTopSolutions`                               | `{limit, returned_count}`                                      | mcp (typical)     | true        |
| `hook.executed`       | system (entity_id=0)    | `hooks.Engine` after `Action.Execute` returns                 | `{hook_index, action, event_type, target_event_id, success, error?, duration_ms}` | system            | true        |
| `bundle.imported`     | system (entity_id=0)    | `agentruntime.buildProjectRuntime` composes the payload and calls `Store.RecordEntityEvent` after `config.BuildSnapshot` materialises the per-project Snapshot | `{path, hash, workflow_key, workflow_count, persona_count, skill_count, law_count, template_count}` | system            | true        |
| `bundle.swapped`      | system (entity_id=0)    | `tui.reloadBundle` after a successful preset swap             | `{from_workflow, to_workflow, orphan_count, has_orphans, groups}` | tui               | true        |
| `confirmation.granted`| system (entity_id=0)    | `tui.Model.handleNotificationAction` before dispatching a non-empty action command | `{notification_slug, action_id, command}`                       | tui               | true        |
| `cli.tool_call`       | system (entity_id=0)    | `activity.Track` from any CLI invocation, recorded post-Finish | `{tool_name, source, entrypoint, status, duration_ms, error_message, args}` | cli               | true        |
| `mcp.tool_call`       | system (entity_id=0)    | `activity.Track` from any MCP `tools/call`, recorded post-Finish | `{tool_name, source, entrypoint, status, duration_ms, error_message, args}` | mcp               | true        |
| `tui.tool_call`       | system (entity_id=0)    | `activity.Track` from any TUI-driven service call, recorded post-Finish | `{tool_name, source, entrypoint, status, duration_ms, error_message, args}` | tui               | true        |

> **Note on `comment`:** the comment row IS user-visible data, not an
> audit trail. The log gate (`config.events.overrides.comment.log`)
> does NOT silence comment writes — `Store.AddComment` uses a raw
> INSERT. Setting it to `false` only affects downstream tooling that
> filters by event_type; the comment text still lands in the table.

## Schema policy

Every event row populates the canonical columns from
`internal/sqlite/migrations/*` (`events` table). Different event types
fill different subsets:

- `body` — comment text (only for `event_type='comment'`); empty for
  audit events.
- `payload` — JSON object (string column). Audit events use this.
- `author_type` — `human` | `agent`; populated for comments.
- `source`, `entrypoint`, `agent_model`, `agent_session_id` — set by
  `RecordEntityEvent` from the request context (`activity.WithAgent`).
- `operation`, `status`, `duration_ms` — only for the
  `<source>.tool_call` rows written by `activity.Track`. The same
  values are mirrored into `payload.tool_name` / `payload.status` /
  `payload.duration_ms` so hook `when:` filters work without reading
  SQL columns.

Payload contracts are documented above per event type. Today the runtime
treats payload as opaque JSON — no per-event schema validation. Consumers
are expected to honor the contracts; future work may add typed decoders.

### Payload contract: `guard.violated`

```json
{
  "operation":   "<task.transition | task.archive | task.delete | task.unarchive | task.edit | comment.edit | comment.delete>",
  "rule":        "<transition_not_allowed | permissions | blockers_in | comments_min | comments_tagged | (any user-defined guard.Type)>",
  "hint":        "<rendered guard message — same string returned in domain.ErrGuardViolation.Message>",
  "target":      { "task_id": 123, "from_bucket": "review", "to_bucket": "done" },
  "attempted_by": "user | agent"
}
```

`operation` and `rule` are intentionally free-form strings. The runtime
ships canonical values via `app.GuardOperation*` and `app.GuardRule*`
constants, but custom guards can supply any string and consumers filter
on whatever value lands in the payload.

### Payload contract: `<source>.tool_call`

```json
{
  "tool_name":     "tasks.create",
  "source":        "cli | mcp | tui",
  "entrypoint":    "tools/call",
  "status":        "running | ok | error",
  "duration_ms":   42,
  "error_message": "",
  "args":          { "title": "Hello" }
}
```

`tool_name` / `source` / `entrypoint` / `status` / `duration_ms` /
`error_message` mirror the discrete `events` columns of the same name —
metrics queries hit the indexed columns for speed, hooks read the
payload via `when:` filters. `args` carries the raw tool arguments
passed in (preserving caller key order). The event is emitted on the
bus only at `FinishActivityLog`, after status is final, so hooks can
match on the outcome:

```yaml
hooks:
  - on: mcp.tool_call
    when:
      tool_name: tasks.create
      status: ok
    do: exec
    args:
      argv: ["notify-send", "task created via MCP"]
```

## Naming convention

`<entity>.<action>` in past tense.

- Use the existing entity prefixes (`task`, `comment`, `tag`,
  `dependency`, `guard`, `error`, `solution`).
- Past tense reflects "this happened" — events are facts about things
  that already occurred. `task.create` (imperative) is wrong;
  `task.created` is right.
- Granularity belongs in the payload, not the name. `guard.violated`
  with `rule=blockers_in` is preferred over `guard.blockers_in`.

## Adding a new event

1. **Declare the constant** in `internal/domain/event.go` with a one-line
   godoc that names the entity, the trigger, and the minimum payload.
2. **Add it to `KnownEventTypes`** so config validation accepts overrides
   referencing it.
3. **Emit it** at the canonical mutation point. Service layer
   (`internal/app/*_service.go`) is preferred over the sqlite layer when
   the emission can be expressed without a transaction; co-locate with
   the persist call when atomicity matters.
4. **Document it** in the catalog table above (alphabetical within the
   family is fine).
5. **Add a smoke test** asserting the row lands. Existing patterns:
   `internal/sqlite/events_test.go` for transactional emits,
   `internal/app/workflow_service_test.go` for service-level emits.

## Configuration

`config.events` controls per-event-type behaviour. See
`defaults/config/omakase.yaml::config.events` for the canonical block. The
shape:

```yaml
config:
  events:
    default_recent_limit: 50    # Store.ListRecentEvents fallback
    defaults:                    # required; applied to every event type
      log: true
      broadcast: true            # reserved for the upcoming event-bus task
      hook: true                 # reserved for the upcoming event-bus task
    overrides:                   # optional; keys must be in KnownEventTypes
      tag.added:    { log: false }
      tag.removed:  { log: false }
```

Pointer-to-bool semantics in code mean "omitted = inherit". An override
that declares only `log: false` keeps inheriting the defaults' `broadcast`
and `hook`. The validator hard-rejects unknown keys so YAML typos do not
silently no-op.

All three channels are now consumed at runtime:

- `log` — gated inside `Store.RecordTaskEvent` / `RecordEntityEvent` /
  inline-tx `insertTaskEvent` callsites. When false, the row is dropped
  before insertion; the bus still receives a synthetic in-memory event
  so subscribers (hooks, future notifications) cannot be silenced by a
  log-only opt-out.
- `broadcast` — gated inside `events.Bus.Publish`. When false, the bus
  short-circuits the fan-out before walking subscribers.
- `hook` — gated inside `hooks.Engine.dispatch`. When false, the engine
  ignores the event entirely; no action runs and no `hook.executed` is
  emitted.

## Runtime broadcast

The composition root constructs one `events.Bus` per process and wires
it into the sqlite Store via `ApplyConfig{EventBus}`. Every emit path
in the Store fans the captured event out to the bus AFTER the
surrounding transaction commits, so subscribers never observe events
for rows that were rolled back.

Subscribers register through `Bus.Subscribe(filter, handler)`. Filter
is exact-match equality only:

- `EventTypes []string` — match if the event_type equals any entry.
  Empty slice matches every type.
- `PayloadEq map[string]string` — top-level keys in the JSON
  `domain.Event.Payload`. Strings, bools (`"true"` / `"false"`), and
  JSON numbers all coerce to string equality.

The two dimensions AND together. Handlers run synchronously on the
publisher's goroutine with a per-handler `recover` so a rogue panic
cannot derail the others or the publisher itself. Hooks are the only
subscriber that fans out to a fire-and-forget goroutine — they own
that decision inside `hooks.Engine`.

`Subscription.Unsubscribe()` is idempotent and thread-safe.

## Hooks engine

Authors declare hooks in the active profile yaml under `config.hooks`:

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

`on:` is matched against `event_type` (must be in
`domain.KnownEventTypes`; the validator rejects typos at LoadBundle).
`when:` is the same top-level payload-equality contract as `Filter.PayloadEq`.
`do:` is the action's registered name; `args:` is per-action and
documented in `.docs/hooks.md`.

Dispatch is asynchronous — the engine spawns one goroutine per matched
hook so a slow script never blocks the request that emitted the event.
The goroutine:

1. Drops any inherited request deadline so the action's own timeout
   takes effect (`exec` defaults to 30s).
2. Calls `Action.Execute(ctx, ev, args)`. Panics are recovered.
3. Emits `hook.executed` via `Store.RecordEntityEvent` with payload
   `{hook_index, action, event_type, target_event_id, success, error?, duration_ms}`.

`hook.executed` fires only when an action actually ran. Skipped hooks
(missing action, payload non-match, hook channel gated false) do not
emit it — the event records what happened, not what was tried.

Mutating `config.hooks` in the active profile yaml requires restarting the app for
changes to take effect; the bundle is read once at startup like every
other config block.
