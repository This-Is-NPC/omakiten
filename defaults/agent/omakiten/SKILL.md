---
name: omakiten
description: Use the okt CLI to recover development context and maintain Omakiten tasks, plans, decisions, errors, solutions, and handoffs.
metadata:
  owner: omakiten
---

# Omakiten

Omakiten is a local checkpoint store for development work. Use `okt` to read
and write its board; the database is managed by the CLI. Project workflow,
permissions and guards come from the active preset.

## Recover context

Run `okt --help` to discover commands and `okt <command> --help` for arguments.
If `okt` is unavailable, report the missing installation. Select the intended
project explicitly with `--project <slug>`; use `okt projects list` to discover
registered projects. Register an authorized new project with
`okt init --name <name> --slug <slug> --root <path>`.

Start with `okt --project <slug> project resume`. Read the task being continued
with `okt --project <slug> task continue <id>` and its comments. The continuation
includes workflow context, relevant history and similar work. Use `okt search`
for decisions and previous solutions instead of repeating investigations.

## Follow the configured workflow

Use `okt --project <slug> command list` to discover available playbooks and
`okt --project <slug> command resolve <name>` to obtain the configured persona,
skills, laws and templates for the current action. Read `data.markdown` from
the response when a playbook is relevant. These instructions supplement the
user's request and repository rules.

Create authorized work with `okt task create-intent`; inspect similarity hints
before confirming a possible duplicate. Use `okt plan` to organize tasks into
waves and `okt plan claim` to acquire work through the atomic claim operation.
For claims, set `OMAKITEN_AGENT_MODEL` to the actual agent model identifier.
`OMAKITEN_AGENT_SESSION_ID` can carry a stable session identifier; both values
also attach provenance to CLI activity. Do not invent another agent's identity.
Use `okt move` for transitions. Respect guard failures and return actionable
blocking information rather than bypassing the workflow.

## Record and hand off

Use `okt comment add` for decisions, evidence and handoffs; select task, project
or universal scope deliberately. Mark agent comments with `--author agent`.
Record failures through `okt error`, reusable fixes through `okt solution`,
and progress through `okt progress`. Consult each command's help for its
required arguments and confirmation behavior.

Before leaving, record the current state, validation results, blockers and
next action in a handoff comment on the work being performed. Keep factual
notes concise and avoid storing credentials or unrelated private data.

## Interpret results

Data commands return JSON: `ok: true` carries `data`; `ok: false` carries a coded
error and the process exits unsuccessfully. Inspect both the exit status and
the response before acting. Help and interactive setup/TUI have their own
terminal output. Use noninteractive flags where an editor or confirmation
prompt would block automation. Do not infer success from an empty response.
