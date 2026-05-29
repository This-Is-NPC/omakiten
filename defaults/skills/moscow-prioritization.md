---
name: MoSCoW prioritization
description: Qualitative ranking — Must / Should / Could / Won't (this iteration). Record rationale per item.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
---
MoSCoW (Clegg & Barker, 1994; codified in DSDM) is a qualitative prioritisation method that sorts work into four named bands for a single iteration. Its value is forcing an explicit decision about what is *not* being done now, captured in the fourth band rather than left implicit.

- **Must** — the iteration fails without it. If a Must slips, the release is not viable. Keep this band small; everything cannot be a Must.
- **Should** — important and painful to omit, but the iteration still delivers value without it. The first candidates to defer under pressure.
- **Could** — desirable if time allows; cut without ceremony when capacity tightens.
- **Won't (this iteration)** — explicitly out of scope now. Recording it prevents re-litigation and signals it was considered, not forgotten.

## Discipline

Attach a one-line rationale to every item so the ranking survives the person who made it. Re-balance when scope or capacity changes, and treat a growing Must band as a signal that the iteration is overcommitted rather than a reason to extend it. The "this iteration" qualifier is load-bearing — MoSCoW is per-timebox, not a permanent verdict.

## Boundaries

MoSCoW is qualitative and team-relative; it does not produce a cross-team comparable score. When you need to rank work objectively across teams or quarters, reach for a quantitative method such as RICE instead.
