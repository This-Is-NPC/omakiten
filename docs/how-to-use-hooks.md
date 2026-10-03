# How to run an action when work changes

**The question:** can completing work run a command, can a failed guard
show a notification with the next action, or can a CI job tell Omakiten
that a build failed?

Hooks belong to the active preset under `config.hooks`. An event selects the
hook; an action handles it. The runtime dispatches actions asynchronously.

## 1. Choose the event and action

This settings fragment runs a literal executable for a task move:

```yaml
hooks:
  - on: task.moved
    do: exec
    args:
      argv: [notify-send, Omakiten, Task moved]
      timeout_ms: 5000
```

`argv` is an argument array, rather than a shell command string. The program
receives the complete event as JSON on stdin. If you need a shell, name it
explicitly in the array. An omitted timeout uses 30 seconds. Program output
is captured; a failed exit or timeout is recorded as a hook failure.

Use executable paths that are meaningful on the user's machine. A script in
a package is captured with the package, but its arguments do not automatically
become relative to that package directory.

## 2. Filter the event

Add `when` to match top-level event payload fields:

```yaml
hooks:
  - on: guard.violated
    when: {operation: task.transition, rule: wave_gate}
    do: noop
```

All conditions must match. Use the payload seen in `okt logs` to choose the
keys and values. Hooks are project-scoped; another project's event does not
run this project's action. Intentionally global events have explicit scope.

## 3. Show a notification

```yaml
hooks:
  - on: guard.violated
    notification: kitten_blocked
    message: "${{intl:notifications.omakase.guard_task_transition.message}}"
    detail_message_field: hint
```

The notification key resolves a file in the package's `notifications/` folder.
Start from one of its complete cards when customizing geometry, placement,
animation, colors, dismiss keys, or action buttons. A card's action contains an
operation name and structured arguments; the TUI injects the project scope
and dispatches through its operation port.

`${{intl:KEY}}` uses Omakiten's bundled translations. A literal message works
too. `detail_message_field` selects an event payload field for the detail text.
The TUI shows the notification when the hook runs inside it. Elsewhere, as in
an agent's CLI call or the daemon, the rendered notification is recorded as a
`notification.shown` event: an open TUI shows it, and any client following the
event stream can too. A notification cannot fire on `notification.shown`
itself.

## 4. React to an outside event

A script, a CI job, or another app can tell Omakiten something happened. The
preset declares each event it accepts under `config.events.definitions`, named
`external.<name>` in the `external` category:

```yaml
events:
  definitions:
    external.ci_failed:
      category: external
      display: "CI failed"
      entity_type: project
      formatter: external
hooks:
  - on: external.ci_failed
    notification: kitten_blocked
    message: "CI failed"
    detail_message_field: branch
```

The caller emits it with the CLI or the local API:

```bash
okt --project example emit ci_failed --field branch=main --field url=https://ci.example/1
curl -X POST -H "Authorization: Bearer $token" \
  -d '{"name":"ci_failed","payload":{"branch":"main"}}' \
  "$url/api/v1/projects/example/events"
```

The fields are a flat map of strings: at most 32, each named in lower snake
case, each value up to 4 KiB. A name the preset does not declare is refused,
and a project takes at most 60 outside events a minute. The event's hooks run
in the process that recorded it, `when` filters its fields, and
`okt logs --category external` lists them.

## 5. Read what happened

```bash
okt --project example logs --help
okt --project example logs
```

Look for the triggering event and `hook.executed`. A matching admitted action
records its result; a hook that did not match has no execution record. Check
the event policy's logging, broadcast, and hook gates if the action never ran.

A reload stops admission and cancels active work. A process that exits, a
one-shot CLI call included, stops admission and lets the admitted actions
finish for up to five seconds, then cancels what is left. Hooks are not a
persistent background scheduler. Keep actions short and observe their
recorded result.

## What installation checks

Package installation checks paths, file integrity, configuration, and
references. It does not inspect script code or run hooks to approve them.
The user chooses the preset and the programs it is allowed to invoke.
