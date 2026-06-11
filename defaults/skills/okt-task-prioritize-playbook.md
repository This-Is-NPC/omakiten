---
name: okt-task-prioritize playbook
description: Rank the work against alternatives with an explicit scoring method and rationale.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
---
Rank the work against alternatives. The ordering must be auditable, not arbitrary.

## Score with an explicit method

Score the candidates with an explicit method — MoSCoW, RICE, or value-vs-effort — and persist the rationale so the ranking can be defended later.

## Fill the bound scaffold

Call `templates.show` for the bound scoring scaffold, fill it, and persist it with `comments.add` (task-scoped when a task id exists, project-scoped when ranking pre-task candidates). Stay read-only with respect to code and task bodies.

## Handoff

Next: when the priority is justified, suggest `okt-task-create` to author the top candidate.
