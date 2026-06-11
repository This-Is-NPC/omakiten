---
name: okt-task-requirements playbook
description: Capture functional + non-functional requirements and explicit acceptance criteria.
schema_version: 2
role_affinity:
  - Ideator
  - Tester
---
Capture what the solution must satisfy. This is the requirements baseline the rest of the work is measured against.

## Elicit the requirements

Elicit functional and non-functional requirements, the edge cases, and explicit acceptance criteria. Separate must-have from nice-to-have so the scope is unambiguous.

## Fill the bound scaffold

Call `templates.show` for any bound requirements/acceptance scaffold, fill it, and persist the requirements baseline with `comments.add` (task-scoped when a task id exists, project-scoped when shaping before task creation). Stay read-only with respect to the task body — you record the baseline, you do not author the task here.

## Handoff

Next: suggest `okt-task-prioritize` to rank the work against alternatives.
