---
name: Automail fallback
description: Rollback planning — a tested revert path, post-rollback validation, and a comms plan, prepared before the change ships.
schema_version: 2
role_affinity:
  - Builder
  - Owner
  - Reviewer
---
Automail fallback is the discipline of preparing a working spare limb before you risk the real one: every change ships with a rehearsed way back. Like a maintenance crew that keeps a fitted replacement ready, you do not improvise the revert in the middle of an outage — you design it before the change goes out.

## The revert path

Write the concrete steps that undo the change — the revert commit or the down-migration, the config to restore, the flag to flip back. The path must be specific enough that someone who did not write the change can execute it under pressure. A revert that exists only as "we'll figure it out" is not a fallback.

## Post-rollback validation

Name the checks that confirm the system is healthy after the revert: the queries, the smoke test, the dashboard that must return to baseline. Rolling back is only half the job; proving the rollback worked is the other half.

## Comms plan

State who is told, through what channel, and what they are told — including any customer-facing message. A silent rollback that leaves stakeholders guessing costs trust even when the system recovers.

## Non-trivial cases need sign-off

Multi-step migrations, schema or data-shape changes, and anything that cannot be cleanly reversed require a reviewer to sign off on the strategy in advance. Where a change is genuinely irreversible, say so explicitly and stage it behind a flag so the exposure can be cut even if the data cannot be unwound.
