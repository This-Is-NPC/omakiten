---
name: omakiten
description: Use the okt CLI to recover development context, maintain work, and configure Omakiten capabilities such as project workflows and hooks.
metadata:
  owner: omakiten
---

# Omakiten

Omakiten is the board where tasks, decisions, evidence, and handoffs live.
`okt` reads and changes it. This skill says where to look; the installed `okt`
says how everything works.

## Rules

- Change board data only through `okt`, never its database or the installed
  `presets/<content-id>/` snapshots.
- Data commands print JSON: check `ok`, read `data`, and on failure act on the
  coded error and its `details`.
- Select the project with `okt projects list`, then pass `--project <slug>`.
- Follow what responses hand you: `next_step_prompt`, a guard's `fix`, an agent
  team in `data.team`, the instructions in your own prompt.
- Leave a handoff with state, validation, blocker, and next action before you
  stop.
- Try configuration and hooks under an isolated `OMAKITEN_HOME`, and report a
  capability as working only after you observe its result.

## Where to look

| To learn | Ask |
| --- | --- |
| Where the work stands | `okt --project <slug> project resume`, then `okt --project <slug> task continue <id>` |
| What the project's workflow expects | `okt --project <slug> command list`, then `command resolve <name>`; read `data.markdown` before acting |
| Which commands and flags exist | `okt --help`, `okt <command> --help`, `okt --version` |
| How a capability works | `okt docs list`, then `okt docs show <topic>` |
| The user's project documentation | `okt --project <slug> knowledge list` |

Start with these guides when the request touches their subject:

| Subject | Topic |
| --- | --- |
| Tasks, evidence, and guards | `how-to-work-on-a-task` |
| Working as an agent | `how-to-use-with-agents` |
| Agent teams and seats | `how-to-run-agent-teams` |
| Hooks and notifications | `how-to-use-hooks` |
| Workflow and preset configuration | `how-to-customize-a-workflow`, `how-to-manage-presets` |
| A refused command | `troubleshooting` |
