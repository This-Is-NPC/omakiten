---
name: okt-task-research playbook
description: Investigate the problem space and map the unknowns before any solution is committed.
schema_version: 2
role_affinity:
  - Ideator
  - Reviewer
---
Investigate the problem space before any solution is committed. This pass is read-only — you map the terrain, you do not build on it yet.

## Survey prior art

Survey what already exists with `search` and `tasks.list`, and read the relevant code and docs so you are not re-solving a solved problem.

## Enumerate the unknowns

Enumerate the unknowns the task must resolve — the open questions, the gaps in understanding, the dependencies that are not yet pinned down.

## Produce a findings digest

Produce a findings digest: options, trade-offs, and open questions. Persist it with `comments.add` (task-scoped when a task id exists, project-scoped when researching before task creation). Keep it read-only with respect to code — record what you learned, do not commit a solution here.

## Handoff

Next: when the unknowns are mapped, suggest `okt-task-validate` to pressure-test the framing.
