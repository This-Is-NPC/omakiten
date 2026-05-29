---
name: Comment — Risk recheck
description: "Re-evaluation of a previously logged risk after the work landed: did the predicted failure mode materialise, and is the mitigation still sound?"
entity: comment
laws:
  - template-fidelity
  - pre-mortem-aware
---
**Original risk** — <the failure mode named earlier; cite the `#pre-mortem` or `#risk-assessment` comment id>

**Outcome** — <materialised / did not materialise / partially — with the observed signal>

**Mitigation status** — <held / needs adjustment / no longer relevant>

**New or revised risk** — <anything surfaced by the implementation that was not foreseen; "none" if clean>

**Action** — <accept residual, add a follow-up task, or tighten a guard — be concrete>
