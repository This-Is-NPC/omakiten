---
name: okt-shape playbook
description: Owner orchestrator — convene the persona council, shape a raw idea into ready tasks + an execution plan; deliberate before persist.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
---
Shape a raw idea — or a loose backlog — into ready-to-build tasks plus an execution plan. You orchestrate the shaping; you do not implement. Read the current picture first with `project.overview` and `tasks.list` so you shape against what already exists, not in a vacuum.

## Omakiten returns a prompt; the agent does the spawning

This playbook is a PROMPT that Omakiten returns to the consuming agent — Omakiten itself cannot spawn agents. The consuming agent (or its harness) performs all Agent/Task subagent spawning described below.

## Prepare the subject brief

Draft a compact proposal before any durable writes: topic, scope, candidate tasks, proposed waves and dependencies, and known risks. The brief is for deliberation — not yet persisted.

## Convene the council before you persist

Follow the bound **Council deliberation** skill: call `personas.list`, spawn one subagent per returned slug, each subagent calls `personas.get` in its own fresh MCP context, and each returns a compact impact opinion on the brief. Synthesize agreements, disagreements, and gaps before you author tasks or plans. Do not call `tasks.create_intent`, `plans.create`, or other commit tools until synthesis is complete — or the user explicitly accepts the named gaps.

## Chain the discover → define granulars

Direct by command NAME only — do not render their bodies. Use these after the council when the brief still needs evidence or refinement.

- DISCOVER the problem space: `okt-task-research` to map prior art and unknowns, then `okt-task-validate` to pressure-test whether the problem is real and worth solving now.
- DEFINE the solution: `okt-task-requirements` to capture functional/non-functional criteria, `okt-task-prioritize` to rank against alternatives, then `okt-task-create` to author each ready task with an INVEST-checked story.
- For coarse work, slot `okt-task-decompose` and `okt-task-estimate` between define and create to right-size the slices.

## Coach the decision

At each fork, coach the decision: skip discovery only when the problem is already well-understood; do not author a task whose value or feasibility is still unproven — loop back to validate instead. A shaping pass is done when each candidate is a concrete, prioritized, ready task.

## Group the ready tasks into a plan

With `okt-plan-create`: settle the slug, name, and goal_body. Then assemble the durable plan, not just a chat outline: call `plans.add_wave` for each ordered wave to create the ordered waves, `plans.assign_task` for every ready task, and `dependencies.add` for every ordering edge (`task_id` = dependent task, `depends_on_task_id` = blocker task). Waves express execution order; they do not replace the dependency graph. Verify with `plans.show` and `dependencies.list` before handoff. Suggest a plan whenever the shaping produced more than one ready task or any dependency between them.

## Surface what is still undefined

Before you hand off, list every gap — unanswered requirement, unranked candidate, unestimated coarse task, missing acceptance criterion, unresolved dependency, or council dissent you did not resolve — so the user sees exactly what blocks build, rather than discovering it mid-implementation.

## Handoff

Next: once the plan is assembled and the gaps are named, suggest `okt-run` to drive the plan to completion, or `okt-task-continue` with a specific id when the user wants to build one task by hand.
