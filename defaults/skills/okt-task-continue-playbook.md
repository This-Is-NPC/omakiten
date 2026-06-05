---
name: okt-task-continue playbook
description: Read a task's checkpoint before resuming work.
schema_version: 2
role_affinity:
  - Builder
---
Read a task's checkpoint — understand where the task stopped, do not start coding. This is orientation, not execution.

## Read the checkpoint

Call `tasks.continue` for the task id, then summarize the last decision, the open questions, and the immediate next increment so you resume the thread rather than restarting it.

## Handoff

Next: suggest `okt-task-implement` with the same id.
