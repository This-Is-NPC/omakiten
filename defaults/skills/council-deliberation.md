---
name: Council deliberation
description: Convene every active-config persona to assess impact on a subject brief before persisting or executing.
schema_version: 2
role_affinity:
  - Owner
  - Concierge
---
Convene a council of every persona wired in the active config before you commit tasks, plans, or execution. The council is the customization surface — spawn one subagent per persona returned by `personas.list`, not per canonical role.

## Omakiten returns a prompt; the agent does the spawning

This skill is a PROMPT contract the consuming agent executes in its own runtime. Omakiten cannot spawn agents. Use the Agent/Task tool in the harness that received this prompt.

## Subject brief

The orchestrator prepares a compact brief first: what is proposed, scope, candidate tasks or waves, dependencies, and known risks. The brief is not persisted until the council has spoken and you have synthesized.

## Convene the council

1. Call `personas.list` — spawn one subagent per returned slug (always all; no role filter).
2. For each slug, hand the subagent a lean delegation contract:
   - Call `personas.get <slug>` in its own fresh MCP context
   - Adopt that persona's body, expanded laws, and skill repertoire as voice and constraints
   - Assess ONLY the subject brief using the impact questions below
   - Do NOT implement, persist, or run unrelated `okt-task-*` playbooks
   - Return a compact opinion: impact, risks, blockers, recommendations (eight bullets max)

Run independent persona assessments concurrently when worthwhile; synthesize only after every spawned subagent returns or you halt with an explicit reason.

## Impact questions

From this persona's perspective on the brief:

- What breaks if we proceed as proposed?
- What is missing for this persona to endorse it?
- What would this persona prioritize differently?

## Synthesize before persist

Aggregate the returns: name agreements, disagreements, and gaps. Coach forks that need the user. Do not call `tasks.create_intent`, `tasks.create`, `plans.create`, or other write tools that commit the shaped outcome until synthesis is complete — or the user explicitly accepts the named gaps.
