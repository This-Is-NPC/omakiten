---
name: Decision record
description: Generic decision-record scaffold — status, context, decision, consequences, alternatives. Project picks file path.
entity: decision
laws:
  - template-fidelity
  - decision-record-on-divergence
---
# <NNNN> — <short title>

**Status** — <proposed | accepted | superseded by <other id> | deprecated>
**Date** — <YYYY-MM-DD>

## Context
<what forces are at play; what problem demands a decision; what constraints frame it>

## Decision
<the choice made, stated clearly in one paragraph>

## Consequences
<what becomes easier; what becomes harder; what gets locked in; what stays open>

## Alternatives considered
- **<Option A>** — why rejected
- **<Option B>** — why rejected

## References
- <link to PR / task / external doc>
