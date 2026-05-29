---
name: RICE scoring
description: Quantitative priority — Reach × Impact × Confidence ÷ Effort. Use when comparing across teams or quarters.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
---
RICE (Intercom, 2016) is a quantitative prioritisation score that makes competing initiatives comparable on one number. It is most useful when ranking across teams or quarters, where qualitative bands lose their shared meaning.

The score is `(Reach × Impact × Confidence) ÷ Effort`.

- **Reach** — how many units (users, requests, accounts) the initiative affects in a defined period. Use a real count from data, not an impression.
- **Impact** — the effect per unit, on a fixed scale (e.g. 3 = massive, 2 = high, 1 = medium, 0.5 = low, 0.25 = minimal). The scale must be applied consistently across all items being compared.
- **Confidence** — a percentage discount for how well-evidenced Reach and Impact are. Low confidence on a high raw score is the method's main guard against optimistic estimates.
- **Effort** — total person-time, in a single unit (person-weeks). It is the denominator, so under-estimating effort inflates priority.

## Discipline

The number ranks; it does not decide. Record the inputs alongside the score so a reviewer can challenge an estimate rather than the conclusion. Re-score when new data moves Reach, Impact, or Confidence — a stale RICE table ranks yesterday's beliefs.

## Boundaries

RICE assumes the items are roughly independent and that effort and reach are estimable. For tightly coupled work, or within a single short iteration where qualitative judgement is faster, MoSCoW is the lighter tool. Pair RICE with INVEST/elicitation upstream so the items being scored are well-formed.
