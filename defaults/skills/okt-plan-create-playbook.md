---
name: okt-plan-create playbook
description: Author a WBS-style plan grouping child tasks into ordered waves with a goal body.
schema_version: 2
role_affinity:
  - Owner
---
Author a WBS-style plan that groups child tasks into ordered waves. The plan is the execution skeleton; settle its shape and persist the grouping before committing it.

## Settle the identity and goal

Settle the slug (kebab-case, unique per project), a human-readable name, and a markdown `goal_body` stating the plan's intent and acceptance criteria before committing. Call `plans.create` with the filled fields.

## Build the wave layout

After the shell exists, call `plans.add_wave` for each ordered wave and `plans.assign_task` for every existing task that belongs in that wave. If the plan encodes task ordering, persist each blocker edge with `dependencies.add`. Verify with `plans.show`; a plan with no waves or no assigned tasks is only a shell, not an assembled plan.

## Handoff

Next: suggest `okt-plan-show` to inspect the assembled plan.
