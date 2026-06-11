---
name: okt-pause playbook
description: Concierge close — snapshot git + active task + plan into a handoff note for the next session.
schema_version: 2
role_affinity:
  - Concierge
  - Scribe
---
Close the current session by snapshotting where the work stands into a handoff note the next session reads first.

## Capture the live picture across all three planes

- GIT state — run `git status` and `git diff --stat` via Bash for the working-tree summary, the current branch, and uncommitted work.
- ACTIVE task — `tasks.list` for in-flight ids, `task_activity.list` for what moved since the previous handoff.
- PLAN — `plans.continue` / `plans.show` for the active wave and what remains claimable.

Find the previous handoff with `comments.list` (`scope=project`, `kind=handoff`, newest first), then synthesise material state since that handoff via `project.overview`.

## Persist the handoff

Call `templates.show note-handoff` to fetch the scaffold, fill the populated slots, and PERSIST THE HANDOFF via `comments.add` with `scope=project`, `kind=handoff` (no `task_id`) — the durable artifact is the handoff comment, not the chat. Honor `--body` to override the rendered body verbatim and `--note` to append extra context under a free-form section. When nothing material changed since the last handoff, render with a "no material changes since <prev>" marker and still persist so the timeline stays continuous.

## Coach the handoff quality

Lead with the single next action the next session should take, then the open questions and the in-flight diff — write what an agent with zero context needs to resume, not a changelog of what you did.

## Boundaries and handoff

When the cwd resolves no project, stop with `no project at <cwd>` and suggest `--project <slug>`; when the project lacks an active workflow, omit the workflow/wave sections. Next: suggest the user run `okt-start` (or `okt-note-recap`) at the top of their next session to load this handoff back into context.
