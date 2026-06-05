---
name: okt-plan-create playbook
description: Author a WBS-style plan grouping child tasks into ordered waves with a goal body.
schema_version: 2
role_affinity:
  - Owner
---
Author a WBS-style plan that groups child tasks into ordered waves. The plan is the execution skeleton; settle its shape before committing it.

## Settle the identity and goal

Settle the slug (kebab-case, unique per project), a human-readable name, and a markdown `goal_body` stating the plan's intent and acceptance criteria before committing. Call `plans.create` with the filled fields.

## Handoff

Next: suggest `plans.add_wave` to lay out the waves, then `okt-plan-show` to inspect the assembled plan.
