---
name: Comment — Risk assessment
description: Fills the `#risk-assessment` guard. Names top risks, mitigations, and residual risk accepted.
entity: comment
laws:
  - template-fidelity
  - blast-radius-awareness
  - error-budget-aware
---
**Risk matrix**
| Risk | Likelihood | Impact | Mitigation |
| --- | --- | --- | --- |
| <risk 1> | <H/M/L> | <H/M/L> | <plan> |
| <risk 2> | <H/M/L> | <H/M/L> | <plan> |

**Top 3 risks ranked** — <ordered by likelihood × impact>

**SLO impact** — <SLO touched · current budget burn · expected delta>

**Blast radius** — <users / services / irreversibility class>

**Residual risk accepted** — <explicit acknowledgment + reviewer sign-off>
