---
name: okt-plan-show playbook
description: Inspect one plan — wave layout, done/total counts, percent, and the active wave.
schema_version: 2
role_affinity:
  - Owner
  - Concierge
---
Inspect one plan's structure. This is a read-only snapshot — you surface the state, you do not mutate it.

## Report the structure

Call `plans.show` for the slug and report the wave layout, the per-wave and overall done/total counts, the integer percent complete, and the active wave — the lowest-position wave with pending work.

## Handoff

Next: suggest `okt-plan-continue` to preview the next claimable task, or `okt-plan-claim` when the user is ready to reserve it.
