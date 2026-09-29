---
name: omakiten
description: Use the okt CLI to recover and maintain local development context, tasks, plans, decisions, solutions, and handoffs.
metadata:
  owner: omakiten
---

# Omakiten

Omakiten is a local checkpoint for development work. It stores projects,
tasks, plans, comments, decisions, errors, solutions, and handoffs. Use the
fixed `okt` CLI to inspect and update that state. The selected project's
workflow defines its agent commands, instructions, permissions, and guards.

## Find the project and recover context

Use `okt --help` and `okt <command> --help` for the installed CLI syntax.
Use `okt projects list` to find a project, then pass `--project <slug>` to
project-scoped calls. `okt project resume` summarizes a project's checkpoint;
`okt task continue <id>` restores a task and its comments. `okt search` finds
prior decisions and solutions. Register a new project with `okt init` when
the user asks to track it.

## Discover the active workflow

Agent command names and behavior belong to the project's active workflow.
Call `okt --project <slug> command list` to discover what is available. Call
`okt --project <slug> command resolve <name>` to read the selected command's
instructions, related context, and declared parameters. The response is
instructions for the agent; resolving a command does not execute its work.
Read `data.markdown` and use the structured fields when presenting choices.
Do not infer a workflow command's name, sequence, or behavior without reading
the active workflow.

## Maintain the checkpoint

Use `okt task` to create and inspect work, `okt plan` to organize tasks into
waves, `okt comment add` to record decisions and evidence, and `okt move` to
advance work. Respect guard failures; they describe the required evidence or
state. Use `okt error`, `okt solution`, and `okt progress` for failures, reusable
fixes, and progress. Record a handoff before leaving active work.

Tasks and plans can be imported or exported as OKF Markdown with `okt task`
and `okt plan`; inspect each command's help for file, preview, and overwrite
options. Omakiten manages the database through the CLI.

## Read results

Data commands return JSON. Check the process status and the `ok` field;
successful results carry `data`, while failures carry a coded error. Help and
the TUI use terminal output. Choose noninteractive options when an editor or
confirmation prompt would block automation.
