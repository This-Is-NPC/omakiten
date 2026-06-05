---
name: okt-shape playbook
description: Owner orchestrator — shape a raw idea or backlog into ready tasks + an execution plan; chains discover/define + okt-plan-create and surfaces gaps.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
---
Shape a raw idea — or a loose backlog — into ready-to-build tasks plus an execution plan. You orchestrate the shaping; you do not implement. Read the current picture first with `project.overview` and `tasks.list` so you shape against what already exists, not in a vacuum.

## Chain the discover → define granulars

Direct by command NAME only — do not render their bodies.

- DISCOVER the problem space: `okt-task-research` to map prior art and unknowns, then `okt-task-validate` to pressure-test whether the problem is real and worth solving now.
- DEFINE the solution: `okt-task-requirements` to capture functional/non-functional criteria, `okt-task-prioritize` to rank against alternatives, then `okt-task-create` to author each ready task with an INVEST-checked story.
- For coarse work, slot `okt-task-decompose` and `okt-task-estimate` between define and create to right-size the slices.

## Coach the decision

At each fork, coach the decision: skip discovery only when the problem is already well-understood; do not author a task whose value or feasibility is still unproven — loop back to validate instead. A shaping pass is done when each candidate is a concrete, prioritized, ready task.

## Group the ready tasks into a plan

With `okt-plan-create`: settle the slug, name, and goal_body, then lay the tasks into ordered waves so dependencies fall across wave boundaries. Suggest a plan whenever the shaping produced more than one ready task or any dependency between them.

## Surface what is still undefined

Before you hand off, list every gap — unanswered requirement, unranked candidate, unestimated coarse task, missing acceptance criterion, unresolved dependency — so the user sees exactly what blocks build, rather than discovering it mid-implementation.

## Handoff

Next: once the plan is assembled and the gaps are named, suggest `okt-run` to drive the plan to completion, or `okt-task-continue` with a specific id when the user wants to build one task by hand.
