# How to open agents for a task

**The question:** how do I have Omakiten open coding agents in terminal panes
for a task, choose which harness and model each one runs, and see what they
are doing?

Prerequisites: a registered project, a task, a terminal multiplexer Omakiten
knows (`tmux` or `herdr`), and a harness (`claude` or `opencode`) on `PATH`.
Nothing here runs unless a bucket declares a team or you call
`okt agent spawn`.

## 1. Open one agent

```bash
okt --project example agent spawn --task 42 --command okt-task-review
```

The lead agent opens in a new pane at the project root. Its prompt is the
resolved command, the task, and its seat in the team. The result names the
team, the session, the pane, and the commands to follow it:

```bash
okt --project example agent show s-1a2b3c4d5e6f
okt --project example team show t-1a2b3c4d5e6f
okt --project example logs --session s-1a2b3c4d5e6f
okt --project example task continue 42
```

`logs --session` lists everything the agent's own `okt` calls recorded.

`--harness`, `--model`, and `--launcher` override the adapters for this spawn.
Spawning again for the same task and bucket returns the running team instead
of opening a second one.

## 2. Declare a team for a bucket

In the workflow, a bucket can name a lead and members:

```yaml
buckets:
  - id: 3
    key: review
    name: Review
    position: 3
    agents:
      trigger: manual
      lead: {command: okt-task-review}
      members:
        - {key: security, command: okt-task-review, harness: opencode, brief: "security only"}
```

`trigger: auto` opens the team when a task moves into the bucket, after the
move commits; the move's result carries it in `data.team`, with the sessions,
panes, and commands to follow them. The agent that moved the task keeps its
conversation; it reads progress when it wants. A team that cannot open never
undoes the move: `data.team.status` is `failed` and the error says why.
`okt move --no-spawn`, and `--no-spawn` on `progress record`, report the team
as `skipped` instead.

When the task leaves the bucket, its team there closes: the team becomes
`closed`, running seats become `stopped`, and each pane receives
`okt: task #42 left review; stop working on it`. Panes are never killed.
`okt team close t-…` does the same at any time. A task that enters the bucket
again opens a new team.

To reopen one seat of an active team, for example a member that failed or
went silent, run `okt agent spawn --team t-… --member security`. Only keys of
the bucket's roster, and `lead`, are accepted. The seat gets a new session
and prompt; its previous session becomes `replaced`.

`trigger: manual` makes a move into the bucket report the team instead of
opening it: `data.team` has `status: suggested`, the resolved seats, and the
`spawn` command to run when you want it. `okt move --dry-run` reports the same
`team` a real move would.

`okt agent spawn --task 42` without `--command` opens the team of the task's
current bucket: the lead first, then every member in parallel. A member that
cannot open is reported as `failed` while the others keep working; a lead that
cannot open fails the whole spawn. `command` must be bound in `bindings.yaml`;
the persona comes from that binding.

## 3. Let the team talk through the task

Teammates communicate with tagged comments on the task, never by typing into
each other's panes:

| To | Run |
| --- | --- |
| Publish a member's result | `okt comment add 42 -a agent -T review-security -b "…"` |
| Ask the lead or a member | `okt comment add 42 -a agent -T to-lead -b "…"` |
| Read messages for a key | `okt comment list 42 --tag to-security` |
| Wait for results | `okt wait --task 42 --tag 'review-*' --timeout 10m` |

A member's `review-<key>` comment marks its session `done`, records
`team.member_done`, and types one line into the lead's pane:
`okt: security finished task #42 — okt team show t-…`. A `to-<key>` comment
types one line into that seat's pane:
`okt: message for security on task #42 — okt comment list 42 --tag to-security`.
A key without a running seat is ignored. Nothing else is ever typed into a
running agent.

`wait` returns the first comment newer than `--after` whose tag matches the
glob, plus the cursor for the next wait. Without `--after`, it starts from the
moment the task's active team opened, so a result published before the wait
began still counts; a task without an active team starts from now. On timeout it
succeeds with `timed_out: true`. Each seat's prompt explains its key, its
lead, and these commands.

## 4. Choose harnesses and launchers

Adapters come from three layers, each replacing the one below by name:
Omakiten's defaults, `agents` in `preferences.yaml`, and `agents` in the
preset's `config/settings.yaml`.

```yaml
agents:
  defaults: {harness: claude-code, model: claude-haiku-5-5, launcher: tmux}
  harnesses:
    claude-code: {argv: [claude, [--model, "{model}"], --, "{prompt}"]}
  launchers:
    tmux:
      target: work
      open: [tmux, new-window, -d, -P, -F, "#{pane_id}", [-t, "{target}"], -c, "{cwd}", -n, "{label}", "{command}"]
      send: [tmux, send-keys, -t, "{pane}", "{text}", Enter]
```

A list inside `argv` is a group, dropped when one of its placeholders is
empty. Put `--` before a positional `{prompt}`, as the shipped `claude-code`
does, so a flag that takes several values cannot swallow the prompt. `{prompt}` becomes the prompt file's content; `{command}` is the
harness command line the launcher runs. A launcher whose `open` prints JSON
names the pane id with `pane: result.root_pane.pane_id`, and `run` starts the
command in that pane.

When the selected harness or launcher is not on `PATH`, the one in your
preferences' `defaults` replaces it and the result reports `fallback`.
`max_sessions` and `max_members` in `preferences.yaml` cap running sessions
per project and members per team; unset means no limit. A preset cannot set
them.

## What the agent inherits

Each pane runs with `OMAKITEN_AGENT_SESSION_ID`, `OMAKITEN_AGENT_MODEL`,
`OMAKITEN_TEAM_ID`, `OMAKITEN_TEAM_MEMBER`, `OMAKITEN_HOME` when it is set, and
a `PATH` that starts with the directory of the `okt` that spawned it. The
agent's own `okt` calls therefore reach the same store and carry its session
id. A spawn from an invocation that used `--db` or `--config` fails, because
the agent cannot inherit flags; select the store with `OMAKITEN_HOME`.
