# How to run agents for a task

**The question:** how do I have Omakiten run coding agents for a task,
choose which harness and model each one runs, and see what they did?

Prerequisites: a registered project, a task, and a harness (`claude`,
`codex`, or `opencode`) on `PATH`. Nothing here runs unless a bucket declares
a team or you call `okt agent spawn`.

Every agent runs once and headless: the harness does its work through the
board and exits. Nothing stays open. Results and questions are task comments
that the next agent, or you, reads.

## 1. Run one agent

```bash
okt --project example agent spawn --task 42 --command okt-task-review
```

The lead starts in the background at the project root. Its prompt is the
resolved command, the task, and its seat in the team. The result names the
team, the session, its transcript, and the commands to follow it:

```bash
okt --project example agent show s-1a2b3c4d5e6f
okt --project example team show t-1a2b3c4d5e6f
okt --project example logs --session s-1a2b3c4d5e6f
okt --project example task continue 42
```

A session is `running` while its harness runs, then `done` when the harness
exits 0, `failed` when it exits otherwise, cannot start, or passes its
timeout, and `stopped` when its team closes. `agent show` carries the
`exit_code` (`-1` when a signal ended the harness), the error, the harness
`pid`, and the `transcript`: a JSONL file holding the harness output. A JSON
object the harness prints on stdout is a line as is; every other line is
`{"stream": "stdout" | "stderr", "text": …}`. The file ends with the
supervisor's own result line.
`logs --session` lists everything the agent's own `okt` calls recorded.

`okt task show 42` and `okt task continue 42` carry the task's active team,
with its sessions, in `team`; the field is absent when no team is active.
`okt agent list` lists the project's teams, newest first, each with its
sessions; `--task 42` and `--status active|failed|closed` narrow it. Over the
local API it is `GET /api/v1/projects/{project}/agents?task=&status=`.

`--harness` and `--model` override the lead's for this spawn. Spawning again
for the same task and bucket returns its active team, with `status:
running`, instead of starting a second one; a team stays active, with its
finished seats, until its task leaves the bucket or the team is closed.

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
      lead: {command: okt-task-review, timeout: 30m}
      members:
        - {key: security, command: okt-task-review, harness: codex, brief: "security only"}
```

`trigger: auto` starts the team when a task moves into the bucket, after the
move commits; the move's result carries it in `data.team`, with `status:
started`, each seat's session and transcript, and the commands to follow
them. A team that cannot start never undoes the move: `data.team.status` is
`failed` and the error says why. `okt move --no-spawn`, and `--no-spawn` on
`progress record`, report the team as `skipped` instead.

When the task leaves the bucket, its team there closes: the team becomes
`closed`, and each running seat becomes `stopped` while its harness process
group is told to stop. `okt team close t-…` does the same at any time. A task that
enters the bucket again opens a new team.

To run one seat of an active team again, for example a member that failed,
run `okt agent spawn --team t-… --member security`. Only keys of the
bucket's roster, and `lead`, are accepted. The seat gets a new session and
prompt; its previous session is stopped if it still runs and becomes
`replaced`.

`trigger: manual` makes a move into the bucket report the team instead of
starting it: `data.team` has `status: suggested`, the resolved seats, and the
`spawn` command to run when you want it. `okt move --dry-run` reports the same
`team` a real move would.

`okt agent spawn --task 42` without `--command` starts the team of the task's
current bucket: the lead first, then every member in parallel. A member that
cannot start is reported as `failed` while the others keep working; a lead
that cannot start fails the whole spawn. `command` must be bound in
`bindings.yaml`; the persona comes from that binding.

## 3. Let the team talk through the task

Teammates communicate with tagged comments on the task:

| To | Run |
| --- | --- |
| Publish a member's result | `okt comment add 42 -a agent -T review-security -b "…"` |
| Leave a question for the lead or a member | `okt comment add 42 -a agent -T to-lead -b "…"` |
| Read the questions for a key | `okt comment list 42 --tag to-security` |
| Wait for results | `okt wait --task 42 --tag 'review-*' --timeout 10m` |

`okt team show` lists the `results` each member published and, in
`missing_results`, the members whose session ended `done` without a
`review-<key>` comment.

`wait` returns the first comment newer than `--after` whose tag matches the
glob, plus the cursor for the next wait. Without `--after`, it starts from the
moment the task's active team opened, so a result published before the wait
began still counts; a task without an active team starts from now. On timeout it
succeeds with `timed_out: true`. Each seat's prompt explains its key, its
lead, and these commands.

## 4. Choose harnesses

Omakiten ships how to run `claude-code`, `codex`, and `opencode` headless,
but never chooses one. A seat's harness, model, and timeout come from the
role, then `agents.defaults` in the preset's `config/settings.yaml`, then
`agents.defaults` in your `preferences.yaml`. Harness templates come from the
same layers, each replacing the one below by name, over Omakiten's own:

```yaml
agents:
  defaults: {harness: claude-code, timeout: 45m}
  harnesses:
    claude-code:
      argv: [claude, -p, --output-format, stream-json, --verbose, --permission-mode, acceptEdits, --allowedTools, "Bash(okt *)", [--model, "{model}"], --, "{prompt}"]
    codex:
      argv: [codex, exec, --json, --skip-git-repo-check, --sandbox, workspace-write, --add-dir, "{store_dir}", --cd, "{cwd}", [--model, "{model}"], --, "{prompt}"]
    opencode:
      argv: [opencode, run, --format, json, --auto, [--model, "{model}"], --, "{prompt}"]
```

These are the shipped templates. The template owns what the harness may do
without asking: `claude-code` accepts edits and runs `okt`; `codex` writes in
the project and in the store directory, `{store_dir}`, where `okt` keeps its
database; `opencode --auto` approves what its own configuration does not
deny. Override a template in the preset or your preferences to grant more or
less.

| Placeholder | Value |
| --- | --- |
| `{prompt}` | The prompt file's content. |
| `{prompt_file}` | The prompt file's path. |
| `{model}` | The seat's model; empty when none is set. |
| `{cwd}` | The project root, where the harness runs. |
| `{store_dir}` | The directory of the Omakiten database. |
| `{session_id}`, `{team_id}`, `{member}` | The seat's identity. |

A list inside `argv` is a group, dropped when one of its placeholders is
empty: without a model, `[--model, "{model}"]` drops and the harness uses its
own default. Put `--` before a positional `{prompt}`, as the shipped
templates do, so a flag that takes several values cannot swallow the prompt.

`timeout` is a Go duration such as `30m`. The supervisor stops the harness
when it passes, and the session ends `failed`. Without one, the run lasts
until the harness exits.

A seat left without a harness starts nothing: its status, and the team's
when it is the lead, is `unconfigured`, and `error` (`agent_unconfigured`)
names the missing setting and where to put it, with the absolute path of your
`preferences.yaml` in `preferences_path`. The move still succeeds. A `manual`
team is still `suggested`, with each incomplete seat marked.

When the selected harness is not on `PATH`, the one in your preferences'
`defaults` replaces it and the result reports `fallback`; without one, the
seat fails with `harness_missing`. `max_sessions` and `max_members` in
`preferences.yaml` cap running sessions per project and members per team;
unset means no limit. A preset cannot set them.

## What runs, and what the agent inherits

`okt agent spawn` and an automatic move start one supervisor per seat: a
hidden `okt agent run` process, detached from the invoking command, which
runs the harness in its own process group and records how it ended. The
supervisor takes a lock under `<state>/agents/` and the harness inherits it,
so the lock stays held while any process of the run lives, even one whose
supervisor was killed. A session still `running` once no one holds its lock,
because the machine restarted or the run ended unrecorded, ends `failed`
with an `orphaned` error the next time `agent list`, `agent show`,
`team show`, or a spawn reads it.

The harness runs with `OMAKITEN_AGENT_SESSION_ID`, `OMAKITEN_AGENT_MODEL`,
`OMAKITEN_TEAM_ID`, `OMAKITEN_TEAM_MEMBER`, `OMAKITEN_HOME` when it is set,
and a `PATH` that starts with the directory of the `okt` that spawned it.
The agent's own `okt` calls therefore reach the same store and carry its
session id. A spawn from an invocation that used `--db` or `--config` fails,
because the agent cannot inherit flags; select the store with
`OMAKITEN_HOME`.

Hooks can react to the run: `agent.session_started` when a seat starts and
`agent.session_finished` when it ends, each with `team_id`, `task_id`,
`member`, and `session_id`, the latter with `status`, `exit_code`, and
`error`; and `team.closed`, with `team_id`, `task_id`, `bucket`, and
`reason`, when a team closes.
