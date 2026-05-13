---
name: Comment — Rollback plan
description: Fills the `#rollback-plan` requirement before review. Names the path back to safety.
entity: comment
laws:
  - template-fidelity
  - rollback-plan-mandatory
---
**Steps to revert**
1. <command or runbook step>
2. <command or runbook step>

**Validation post-rollback** — <queries / metrics / signals confirming the revert succeeded>

**Customer-comms plan** — <who notifies whom; templated copy if any>

**Owner during rollback** — <name; on-call rotation if applicable>

**Reviewer sign-off on strategy** — <yes / no; required when rollback is non-trivial>
