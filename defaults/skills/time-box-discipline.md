---
name: Time-box discipline
description: Declare boxes up front; recognize when to kill, promote, or extend with explicit reason.
schema_version: 2
role_affinity:
  - Owner
  - Builder
---
A time box is a fixed budget of effort declared before the work starts, used to bound open-ended activities — spikes, investigations, refactors with uncertain payoff. The discipline is in honouring the box and making the end-of-box decision explicit rather than drifting.

## Declare up front

State the box before beginning: the duration and the question it is meant to answer ("two hours to determine whether the cache approach is viable"). A box without a stated question is just a deadline; the question is what makes the end-of-box decision possible.

## The end-of-box decision

When the box expires, choose one of three explicitly and record why:

- **Kill** — the question is answered negatively, or the cost now clearly exceeds the value. Stop and capture what was learned.
- **Promote** — the spike proved the approach; convert the learning into planned, properly-engineered work.
- **Extend** — genuinely close to an answer and the extension is bounded. Extend once, with a new explicit box and reason; a serially-extended box is a kill that nobody made.

## Discipline

The failure mode is the silent extension — continuing past the box because stopping feels like waste. The sunk cost is already spent; the only question at the boundary is the value of the *next* increment. Name the decision so it is reviewable.

## Boundaries

Time-boxing bounds effort on uncertain work; it is not a substitute for estimation on well-understood deliverables. Use it for investigation and risk reduction, then hand promoted work to normal planning and implementation.
