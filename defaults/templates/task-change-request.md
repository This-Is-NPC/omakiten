---
name: Task — Change request
description: Change-control scaffold — risk class, SLO impact, pre-mortem, rollback strategy, approval matrix.
entity: task
laws:
  - template-fidelity
  - pre-mortem-required
  - rollback-plan-mandatory
  - blast-radius-awareness
---
## Summary
<one-paragraph description of the change>

## Risk classification
- **Class** — <low | medium | high | critical>
- **Blast radius** — <users affected · services touched · irreversibility>

## SLO impact
- **SLOs touched** — <list, with current budget consumption>
- **Expected effect** — <reliability / latency / throughput delta>

## Pre-mortem (top 3 failure modes)
- <mode> → detection: <signal> → mitigation: <plan>
- <mode> → detection: <signal> → mitigation: <plan>
- <mode> → detection: <signal> → mitigation: <plan>

## Rollback strategy
<steps to revert · post-rollback validation · customer-comms plan>

## Approval matrix
- Reviewer A: <role / sign-off scope>
- Reviewer B: <role / sign-off scope>

## Out of scope
<related changes intentionally not covered>
