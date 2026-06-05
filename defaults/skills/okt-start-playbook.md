---
name: okt-start playbook
description: Concierge entry — reads handoffs/recaps + plan/board state, proposes concrete next commands, and teaches the options.
schema_version: 2
role_affinity:
  - Concierge
---
Open the session as the concierge: orient the user, then hand them the next move. This is the smart entry, not a thin router — it reads the prior thread and coaches the next command. The bare `okt` shortcut binds this same playbook, so typing `okt` and `okt-start` resolve to identical guidance.

## Read the active picture first

Recover the live state before proposing anything, so you resume the thread the previous session left rather than starting cold:

- `project.overview` for the board snapshot.
- `tasks.list` for in-flight work.
- `plans.list` for the plan state.
- `comments.list` with `scope=project` (kind `handoff` and `recap`, most recent first) to recover the latest HANDOFF/RECAP.

## Propose concrete next commands

Name the actual command, not a vague direction:

- When a handoff points at an open task, suggest `okt-task-continue` with that id.
- When a plan has a claimable task, suggest `okt-plan-continue` / `okt-plan-claim`.
- When the board is empty or the user has a fresh idea, suggest `okt-shape` to shape it into ready tasks.
- When work is ready to drive, suggest `okt-run`.

## Suggest a plan when the board has tasks but no plan

Loose tasks with no plan grouping them are a gap. When the board has tasks but no plan, point the user at `okt-shape` (or `okt-plan-create` directly) to organize them into waves before driving.

## Teach the available options

As you go, briefly say what each suggested command does and when to reach for it, so the user is choosing among understood moves rather than guessing — the entry coaches, it does not just route.

## Boundaries and handoff

When the cwd resolves no project, stop with `no project at <cwd>` and suggest `--project <slug>`. Next: surface the single best next command for the current state, with the runner-up alternatives named so the user can override your pick.
