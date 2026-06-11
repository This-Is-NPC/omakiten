---
name: okt-task-resume playbook
description: Cold-start a task from scratch — rebuild full context when none is loaded in this session.
schema_version: 2
role_affinity:
  - Builder
---
Cold-start a task — assume no prior context exists in this session. Unlike a warm checkpoint read, you know nothing and must rebuild the full picture from the artifacts.

## Reconstruct from scratch

Call `tasks.continue` for the task id, then call `comments.list` for the same task to read the full thread. Reconstruct the full picture: description, every comment, the dependency graph, and the latest `#resume` / `#tests-passing` checkpoints. Re-derive the current state, the open questions, and the immediate next increment rather than assuming any of it is already in context.

## Handoff

Next: suggest `okt-task-implement` with the same id once you have rebuilt context.
