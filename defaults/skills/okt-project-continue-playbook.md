---
name: okt-project-continue playbook
description: Warm-resume the project from the last session — pick up the open thread.
schema_version: 2
role_affinity:
  - Concierge
---
Warm-resume the current project from the last session. Assume continuity — you are picking up a thread, not deriving the project from scratch.

## Recover the recent picture

Recent project handoffs live in `comments.list` (scope=project). Call `project.overview` for the active snapshot and `tasks.list` for in-flight work, then pick up the most recent open thread without re-deriving the whole project.

## Surface what changed

Unlike the cold `okt-project-resume` scan, this is the warm hand-back: surface what changed since the last session and name the immediate next move.

## Handoff

Next: suggest `okt-task-continue` with the in-flight task id to read its checkpoint.
