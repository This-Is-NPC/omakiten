---
name: Handoff synthesis
description: Gather current project state, populate the note-handoff template, and persist as a project-scoped handoff note for the next session.
---
A handoff is a checkpoint another agent (or your future self) can read in one pass and resume work without re-deriving context. Treat it as a write to the project's shared memory, not a log line.

## Inputs to gather

1. **Active tasks** — `tasks_list` with `bucket=dev` then `bucket=review`. Capture id, title, bucket, last bucket transition.
2. **Active plan wave** — `plans_list` then `plans_show` on the most recently updated plan; record the current wave key, claimed/unclaimed task ids, and any blockers logged on the plan row.
3. **Recent progress** — `task_activity_list` over the trailing 24h window for each active task; collect distinct progress comments.
4. **Decisions & blockers** — `comments_list` filtered by tags `decision` and `blocker` over the same window across the active tasks.
5. **Unmet dependencies** — `dependencies_list` per active task; record any blocker whose blocking task is not yet `done`.
6. **Previous handoff** — `notes_list` with `kind=handoff`, `scope=project`, `limit=1`. Diff against the previous snapshot to surface what moved.

## Synthesis

- Group findings by task, not by source. One task = one bullet stack.
- For each task: title, current bucket, last meaningful progress line, open blockers, next concrete step.
- Promote at most three items to a top-level "Focus" list — the work the next session should pick up first.
- Note any decision that changes scope, contract, or interfaces. Cite the comment id.
- Flag stale work: dev tasks with no activity in 48h.

## Persist

- Fill the `note-handoff` template with the synthesised content. Leave optional slots empty rather than padding.
- Persist via `notes_create` with `scope=project` and `kind=handoff`. The project is the cwd-resolved active project.
- Return the new note id and a body preview to the user for confirmation; do not mutate any task or comment as part of synthesis.

## Boundaries

- Read-only against tasks, plans, comments, and dependencies. The only write is the `notes_create` call.
- If no project resolves from cwd, abort and ask the user to name a project — handoffs are never global.
- If the previous handoff is missing or older than 14 days, synthesise from current state alone and note the gap in the body.
