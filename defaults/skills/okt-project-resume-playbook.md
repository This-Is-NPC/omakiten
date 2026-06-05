---
name: okt-project-resume playbook
description: Scan likely-next work across the active project.
schema_version: 2
role_affinity:
  - Concierge
---
Scan for the next work to pick up. This is the cold scan across the project, not a warm hand-back of a known thread.

## Scan and report

Call `project.resume` and report the top candidates, each with a one-line rationale so the user can choose with context rather than guessing.

## Handoff

Next: when the user picks a task, suggest `okt-task-continue` with that task id.
