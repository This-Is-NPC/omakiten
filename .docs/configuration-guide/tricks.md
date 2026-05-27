# Tricks — TUI trick palette configuration

The trick palette (`ctrl+k` overlay — see [tui.md](../tui.md#trick-palette)) is fully usable with defaults; this page covers the optional `tricks:` block in `omakiten.yaml` and the reserved-verb rules user-defined hooks must respect.

## Schema

```yaml
tricks:
  nav:
    # Override the positional default for a code → route.
    "33": settings.tags
    "34": tasks.plans
```

`tricks.nav` is the only field today. Keys are positional 2-digit codes (`^[1-9][1-9]$`); values are route slugs from the table below. The validator rejects malformed codes (`"0a"`, `"123"`, etc.) at load time so a typo never surfaces as a runtime miss.

## Route slugs

| Slug | Destination |
|---|---|
| `tasks.board` | Tasks · board lens |
| `tasks.table` | Tasks · table lens |
| `tasks.graph` | Tasks · graph lens |
| `tasks.plans` | Tasks · plans lens |
| `stats.general` | Stats · general |
| `stats.logs` | Stats · logs |
| `settings.general` | Settings · general |
| `settings.laws` | Settings · laws |
| `settings.personas` | Settings · personas |
| `settings.skills` | Settings · skills |
| `settings.templates` | Settings · templates |
| `settings.tags` | Settings · tags |

Unknown slugs in an override are non-fatal: the palette downgrades them to a warning and falls back to the positional default. Adding a new route is a code change in `internal/tui/palette/registry.go` (`validRoutes` + `DefaultScreens`) and `internal/tui/palette_dispatch.go` (`routeBindings`).

## Default positional layout

Defaults ship as `1x`/`2x`/`3x` grouped by top-level zone (matches the canonical menu cycle in `internal/tui/state.go::subsByTop`):

| Code | Slug |
|---|---|
| `11` | `tasks.board` |
| `12` | `tasks.table` |
| `13` | `tasks.graph` |
| `14` | `tasks.plans` |
| `21` | `stats.general` |
| `22` | `stats.logs` |
| `31` | `settings.general` |
| `32` | `settings.laws` |
| `33` | `settings.personas` |
| `34` | `settings.skills` |
| `35` | `settings.templates` |
| `36` | `settings.tags` |

The 2-digit cap leaves 81 slots; adding a sub appends the next unused code under its parent zone — no renumbering. The reload path (`internal/tui/reload_bundle.go`) rotates the registry on every bundle hot-reload, so `tricks.nav` edits land on the next `ctrl+k` open without restarting the TUI.

## Reserved verbs

The built-in palette handler owns two verbs:

| Verb | Handler |
|---|---|
| `nav` | resolves the operand against the screen registry and jumps to the matched `(zone, sub)` |
| `op` | parses the operand as a task id and opens the task detail view |

The config validator hard-rejects any `hooks[i].when.verb` entry that equals `nav` or `op`. A user hook filtering on these verbs would either silently lose to the built-in dispatch or rebind expected behaviour — both options break user expectation, so the load-time error fails fast with the reserved list quoted in the message.

```yaml
hooks:
  - on: trick.executed
    when:
      verb: nav        # ← rejected at load time
      operand: "11"
    do: notify
```

```
config.hooks[0].when.verb: "nav" is reserved by the trick palette built-in handler;
pick a different verb (reserved: [nav op])
```

## User-defined verb hooks

Any verb outside `nav` / `op` is user-defined. The palette parser accepts any verb that matches `^[a-z][a-z0-9_-]*$`; the dispatcher emits `trick.executed` with payload `{verb, operand, raw}` and stops — no built-in side-effect runs. Hooks subscribed to the event handle the rest.

### Exec example — `hook:1` runs a script

```yaml
hooks:
  - on: trick.executed
    when:
      verb: hook
      operand: "1"
    do: exec
    args:
      cmd: ["/home/howl/bin/standup.sh"]
```

### Notify example — `cmd:deploy` shows a confirmation card

```yaml
hooks:
  - on: trick.executed
    when:
      verb: cmd
      operand: deploy
    do: notify
    args:
      title: "Deploy"
      body: "Confirm to proceed"
```

### Fan-out — multiple hooks on the same trick

```yaml
hooks:
  - on: trick.executed
    when:
      verb: hook
      operand: "1"
    do: exec
    args:
      cmd: ["./scripts/deploy.sh"]
  - on: trick.executed
    when:
      verb: hook
      operand: "1"
    do: notify
    args:
      title: "Deploy"
      body: "Started"
```

Both fire — the engine dispatches per match independently (see `internal/hooks/engine.go::dispatch`).

## Operand grammar

The parser is verb-agnostic about the operand: any non-empty string passes. Per-verb constraints live in the handler:

- `nav:<code>` — handler matches `<code>` against the registry's positional grammar (`^[1-9][1-9]$` + any user override key). Unknown codes surface inline; the overlay stays open.
- `op:<id>` — handler requires `<id>` to be a positive integer (task id). MVP only opens tasks; the reserved type-prefix grammar (`op:t<id>`, `op:c<id>`, etc., aligned with `domain.SearchEntityType`) will land when a second entity type opens up.
- *(user verbs)* — entirely up to the hook's `when:` filter. Examples above use string operands; numeric is fine; both round-trip through the `trick.executed` payload verbatim.

## Event payload

```json
{
  "verb": "hook",
  "operand": "1",
  "raw": "hook:1"
}
```

`raw` preserves the user's exact input (post-trim) so hook actions can echo it back without re-rendering. `EntityType` on the event is `system`.
