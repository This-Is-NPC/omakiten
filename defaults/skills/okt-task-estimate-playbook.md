---
name: okt-task-estimate playbook
description: Size each increment with a relative estimate and a one-line basis-of-estimate.
schema_version: 2
role_affinity:
  - Owner
  - Builder
---
Size each increment. Estimates are relative and explicit, not gut feel left unstated.

## Attach a relative estimate

Attach a relative estimate — points or t-shirt size — to every slice, each with a one-line basis-of-estimate so the number can be questioned. Persist the sizing note with `comments.add` (or `progress.record` when updating an existing task checkpoint).

## Flag the uncertain slices

Flag the increments whose uncertainty dominates, since those are where the plan is most likely to slip. Stay read-only.

## Handoff

Next: when the sizing is recorded, suggest `okt-task-design` to shape the first increment.
