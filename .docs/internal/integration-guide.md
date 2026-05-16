# Integration Guide — wiring hooks into your workflow

This guide walks you from "I have Omakiten installed" to "a script of mine runs every time event X happens" without writing Go. Hooks subscribe to Omakiten's domain events and dispatch a configured action; the canonical action ships in the kit (`exec`) so anything you can run in a shell can react to a task move, a comment, a guard violation, or any other event in [`domain-events.md`](../domain-events.md).

If you need the schema reference, the full action contract, or the list of channel gates, see [`hooks.md`](../hooks.md). This document is the hands-on path; it points back at the reference for the details you do not need to read first.

---

## Prerequisites

- Omakiten installed and at least one project registered. If `okt --version` works and `okt project list` shows your project, you are ready. New install? Follow the [README quickstart](../../README.md#install).
- A shell (bash / zsh / fish — anything that can read a line from stdin).
- `jq` — used by every example below to read the JSON event payload from stdin. Optional; you can substitute any JSON parser.

---

## Step 1 — Locate the active profile yaml

See [`reference/path-resolution.md`](../reference/path-resolution.md) for the full resolver contract — `$OMAKITEN_HOME` / XDG / OS defaults, `.active` resolution, `custom/` shadowing, and discovery fallthrough.

```bash
cat "$HOME/.config/omakiten/config/.active"   # prints the active basename (e.g. omakase.yaml)
okt config validate                            # runs the strict loader against the resolved path
```

Open the active file in your editor; you will be appending one block. If you want your edits to survive `mise run install` / kit refreshes, copy the file into `<config-dir>/custom/<name>.yaml` first.

---

## Step 2 — Add a hook block

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

- `on:` must be one of the event types in [`domain-events.md`](../domain-events.md). Typos fail at startup with a `validation_error` pointing at the bad value, so you cannot configure a hook that silently never fires.
- `argv` is taken **literally**. No shell expansion — write the absolute path of your script there, not `~/scripts/...`.
- `timeout_ms` is the deadline for your script. Default is `30000` (30 s); shorter is better for hot-path events.

---

## Step 3 — Write the script

Make the file executable and have it read one line from stdin (the event JSON). Example:

```bash
#!/usr/bin/env bash
set -euo pipefail

# The engine pipes the entire domain.Event to stdin as a single JSON line.
read -r event

# Pull the fields you care about. The shape is documented in
# `domain-events.md` — for comments, body lives at .body, the task id at
# .entity_id, and the project id at .project_id.
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

---

## Step 4 — Restart and verify

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

---

## Step 5 — Confirm the engine saw the hook

Every dispatch emits a `hook.executed` row through the events store regardless of success. Inspect the recent ones:

```bash
okt mcp call events.list_recent --input '{"event_type":"hook.executed","limit":5,"_agent_model":"local"}'
```

The payload tells you which hook fired (`hook_index`), which event triggered it (`target_event_id`), the configured action (`action`), the duration (`duration_ms`), and on failure the captured error (`error`). Use this to tell "the script ran but exited non-zero" apart from "the engine never reached the script".

---

## Step 6 — Filter events with `when:`

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

---

## Step 7 — Production-ready patterns

The examples below cover the cases users hit first. Each builds on Steps 1–6.

### Wire multiple hooks against the same event

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

### Filter by source / agent_model

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

### Capture every blocked transition

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

The recipe in [`hooks.md` § Recipes](hooks.md#log-every-blocked-delete) shows the matching `jq` extraction.

### Disable a hook without deleting it

You can comment the YAML block out, or you can shut the channel gate at `config.events.channels.<event_type>.hook = false`. The latter keeps the hook configured but stops dispatch — useful when you want to A/B a noisy hook without re-editing the array.

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `okt config show` rejects the bundle with `validation_error` and a path like `config.hooks[0].on` | Event type typo — must match a string in [`domain-events.md`](../domain-events.md) | Copy the event name verbatim from the catalog |
| Hook never fires; nothing in `hook.executed` | (a) Channel gate closed (`config.events.channels.<event_type>.hook` is `false`); (b) `when:` rules out every event; (c) you forgot to restart after editing the YAML | Check the gate first via `okt config show`; `when:` accepts only top-level payload keys; restart the running runtime |
| `hook.executed.success = false`, `error: "exec /path/foo failed: …"` | Script exited non-zero or could not run | Run the script manually with the same JSON on stdin: `echo '{}' \| /path/foo` |
| `hook.executed.error: "exec … timed out after 3s"` | Script crossed `timeout_ms` | Lengthen the timeout, or move the slow part into a background job your script kicks off and returns from |
| Script gets `argv` placeholders verbatim instead of expansions | `argv` does not run a shell — `~/scripts/foo` is taken literally | Use absolute paths, or `argv: ["/bin/bash", "-c", "your shell expression"]` |
| Hook fires twice on a single action | Two YAML entries match the same event | Tighten `when:` on one of them, or remove the duplicate |

---

## Beyond `exec` — adding a custom action

`exec` covers most integrations because the script can do anything. When you genuinely need a Go-native action — for example, talking to an in-process service that does not have a CLI — you implement `hooks.Action` and register it. The full how-to lives in [`hooks.md` § Adding a new action](hooks.md#adding-a-new-action). One file to write, one line in `RegisterBuiltins` to wire, and a table entry in `hooks.md` to document the args contract.

---

## Where to read next

- [`hooks.md`](../hooks.md) — schema reference, channel-gate semantics, action contract, and the dispatch lifecycle.
- [`domain-events.md`](../domain-events.md) — full event catalog with payload shapes you can match on.
- [`configuration-guide.md`](../configuration-guide.md) — the surrounding YAML blocks (`config.events.channels`, `config.activity_log`, etc.) that interact with hook dispatch and persistence.
