---
name: okt-plan-claim playbook
description: Atomically reserve the next claimable task in the plan's active wave.
schema_version: 2
role_affinity:
  - Owner
  - Builder
---
Reserve the next claimable task in the plan's active wave. This is the atomic commit step that turns a preview into ownership.

## Claim atomically

Call `okt plan claim` for the slug — it atomically stamps the task with the caller and emits `task.assigned`, but it does not move the bucket. Report the claimed task id, or surface `claimed=false` when no unassigned first-bucket task remains in the active wave.

## Handoff

Next: suggest `okt move` to advance the claimed task once the preset guards are satisfied, then `okt-task-continue` with the claimed id to start work.
