# Tricks — trick palette reference

The trick palette is the global Ctrl+K overlay inside the TUI. Two tabs: **Tricks** for verb-prefixed shortcut commands (`verb:operand`) and **Search** for FTS5 fuzzy search. This page is the single source for the palette: feature reference, command catalog, configuration, hook recipes, troubleshooting.

## Contents

- [Overview](#overview)
- [How it works](#how-it-works)
- [Command catalog](#command-catalog)
- [Tricks tab reference](#tricks-tab-reference)
- [Search tab reference](#search-tab-reference)
- [Configuration](#configuration)
- [Hook recipe gallery](#hook-recipe-gallery)
- [Troubleshooting](#troubleshooting)
- [Related docs](#related-docs)

## Overview

The trick palette gives keyboard-first power users a single binding (`ctrl+k`) to reach every TUI screen, open any task or comment by id, and trigger any user-configured side-effect — without leaving the keyboard.

Every Tricks-tab submission emits a `trick.executed` event with payload `{verb, operand, raw}` *before* the built-in handler runs. User hooks subscribe to this event via the standard `hooks:` schema so arbitrary verbs become user-defined commands — palette is open by design.

The Search tab wraps `app.SearchService.Search` across the unified FTS5 index (tasks, comments, errors, solutions, plans, notes). Result list is navigable; Enter on a focused row opens the hit in its TUI detail view when one exists.

## How it works

1. Press `ctrl+k` from any non-modal TUI screen — overlay opens centred, focus lands on the **Tricks** tab.
2. Tricks tab: type a `verb:operand` command (e.g. `nav:31`, `op:381`, `hook:1`) and press `enter`.
3. Built-in handler dispatches `nav` (route jump) and `op` (entity open); user-defined verbs reach hooks via `when: {verb, operand}` filtering and run any configured `exec` / `notify` / etc. action.
4. Press `tab` from Tricks to switch to the **Search** tab. Type a query, `enter` to run.
5. Search results render as a navigable list. `up` / `down` / `pgup` / `pgdown` move the cursor; `enter` on a focused row opens the hit (task / comment).
6. `esc` closes the overlay and restores the prior screen state.

The palette is mutually exclusive with the notification overlay — when a notification fires while the palette is open, the palette is suppressed visually until the notification dismisses, then re-appears with its prior state (results, cursor, input text).

## Command catalog

Mirrors the [MCP tools catalog](../mcp.md#tools) format — one row per command, grouped by category.

### Built-in commands

| Command | Purpose |
|---|---|
| `nav:11` | Jump to Tasks · board lens. |
| `nav:12` | Jump to Tasks · table lens. |
| `nav:13` | Jump to Tasks · graph lens. |
| `nav:14` | Jump to Tasks · plans lens. |
| `nav:21` | Jump to Stats · general. |
| `nav:22` | Jump to Stats · logs. |
| `nav:31` | Jump to Settings · general. |
| `nav:32` | Jump to Settings · laws. |
| `nav:33` | Jump to Settings · personas. |
| `nav:34` | Jump to Settings · skills. |
| `nav:35` | Jump to Settings · templates. |
| `nav:36` | Jump to Settings · tags. |
| `op:<task-id>` | Open task detail view for `<task-id>` (positive integer). |

### Reserved verb names

| Verb | Why reserved |
|---|---|
| `nav` | Built-in screen-route dispatcher. User hooks filtering `when: {verb: nav}` are hard-rejected at config load — would silently lose to the built-in or rebind expected navigation. |
| `op` | Built-in entity-open dispatcher. Same shadowing rule as `nav`. |

### User-defined commands (examples)

Any verb that matches `^[a-z][a-z0-9_-]*$` and is not in the reserved set above is user-defined. The dispatcher emits `trick.executed` and stops — `hooks:` entries handle the side effect.

| Example | Pattern in `omakiten.yaml` |
|---|---|
| `hook:1` | Run a one-shot script (see [Daily standup](#1-daily-standup--hook1)). |
| `cmd:deploy` | Show a confirmation card and run on accept (see [Deploy with confirmation](#2-deploy-with-confirmation--cmddeploy)). |
| `env:prod` · `env:staging` · `env:dev` | Switch environment context (see [Environment switcher](#3-environment-switcher--envname)). |
| `kata:morning` | Trigger a multi-action fan-out (see [Multi-action fan-out](#4-multi-action-fan-out--kataname)). |

## Tricks tab reference

### Verb grammar

The parser accepts a `verb:operand` token with exactly one `:` separator. Both sides are trimmed of surrounding whitespace before validation.

- Verb matches `^[a-z][a-z0-9_-]*$` — lowercase ASCII, starts with letter, may contain digits / underscore / hyphen.
- Operand is any non-empty string after trim. Per-verb constraints live in the handler.

### Parse error matrix

Each error produces an inline status; no event emits until the input parses cleanly.

| Sentinel | Inline status | Example input |
|---|---|---|
| `ErrMissingColon` | `missing : separator` | `nav31` |
| `ErrTooManyColons` | `too many : separators` | `nav:31:foo` |
| `ErrEmptyVerb` | `verb is empty` | `:31` |
| `ErrEmptyOperand` | `operand is empty` | `nav:` |
| `ErrInvalidVerb` | `verb must match [a-z][a-z0-9_-]*` | `Nav:31`, `1nav:31`, `na v:31` |

### Built-in `nav` handler

Resolves the operand against the screen registry (positional defaults + `tricks.nav` overrides). On hit: closes the overlay and jumps to the matched `(zone, sub)`. On miss: inline status `no screen for nav code "<code>"`, palette stays open.

### Built-in `op` handler

Parses the operand as a positive integer task id. On hit: closes the overlay and opens the task detail view via `openTaskView`. On miss (non-numeric or unknown id): inline status, palette stays open.

The reserved type-prefix grammar (`op:t<id>`, `op:c<id>`, `op:p<id>`, etc., aligned with `domain.SearchEntityType`) is parser-compatible but not yet implemented — bare digits = task id at MVP.

### User-defined verb dispatch

The dispatcher emits `trick.executed` with payload `{verb, operand, raw}` and closes the overlay. Built-in does nothing else. Hooks subscribed to the event handle the rest — see [Hook recipe gallery](#hook-recipe-gallery).

## Search tab reference

### Query flow

1. Type a query into the Search input.
2. Press `enter` — the palette dispatches `app.SearchService.Search` asynchronously (off the UI goroutine via `tea.Cmd`); inline status shows `searching "<query>"…` while the goroutine runs.
3. Results arrive as a navigable list rendered under the input. Empty result set surfaces `no results for "<query>"`.

### Result-list keybindings

| Key | Action |
|---|---|
| `↑` · `k` | Move cursor up one row (clamped at first row). |
| `↓` · `j` | Move cursor down one row (clamped at last row). |
| `pgup` | Move cursor up `resultListPageStep` (5) rows. |
| `pgdown` | Move cursor down `resultListPageStep` (5) rows. |
| `enter` | Open the focused hit via the dispatch table below (when openable); otherwise show an inline hint. |
| `tab` | Toggle back to the Tricks tab (preserves result list — survives the round-trip). |
| `esc` | Close the overlay in one keystroke (one-shot dismiss). |

### Open dispatch table

Locked per #319 D1 — open only entity types that have a TUI detail screen today. Others are inert with an inline hint.

| Entity type | TUI detail screen | Open behaviour |
|---|---|---|
| `task` | `openTaskView` | Opens task detail view. Palette closes on success. |
| `comment` | comment detail screen | Opens parent task view + scrolls activity cursor to the comment + opens detail. Palette closes on success. |
| `plan` | none (subPlans is a list, no per-plan detail open-by-id) | Inline hint `plan: no TUI view`. Palette stays open. |
| `error` | none | Inline hint `error: no TUI view`. Palette stays open. |
| `solution` | none | Inline hint `solution: no TUI view`. Palette stays open. |
| `context` | none | Inline hint `context: no TUI view`. Palette stays open. |

### Snippet sanitisation

Each rendered row passes the snippet through `cleanSnippet`:

- Strips FTS5 `<mark>…</mark>` highlight tags.
- Collapses embedded `\n` / `\r` into spaces so each hit fits one visual row.
- Strips ANSI escape sequences via `ansi.Strip` (terminal-injection guard for snippets sourced from user-controllable text).
- Truncates to `resultListMaxWidth` (44 cells, sized to fit the parent's 48-cell panel border) with a `…` ellipsis indicator.

## Configuration

The palette ships fully usable with defaults. The optional `tricks:` block in `omakiten.yaml` carries per-screen `nav:` overrides.

### Schema

```yaml
tricks:
  nav:
    # Override the positional default for a code → route.
    "33": settings.tags
    "34": tasks.plans
```

`tricks.nav` is the only field today. Keys are positional 2-digit codes (`^[1-9][1-9]$`); values are route slugs from the catalog below. The validator rejects malformed codes (`"0a"`, `"123"`, etc.) at load time so a typo never surfaces as a runtime miss.

### Route slug catalog

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

### Default positional layout

Defaults ship as `1x` / `2x` / `3x` grouped by top-level zone (matches the canonical menu cycle in `internal/tui/state.go::subsByTop`):

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

### Reserved-verb enforcement

The validator hard-rejects any `hooks[i].when.verb` entry that equals `nav` or `op`. The load-time error fails fast with the reserved list quoted in the message:

```yaml
hooks:
  - on: trick.executed
    when:
      verb: nav        # ← rejected at load time
      operand: "11"
    do: notify
```

```text
config.hooks[0].when.verb: "nav" is reserved by the trick palette built-in handler;
pick a different verb (reserved: [nav op])
```

### Event payload

```json
{
  "verb": "hook",
  "operand": "1",
  "raw": "hook:1"
}
```

`raw` preserves the user's exact input (post-trim) so hook actions can echo it back without re-rendering. `EntityType` on the event is `system`.

## Hook recipe gallery

### 1. Daily standup — `hook:1`

One-shot script trigger. Press `ctrl+k`, type `hook:1`, enter — runs a local script.

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

### 2. Deploy with confirmation — `cmd:deploy`

Notify card → user confirms via action button → exec runs.

```yaml
hooks:
  - on: trick.executed
    when:
      verb: cmd
      operand: deploy
    do: notification
    args:
      notification: deploy-confirm   # see configuration-guide/notifications.md
```

The `deploy-confirm` notification YAML carries an `actions[]` entry whose `command` invokes `./scripts/deploy.sh` — see [notifications.md § actions](notifications.md) for the action-button binding.

### 3. Environment switcher — `env:<name>`

Three commands sharing one verb pattern, dispatched by operand value.

```yaml
hooks:
  - on: trick.executed
    when:
      verb: env
      operand: prod
    do: exec
    args:
      cmd: ["./scripts/switch-env.sh", "prod"]
  - on: trick.executed
    when:
      verb: env
      operand: staging
    do: exec
    args:
      cmd: ["./scripts/switch-env.sh", "staging"]
  - on: trick.executed
    when:
      verb: env
      operand: dev
    do: exec
    args:
      cmd: ["./scripts/switch-env.sh", "dev"]
```

### 4. Multi-action fan-out — `kata:<name>`

One trick triggers exec + notify + (any third action) in parallel.

```yaml
hooks:
  - on: trick.executed
    when:
      verb: kata
      operand: morning
    do: exec
    args:
      cmd: ["./scripts/sync-inbox.sh"]
  - on: trick.executed
    when:
      verb: kata
      operand: morning
    do: notify
    args:
      title: "Morning kata"
      body: "Inbox sync started"
```

Engine dispatches per match independently (see `internal/hooks/engine.go::dispatch`). Both arms fire; ordering is not guaranteed.

### 5. Conditional trigger — combined `when:` filters

Hook fires only when the trick matches AND the payload also contains the second filter. Useful when the trick payload is enriched by an upstream hook or when filtering on event metadata other than verb/operand.

```yaml
hooks:
  - on: trick.executed
    when:
      verb: hook
      operand: "1"
      # any additional payload key the publisher includes is matchable
    do: exec
    args:
      cmd: ["./scripts/scoped-action.sh"]
```

`when:` is an AND join — every declared key must match the event payload. Add or remove keys to tighten or relax the trigger scope.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `ctrl+k` does nothing | A modal input owns the keyboard (comment textarea, move input, task screen, entity screen, help). | Press `esc` to close the modal, then retry `ctrl+k`. |
| Tricks tab rejects every input | Verb grammar violation (uppercase, leading digit, missing colon, embedded space). | See the [parse error matrix](#parse-error-matrix). Lowercase the verb, single `:` separator, both sides non-empty. |
| `nav:<code>` hint says `no screen for nav code` | Code is not in the default layout AND not in `tricks.nav` overrides. | See the [default positional layout](#default-positional-layout). Add an override under `tricks.nav` if you want a custom binding. |
| Search result hint says `<type>: no TUI view` | Entity type has no TUI detail screen today (plan, error, solution, context). | Use the CLI or MCP to inspect those entity types; palette open-dispatch is limited to `task` + `comment` per #319 D1. |
| User hook does not fire | Most common: hook entry filters on a reserved verb (`nav` / `op`) and was hard-rejected at config load. | Check `omakiten.yaml` load output — the validator quotes the reserved verb in its error message. Rename the verb. |
| Hook fires but action errors silently | `do: exec` action returns non-zero; the engine logs `hook.executed` with `success: false` and the error message. | Inspect the events feed (Stats · logs) for the most recent `hook.executed` row — the payload carries `error`, `duration_ms`, `action`, `event_type`. |
| Notification covers palette result list | Working as intended — notification and palette are mutually exclusive overlays. | Dismiss the notification (`esc` / `enter` per its action keys) to restore the palette panel with prior state. |

## Related docs

- [tui.md](../tui.md) — TUI surface reference; the palette section there points back here.
- [hooks.md](hooks.md) — full `hooks:` schema, action contracts (`exec`, `noop`, `notification`), and `${{intl:KEY}}` interpolation.
- [notifications.md](notifications.md) — notification YAML format, action buttons, dismiss modes.
- [path-resolution.md](path-resolution.md) — where `omakiten.yaml` lives across project / user / installer scopes.
