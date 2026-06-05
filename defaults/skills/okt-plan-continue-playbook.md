---
name: okt-plan-continue playbook
description: Preview a plan plus the next claimable task before committing to a claim.
schema_version: 2
role_affinity:
  - Owner
  - Concierge
---
Preview a plan before committing to a claim. Nothing is reserved here — this is the look-before-you-leap step.

## Preview the aggregate and the candidate

Call `plans.continue` for the slug: it returns the full plan aggregate — waves, done/total, active wave — plus a non-mutating preview of the task `plans.claim_next` would reserve next. Inspect the goal_body, the wave layout, and the candidate task, then report them. Read-only — nothing is reserved here.

## Handoff

Next: suggest `okt-plan-claim` to atomically reserve the previewed task.
