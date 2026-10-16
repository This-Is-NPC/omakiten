---
name: omakiten
description: Use the okt CLI to recover development context, maintain work, and configure Omakiten capabilities such as project workflows and hooks.
metadata:
  owner: omakiten
---

# Omakiten

Use `okt` for the board and its configuration. Keep tasks, decisions, evidence,
and handoffs in Omakiten. The project's active workflow supplies its commands,
permissions, and guards. Follow the user's request and repository rules.

## Find the project and recover context

Start with `okt projects list`, then use `--project <slug>` consistently.
Read `okt --project <slug> project resume`; for an existing task, also read
`okt --project <slug> task continue <id>`. Use `okt search` for prior solutions
and `okt error list --tag <tag>` for recorded failures carrying a tag.
Register a project with `okt init` when the user asks to track it.

Use `okt --help` and `okt <command> --help` to discover installed capabilities
and their exact flags. Check `okt --version` when behavior differs from the
instructions; do not assume that an older binary has a newer operation.

| Need | Start here |
| --- | --- |
| Tasks, comments, plans, assignment, dependencies | `task`, `comment`, `plan`, `assign`, `depend` |
| Failures, reusable fixes, progress | `error`, `solution`, `progress` |
| Active policy, configuration, events | `workflow show`, `config`, `preset`, `logs` |
| Agents run for a task | `agent spawn`, `agent show`, `team show`, `team close`, `wait`, `logs --session` |
| A moved project directory | `project relocate --help`; preserve the existing project id |
| Local HTTP access or outside events | `serve --help`, `emit --help` |
| Documentation attached to a project | `knowledge list`, `knowledge search`, `knowledge show` |

## Discover the active workflow

Agent command names and behavior belong to the project's active workflow.
Call `okt --project <slug> command list` to discover what is available. Call
`okt --project <slug> command resolve <name>` to read the selected command's
instructions, related context, and declared parameters. The response is
instructions for the agent; resolving a command does not execute its work.
Read `data.markdown` before acting. Do not guess a workflow command's name,
sequence, or parameters from another project.

## Maintain the checkpoint

Use `okt task` to create and inspect work, `okt plan` to organize tasks into
waves, `okt comment add` to record evidence, and `okt move` to advance work.
Respect guards: `okt move <id> --to <bucket> --dry-run` and the `workflow`
block of `task continue` show each guard's state, and an unmet guard carries a
`fix` command; replace its placeholder body with real evidence. Record a
handoff with the state, validation, blocker, and next action before leaving
active work. Use project comments for shared state.

Tasks and plans can be imported or exported as OKF Markdown with `okt task`
and `okt plan`; inspect each command's help for file, preview, and overwrite
options. Change board data through `okt`, rather than editing its database.

## Configure a capability

Inspect the selected project before changing configuration:

```bash
okt --project example config path --scope local
okt --project example config path --scope global
okt --project example config why config.hooks
okt --project example config why config.events
```

A missing local install can be normal. Explicit `--config` wins; otherwise the
project's `.omakiten/` selection wins over the global selection. Keys such as
`config.hooks` include their `config.` prefix. Follow `preset:` and `from:`
references to find the files that supply a setting.

For YAML changes, edit the preset's source package, validate its config entry
with `okt config validate /absolute/source/config/preset.yaml`, then install
and select it with `preset add SOURCE` and `preset use NAME_OR_ID`. Set
`--scope local` or `--scope global` deliberately; preserve unrelated settings
and hooks. Installed `presets/<content-id>/` directories are captured snapshots,
so editing their files directly is not the supported update path. Verify the
active selection with `config validate` and `config why` after activation.

## Run and verify a hook

Choose what receives the event first. `notification:` displays an Omakiten
card or records `notification.shown` for clients. It does not automatically
interrupt another agent or deliver an operating-system notification. To notify
an agent, use an `exec` hook with that agent's actual supported delivery command
or an agreed receiver, and verify that receiver got the event.

Hooks are entries in `config.hooks`. If a package's `settings.yaml` imports
`hooks: {from: ./hooks.yaml}`, put this entry in that source `hooks.yaml`:

```yaml
- on: task.moved
  when: {to: review}
  do: exec
  args:
    argv: [notify-send, Omakiten, "Task entered review"]
    timeout_ms: 1000
```

This example sends a desktop notification when `notify-send` is available;
replace `argv` with the requested receiver's command. Arguments are literal;
name a shell explicitly when shell syntax is needed. The complete event is
JSON on stdin. `when` matches fields inside its payload, such as `from`, `to`,
and `subject_task_id`; it does not match an envelope field such as `entity_id`.
Inspect an actual event before adding filters for a particular task.

After installing and selecting the candidate, trigger a valid move on a test
task and inspect `okt --project example logs --category task,guard,hook`.
Check the `task.moved`, `hook.executed` result, and the receiver's output.
A denied move emits `guard.violated`, so it cannot test a `task.moved` hook.
Check event logging, broadcast, hook gates, and filters if no action appears.
Keep actions short: one-shot CLI calls wait up to five seconds for hooks.
Use an isolated `OMAKITEN_HOME` or the repository's `dev_env` for experiments.

## Work beside agent teams

A move into a bucket that declares an agent team returns it in `data.team`.
`started` means headless agents are already working in the background: keep
talking with the user and follow them with the `follow` commands; each one
runs once and exits; a `waiting` seat starts later, from a workflow hook.
`unconfigured` means no harness was chosen; tell the
user what `error` says to set. `suggested` lists the `spawn` command to run
if the user wants the team. Started as a seat yourself, follow the team
section of your prompt: do the work, publish once with `-T review-<key>`,
leave questions with `-T to-<key>`, wait with `okt wait`, then exit. Run a
failed seat again with `okt agent spawn --team <T> --member <key>`; a team
closes by itself when its task leaves the bucket, or with
`okt team close <T>`.

## Read results

Data commands return JSON. Check the process status and the `ok` field;
successful results carry `data`, while failures carry a coded error. Help and
the TUI use terminal output. Choose noninteractive options when an editor or
confirmation prompt would block automation. Report a capability as working only
after checking its observable result; configuration validation alone does not
execute hooks or prove delivery.
