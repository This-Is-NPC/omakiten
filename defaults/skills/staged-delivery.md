---
name: Staged delivery
description: Move through requirements → planning → dev → review → docs → done with explicit gates and recorded handoffs.
schema_version: 2
role_affinity:
  - Owner
  - Concierge
  - Scribe
---
Staged delivery (McConnell, *Rapid Development*, 1996) runs work through an ordered sequence of stages, each with an explicit entry and exit gate, so progress is visible and nothing advances on an unstated assumption. In this workflow the stages are requirements → planning → dev → review → docs → done.

## Explicit gates

Each transition is a gate, not a default. A stage exits only when its exit criteria are met:

- **Requirements → planning** — scope, acceptance criteria, and sign-off recorded.
- **Planning → dev** — approach agreed, impact and regression risk assessed.
- **Dev → review** — increment complete, tests green, self-review done.
- **Review → docs** — findings resolved, behaviour accepted.
- **Docs → done** — user-facing and contributor docs reflect the change; claims traceable to code.

Naming the gate prevents the common failure of work that is "mostly done" sliding forward with its gaps invisible.

## Recorded handoffs

Every gate produces a handoff the next stage can read in one pass — the decisions made, the open questions, the next concrete step. The handoff is the memory that lets a different person (or a later session) pick up without re-deriving context. Skipping it pushes the cost downstream as rework.

## Discipline

Stages may be lightweight, but the gates are not optional. When a stage reveals an upstream gap, send the work back rather than patching forward — a requirement discovered in review belongs in requirements, not in a silent dev expansion.

## Boundaries

Staged delivery orders and gates the flow; it does not prescribe how each stage does its work. Pair each stage with its own discipline (elicitation, planning, TDD, review, documentation) and use the gates to keep them honest.
